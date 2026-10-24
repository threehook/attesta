// Command Attesta runs the authorization backend: ask a wallet for an SD-JWT presentation, verify it, then evaluate the matching Rego policy.
package main

import (
	"attesta/backend/internal/adl"
	"attesta/backend/internal/authz"
	"attesta/backend/internal/config"
	"attesta/backend/internal/httpapi"
	"attesta/backend/internal/presentation"
	"attesta/backend/internal/scripts"
	"attesta/backend/internal/status"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/threehook/sdjwt-go"
)

// policyReloadInterval is how often the policy directory is read again; a ConfigMap mounted there changes in place within about a minute.
const policyReloadInterval = 30 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "attesta:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	evaluator := authz.NewOPA()

	policies := scripts.NewStore(evaluator)
	if cfg.PoliciesDir != "" {
		if err := policies.LoadDir(cfg.PoliciesDir); err != nil {
			return fmt.Errorf("load policies: %w", err)
		}
	}

	logger.Info("policies loaded", "dir", cfg.PoliciesDir, "ids", policies.IDs())

	if cfg.PoliciesDir != "" {
		go policies.Watch(context.Background(), cfg.PoliciesDir, policyReloadInterval, func(r scripts.SyncResult) {
			if r.Err != nil {
				logger.Error("policy reload failed", "error", r.Err)
			}

			for id, err := range r.Failed {
				logger.Error("policy rejected, keeping the previous version", "id", id, "error", err)
			}

			if len(r.Installed) > 0 || len(r.Removed) > 0 {
				logger.Info("policies reloaded", "installed", r.Installed, "removed", r.Removed)
			}
		})
	}

	statuses, err := status.New(status.Options{Prefixes: cfg.StatusPrefixes, Hosts: cfg.StatusHosts, Keys: sdjwt.DIDKey{}})
	if err != nil {
		return err
	}

	if len(cfg.StatusPrefixes) == 0 {
		logger.Warn("no status list prefixes are allowed (ATTESTA_STATUS_URL_PREFIXES): every credential will be refused")
	}

	presenter := presentation.New(presentation.Options{PublicURL: cfg.PublicURL, Keys: sdjwt.DIDKey{}, Status: statuses})

	decisions, err := adl.New(context.Background(), adl.ConfigFromEnv(), logger)
	if err != nil {
		return err
	}

	server := &httpapi.Server{
		Decisions:   decisions,
		Authz:       evaluator,
		Policies:    policies,
		AdminToken:  cfg.AdminToken,
		Logger:      logger,
		Presenter:   presenter,
		CORSOrigins: cfg.CORSOrigins,
	}

	httpServer := &http.Server{Addr: cfg.Addr, Handler: server.Routes(), ReadHeaderTimeout: 10 * time.Second}

	stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.ListenAndServe() }()

	logger.Info("listening", "addr", cfg.Addr)

	select {
	case err := <-serveErr:
		return err
	case <-stop.Done():
	}
	// Records still batched for the collector are flushed before exit.
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()

	err = httpServer.Shutdown(shutdown)
	if closeErr := decisions.Close(shutdown); closeErr != nil {
		logger.Error("flushing the decision log failed", "error", closeErr)
	}

	return err
}
