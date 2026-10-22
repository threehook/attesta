package workload

import (
	"context"
	"errors"
	"fmt"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
)

// DefaultCredentialLifetime is how long a workload's credential lasts; the workload asks for a new one when it needs it.
const DefaultCredentialLifetime = time.Hour

// DefaultOfferLifetime is how long the link works that a workload gets; it is used at once.
const DefaultOfferLifetime = 5 * time.Minute

// Registry keeps the enrolled workloads.
type Registry interface {
	Enroll(ctx context.Context, in Input) (Workload, error)
	List(ctx context.Context) ([]Workload, error)
	FindEnrolled(ctx context.Context, spiffeID string) (Workload, error)
	Revoke(ctx context.Context, id string) (Workload, error)
}

// Entitlements records what a credential is made from.
type Entitlements interface {
	Create(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error)
	RevokeAll(ctx context.Context, subject entitlement.Subject) (int64, error)
}

// Verifier proves which SPIFFE ID a token belongs to.
type Verifier interface {
	Verify(ctx context.Context, token string) (spiffeID string, err error)
}

// Offerer makes the link a wallet takes a credential from.
type Offerer interface {
	OfferFor(ctx context.Context, e entitlement.Entitlement, lifetime time.Duration) (string, error)
}

type Config struct {
	// CredentialLifetime defaults to DefaultCredentialLifetime.
	CredentialLifetime time.Duration
	// OfferLifetime defaults to DefaultOfferLifetime.
	OfferLifetime time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
}

type Service struct {
	cfg          Config
	registry     Registry
	entitlements Entitlements
	verifier     Verifier
	offers       Offerer
}

func NewService(cfg Config, registry Registry, entitlements Entitlements, verifier Verifier, offers Offerer) *Service {
	if cfg.CredentialLifetime == 0 {
		cfg.CredentialLifetime = DefaultCredentialLifetime
	}
	if cfg.OfferLifetime == 0 {
		cfg.OfferLifetime = DefaultOfferLifetime
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, registry: registry, entitlements: entitlements, verifier: verifier, offers: offers}
}

// ErrNotEnrolled is a token that is valid for a SPIFFE ID nobody enrolled.
var ErrNotEnrolled = errors.New("this workload is not enrolled")

// Offer checks the token and, for an enrolled workload, gives the link its wallet takes a new credential from. Every call makes a new entitlement, so the
// workload gets a credential for each run, and each ends by itself.
func (s *Service) Offer(ctx context.Context, token string) (string, error) {
	spiffeID, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return "", err
	}
	w, err := s.registry.FindEnrolled(ctx, spiffeID)
	if errors.Is(err, ErrNotFound) {
		return "", ErrNotEnrolled
	}
	if err != nil {
		return "", err
	}
	e, err := s.entitlements.Create(ctx, entitlement.New{
		Subject: entitlement.WorkloadOf(w.SpiffeID),
		Type:    credential.Workload,
		Claims: map[string]string{
			"workload": w.SpiffeID, "owner": w.Owner, "environment": w.Environment,
			credential.ExpiresAtKey: s.cfg.Now().Add(s.cfg.CredentialLifetime).UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return "", fmt.Errorf("record the entitlement: %w", err)
	}
	return s.offers.OfferFor(ctx, e, s.cfg.OfferLifetime)
}

func (s *Service) Enroll(ctx context.Context, in Input) (Workload, error) {
	return s.registry.Enroll(ctx, in)
}

func (s *Service) List(ctx context.Context) ([]Workload, error) { return s.registry.List(ctx) }

// Revoke ends the enrollment and withdraws the credentials already made, so a verifier that reads the status list learns of it.
func (s *Service) Revoke(ctx context.Context, id string) error {
	w, err := s.registry.Revoke(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.entitlements.RevokeAll(ctx, entitlement.WorkloadOf(w.SpiffeID))
	return err
}
