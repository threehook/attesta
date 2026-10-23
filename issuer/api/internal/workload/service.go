package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Grant is what people allowed a workload to do, as its active mandate says.
type Grant struct {
	Resource  string
	Approvers []string
	EndsAt    time.Time
}

// Grants gives the active mandate of a workload, if it has one, and ends it.
type Grants interface {
	GrantFor(ctx context.Context, spiffeID string) (Grant, bool, error)
	EndFor(ctx context.Context, spiffeID string) error
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
	grants       Grants
}

func NewService(cfg Config, registry Registry, entitlements Entitlements, verifier Verifier, offers Offerer, grants Grants) *Service {
	if cfg.CredentialLifetime == 0 {
		cfg.CredentialLifetime = DefaultCredentialLifetime
	}
	if cfg.OfferLifetime == 0 {
		cfg.OfferLifetime = DefaultOfferLifetime
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, registry: registry, entitlements: entitlements, verifier: verifier, offers: offers, grants: grants}
}

// ErrNotEnrolled is a token that is valid for a SPIFFE ID nobody enrolled.
var ErrNotEnrolled = errors.New("this workload is not enrolled")

// Offers are the links a workload's wallet takes its credentials from. Mandate is empty when no mandate is active.
type Offers struct {
	Workload string
	Mandate  string
}

// Offer checks the token and, for an enrolled workload, gives the links its wallet takes new credentials from: the Workload credential, and the Mandate
// credential when people allowed something. Every call makes new entitlements, so the workload gets credentials for each run, and each ends by itself.
func (s *Service) Offer(ctx context.Context, token string) (Offers, error) {
	spiffeID, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return Offers{}, err
	}
	w, err := s.registry.FindEnrolled(ctx, spiffeID)
	if errors.Is(err, ErrNotFound) {
		return Offers{}, ErrNotEnrolled
	}
	if err != nil {
		return Offers{}, err
	}
	now := s.cfg.Now()
	end := now.Add(s.cfg.CredentialLifetime)
	var offers Offers
	if offers.Workload, err = s.offer(ctx, w.SpiffeID, credential.Workload, map[string]string{
		"workload": w.SpiffeID, "owner": w.Owner, "environment": w.Environment, credential.ExpiresAtKey: end.UTC().Format(time.RFC3339),
	}); err != nil {
		return Offers{}, err
	}
	grant, ok, err := s.grants.GrantFor(ctx, w.SpiffeID)
	if err != nil || !ok {
		return offers, err
	}
	if grant.EndsAt.Before(end) {
		end = grant.EndsAt
	}
	offers.Mandate, err = s.offer(ctx, w.SpiffeID, credential.Mandate, map[string]string{
		"workload": w.SpiffeID, "resource": grant.Resource, "approvers": strings.Join(grant.Approvers, ","), credential.ExpiresAtKey: end.UTC().Format(time.RFC3339),
	})
	return offers, err
}

// offer records an entitlement of the workload and makes the link its wallet takes the credential from.
func (s *Service) offer(ctx context.Context, spiffeID, credentialType string, claims map[string]string) (string, error) {
	e, err := s.entitlements.Create(ctx, entitlement.New{Subject: entitlement.WorkloadOf(spiffeID), Type: credentialType, Claims: claims})
	if err != nil {
		return "", fmt.Errorf("record the %s entitlement: %w", credentialType, err)
	}
	return s.offers.OfferFor(ctx, e, s.cfg.OfferLifetime)
}

func (s *Service) Enroll(ctx context.Context, in Input) (Workload, error) {
	return s.registry.Enroll(ctx, in)
}

func (s *Service) List(ctx context.Context) ([]Workload, error) { return s.registry.List(ctx) }

// Revoke ends the enrollment and the workload's mandate, and withdraws the credentials already made, so a verifier that reads the status list learns of it.
func (s *Service) Revoke(ctx context.Context, id string) error {
	w, err := s.registry.Revoke(ctx, id)
	if err != nil {
		return err
	}
	if err := s.grants.EndFor(ctx, w.SpiffeID); err != nil {
		return err
	}
	_, err = s.entitlements.RevokeAll(ctx, entitlement.WorkloadOf(w.SpiffeID))
	return err
}
