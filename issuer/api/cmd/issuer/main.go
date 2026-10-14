// Command issuer is the credential issuer: the API its pages call and the OpenID4VCI endpoints wallets talk to.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"attesta/issuer/internal/config"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/httpapi"
	"attesta/issuer/internal/identity"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/oid4vci"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/service"
)

// The database for entitlements and sessions.
const database = "issuer_entitlements"

const renewalCooldown = 10 * time.Minute

func main() {
	if err := run(); err != nil {
		slog.Error("issuer stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, postgres.Options{Host: cfg.Database.Host, User: cfg.Database.User, Password: cfg.Database.Password}, database)
	if err != nil {
		return err
	}
	defer pool.Close()

	me := identity.FromSeed(cfg.Seed)
	secret := sha256.Sum256([]byte("attesta-issuer:" + cfg.Seed))
	store := entitlement.NewStore(pool, 0)

	var svc *service.Service
	protocol := oid4vci.New(oid4vci.Config{
		PublicURL: cfg.PublicURL, IssuerName: cfg.Name, Identity: me, Secret: secret[:], OfferLifetime: cfg.QRLifetime, RenewalCooldown: renewalCooldown,
	}, oid4vci.NewSessions(pool, secret[:]), store, func(ctx context.Context, e entitlement.Entitlement) error { return svc.MailQRCode(ctx, e) })
	svc = service.New(service.Config{IssuerName: cfg.Name, QRLifetime: cfg.QRLifetime}, store, protocol, mail.NewSMTP(cfg.SMTP))

	// A credential made just before the issuer stopped may not have been marked issued; its session still names the entitlement.
	if err := protocol.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.Handler(httpapi.Config{IssuerName: cfg.Name, DID: me.DID, Protocol: protocol.Handler()}, svc),
		ReadHeaderTimeout: 10 * time.Second,
	}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	slog.Info(fmt.Sprintf("issuer listening on %d, public url %s", cfg.Port, cfg.PublicURL))
	slog.Info("issuer DID " + me.DID)

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
