// Command zk-puoi runs the authorization backend: ask a wallet for an SD-JWT presentation, verify it, then evaluate the matching Gno policy.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/config"
	"zk-puoi/backend/internal/httpapi"
	"zk-puoi/backend/internal/presentation"
	"zk-puoi/backend/internal/scripts"
	"zk-puoi/backend/internal/sdjwt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "zk-puoi:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.FromEnv()

	gnoVM, err := authz.NewGnoVM(os.Stderr)
	if err != nil {
		return fmt.Errorf("init gnovm: %w", err)
	}

	policies := scripts.NewStore(gnoVM)
	if cfg.PoliciesDir != "" {
		if err := policies.LoadDir(cfg.PoliciesDir); err != nil {
			return fmt.Errorf("load policies: %w", err)
		}
	}
	logger.Info("policies loaded", "dir", cfg.PoliciesDir, "ids", policies.IDs())

	presenter := presentation.New(presentation.Options{PublicURL: cfg.PublicURL, Keys: sdjwt.DIDKey{}})

	server := &httpapi.Server{
		Authz:       gnoVM,
		Policies:    policies,
		AdminToken:  cfg.AdminToken,
		Logger:      logger,
		Presenter:   presenter,
		CORSOrigins: cfg.CORSOrigins,
	}

	logger.Info("listening", "addr", cfg.Addr)
	return http.ListenAndServe(cfg.Addr, server.Routes())
}
