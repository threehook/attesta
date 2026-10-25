// Command issuer is the credential issuer: the API its pages call and the OpenID4VCI endpoints wallets talk to.
package main

import (
	"attesta/issuer/internal/approval"
	"attesta/issuer/internal/auth"
	"attesta/issuer/internal/config"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/httpapi"
	"attesta/issuer/internal/identity"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/mandate"
	"attesta/issuer/internal/oid4vci"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/role"
	"attesta/issuer/internal/service"
	"attesta/issuer/internal/statuslist"
	"attesta/issuer/internal/svid"
	"attesta/issuer/internal/workload"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

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

	pool, err := postgres.Open(
		ctx,
		postgres.Options{Host: cfg.Database.Host, User: cfg.Database.User, Password: cfg.Database.Password},
		cfg.Database.Name,
	)
	if err != nil {
		return err
	}
	defer pool.Close()

	me := identity.FromSeed(cfg.Seed)
	secret := sha256.Sum256([]byte("attesta-issuer:" + cfg.Seed))
	store := entitlement.NewStore(pool, 0)
	types := credtype.NewStore(pool)

	status := statuslist.New(me, store, strings.TrimSuffix(cfg.PublicURL, "/"), nil)

	var svc *service.Service

	protocol := oid4vci.New(oid4vci.Config{
		PublicURL: cfg.PublicURL, StatusURI: status.URI(), IssuerName: cfg.Name, Identity: me, Secret: secret[:], OfferLifetime: cfg.QRLifetime,
		RenewalCooldown: cfg.RenewalCooldown,
	}, oid4vci.NewSessions(pool, secret[:]), store, types, func(ctx context.Context, e entitlement.Entitlement) error {
		return svc.MailQRCode(ctx,
			e)
	})
	svc = service.New(
		service.Config{IssuerName: cfg.Name, QRLifetime: cfg.QRLifetime},
		store,
		role.NewStore(pool),
		types,
		protocol,
		mail.NewSMTP(cfg.SMTP),
	)

	// A credential made just before the issuer stopped may not have been marked issued; its session still names the entitlement.
	if err := protocol.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	var approvers *auth.Verifier

	httpCfg := httpapi.Config{IssuerName: cfg.Name, DID: me.DID, Protocol: protocol.Handler(), Status: status.Handler()}
	if cfg.Auth.Disabled {
		slog.Warn("sign-in is switched off: anyone who reaches the forms can record entitlements")
	} else {
		opts := auth.Options{Issuer: cfg.Auth.Issuer, DiscoveryURL: cfg.Auth.DiscoveryURL, ClientID: cfg.Auth.ClientID, Role: cfg.Auth.Role}
		httpCfg.Protect = auth.New(opts).Protect
		opts.Role = cfg.Auth.ApproverRole
		approvers = auth.New(opts)
	}

	if cfg.SPIFFE.TrustDomain != "" {
		var options []workloadapi.JWTSourceOption
		if cfg.SPIFFE.Socket != "" {
			options = append(options, workloadapi.WithClientOptions(workloadapi.WithAddr(cfg.SPIFFE.Socket)))
		}

		bundles, err := workloadapi.NewJWTSource(ctx, options...)
		if err != nil {
			return fmt.Errorf("read the SPIFFE trust bundle: %w", err)
		}
		defer bundles.Close()

		verifier, err := svid.New(bundles, cfg.SPIFFE.TrustDomain, cfg.SPIFFE.Audience)
		if err != nil {
			return err
		}

		registry := workload.NewStore(pool)
		mandates := mandate.NewService(mandate.NewStore(pool), registry, store)
		workloads := workload.NewService(
			workload.Config{CredentialLifetime: cfg.SPIFFE.CredentialLifetime},
			registry,
			store,
			verifier,
			protocol,
			mandates,
		)
		httpCfg.Workloads = workload.Handler(workloads, httpCfg.Protect)

		var protectApprovers func(http.Handler) http.Handler
		if approvers != nil {
			protectApprovers = approvers.Protect
		}

		approver := func(r *http.Request) string { return auth.Email(r.Context()) }
		approvals := approval.NewService(approval.Config{
			Required: cfg.SPIFFE.Approvals.Required, RequestLifetime: cfg.SPIFFE.Approvals.RequestLifetime,
			CredentialLifetime: cfg.SPIFFE.Approvals.CredentialLifetime,
			PublicURL:          cfg.PublicURL, Notify: cfg.SPIFFE.Approvals.Notify,
		}, approval.NewStore(pool), verifier, registry, store, protocol, mail.NewSMTP(cfg.SMTP))
		httpCfg.WorkloadApprovals = approval.WorkloadHandler(approvals)
		httpCfg.Approvals = approval.PeopleHandler(approvals, protectApprovers, approver)
		httpCfg.Mandates = mandate.Handler(mandates, protectApprovers, approver)

		slog.Info("workloads are recognized in the SPIFFE trust domain " + cfg.SPIFFE.TrustDomain)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           httpapi.Handler(httpCfg, svc),
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
