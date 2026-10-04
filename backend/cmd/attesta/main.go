// Command attesta runs the authorization backend: ask a wallet for an SD-JWT presentation, verify it, then evaluate the matching Gno policy.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"attesta/backend/internal/authz"
	"attesta/backend/internal/config"
	"attesta/backend/internal/httpapi"
	"attesta/backend/internal/presentation"
	"attesta/backend/internal/scripts"
	"attesta/backend/internal/sdjwt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "attesta:", err)
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
