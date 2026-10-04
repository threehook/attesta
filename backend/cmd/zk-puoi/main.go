// Command zk-puoi runs the authorization backend: verify a ZK proof, then evaluate the matching Gno policy against its disclosed public signals.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"zk-puoi/backend/internal/auth"
	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/config"
	"zk-puoi/backend/internal/httpapi"
	"zk-puoi/backend/internal/proof"
	"zk-puoi/backend/internal/scripts"
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

	vk, err := proof.LoadVerifyingKey(cfg.VerificationKeyPath)
	if err != nil {
		return fmt.Errorf("load verifying key: %w", err)
	}

	registryRoot, err := authz.LoadRegistryRoot(cfg.RegistryPath)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	server := &httpapi.Server{
		Proof:        proof.NewSnarkjsVerifier(vk),
		Authz:        gnoVM,
		Policies:     policies,
		Auth:         auth.NewIssuer(cfg.JWTSecret),
		AdminToken:   cfg.AdminToken,
		Logger:       logger,
		RegistryRoot: registryRoot,
		CORSOrigins:  cfg.CORSOrigins,
	}

	logger.Info("listening", "addr", cfg.Addr)
	return http.ListenAndServe(cfg.Addr, server.Routes())
}
