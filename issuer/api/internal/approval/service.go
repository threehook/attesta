package approval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/workload"
)

var (
	// ErrNotEnrolled is a request from a workload nobody enrolled.
	ErrNotEnrolled = errors.New("this workload is not enrolled")
	// ErrNotApproved is a request that not enough people approved yet.
	ErrNotApproved = errors.New("the request is not approved yet")
	// ErrGone is a request that was rejected, handed out or has run out.
	ErrGone = errors.New("the request is rejected, handed out or has run out")
)

const (
	DefaultRequired           = 2
	DefaultRequestLifetime    = 24 * time.Hour
	DefaultCredentialLifetime = 15 * time.Minute
	DefaultOfferLifetime      = 5 * time.Minute
)

// Requests keeps the requests; Store is the implementation.
type Requests interface {
	Open(ctx context.Context, workload, resource string, action map[string]string, lifetime time.Duration) (Request, bool, error)
	Get(ctx context.Context, id string) (Request, error)
	List(ctx context.Context) ([]Request, error)
	Approve(ctx context.Context, id, approver string) (Request, error)
	Reject(ctx context.Context, id string) error
	Deliver(ctx context.Context, id string, required int) (bool, error)
}

// Verifier proves which SPIFFE ID a token belongs to.
type Verifier interface {
	Verify(ctx context.Context, token string) (spiffeID string, err error)
}

// Workloads tells whether a system is enrolled.
type Workloads interface {
	FindEnrolled(ctx context.Context, spiffeID string) (workload.Workload, error)
}

// Entitlements records what the Approval credential is made from.
type Entitlements interface {
	Create(ctx context.Context, n entitlement.New) (entitlement.Entitlement, error)
}

// Offerer makes the link a wallet takes a credential from.
type Offerer interface {
	OfferFor(ctx context.Context, e entitlement.Entitlement, lifetime time.Duration) (string, error)
}

type Config struct {
	// Required is how many different people must approve before the workload gets its credential. Defaults to DefaultRequired.
	Required int
	// RequestLifetime is how long a request waits for approval. Defaults to DefaultRequestLifetime.
	RequestLifetime time.Duration
	// CredentialLifetime is how long the Approval credential lasts. Defaults to DefaultCredentialLifetime.
	CredentialLifetime time.Duration
	// OfferLifetime defaults to DefaultOfferLifetime.
	OfferLifetime time.Duration
	// PublicURL is where people reach the issuer's pages; the mail links to it.
	PublicURL string
	// Notify are the addresses that get a mail for a new request; none means no mail.
	Notify []string
	// Now defaults to time.Now.
	Now func() time.Time
}

type Service struct {
	cfg          Config
	requests     Requests
	verifier     Verifier
	workloads    Workloads
	entitlements Entitlements
	offers       Offerer
	mailer       mail.Mailer
}

func NewService(cfg Config, requests Requests, verifier Verifier, workloads Workloads, entitlements Entitlements, offers Offerer, mailer mail.Mailer) *Service {
	if cfg.Required == 0 {
		cfg.Required = DefaultRequired
	}
	if cfg.RequestLifetime == 0 {
		cfg.RequestLifetime = DefaultRequestLifetime
	}
	if cfg.CredentialLifetime == 0 {
		cfg.CredentialLifetime = DefaultCredentialLifetime
	}
	if cfg.OfferLifetime == 0 {
		cfg.OfferLifetime = DefaultOfferLifetime
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, requests: requests, verifier: verifier, workloads: workloads, entitlements: entitlements, offers: offers, mailer: mailer}
}

// enrolled proves who the token is and that the system is enrolled.
func (s *Service) enrolled(ctx context.Context, token string) (string, error) {
	spiffeID, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return "", err
	}
	if _, err := s.workloads.FindEnrolled(ctx, spiffeID); errors.Is(err, workload.ErrNotFound) {
		return "", ErrNotEnrolled
	} else if err != nil {
		return "", err
	}
	return spiffeID, nil
}

// Request parks the action of the workload that shows the token. A request that waits already for the same resource is the same request. The people who
// must be told get a mail with a link to the page where they approve; a mail that fails is logged and does not stop the request.
func (s *Service) Request(ctx context.Context, token, resource string, action map[string]string) (Request, error) {
	spiffeID, err := s.enrolled(ctx, token)
	if err != nil {
		return Request{}, err
	}
	r, created, err := s.requests.Open(ctx, spiffeID, resource, action, s.cfg.RequestLifetime)
	if err != nil || !created {
		return r, err
	}
	for _, to := range s.cfg.Notify {
		message := mail.Message{
			To:      to,
			Subject: "Goedkeuring gevraagd: " + r.Resource,
			Text:    fmt.Sprintf("Het systeem %s vraagt goedkeuring voor %s.\n\nKeur het goed of wijs het af op %s/approvals\n", r.Workload, r.Resource, strings.TrimRight(s.cfg.PublicURL, "/")),
		}
		if err := s.mailer.Send(ctx, message); err != nil {
			slog.Error("could not mail a request for approval", "to", to, "error", err)
		}
	}
	return r, nil
}

// Offer gives the link the workload takes its Approval credential from, once, when enough people approved the request. ErrNotApproved says it is too early.
func (s *Service) Offer(ctx context.Context, token, id string) (string, error) {
	spiffeID, err := s.enrolled(ctx, token)
	if err != nil {
		return "", err
	}
	r, err := s.requests.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if r.Workload != spiffeID {
		return "", ErrNotFound
	}
	if r.State != Open {
		return "", ErrGone
	}
	if len(r.Approvers) < s.cfg.Required {
		return "", ErrNotApproved
	}
	if delivered, err := s.requests.Deliver(ctx, id, s.cfg.Required); err != nil {
		return "", err
	} else if !delivered {
		return "", ErrGone
	}
	e, err := s.entitlements.Create(ctx, entitlement.New{
		Subject: entitlement.WorkloadOf(spiffeID),
		Type:    credential.Approval,
		Claims: map[string]string{
			"workload": spiffeID, "resource": r.Resource, "approvers": strings.Join(r.Approvers, ","),
			credential.ExpiresAtKey: s.cfg.Now().Add(s.cfg.CredentialLifetime).UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return "", fmt.Errorf("record the approval entitlement: %w", err)
	}
	return s.offers.OfferFor(ctx, e, s.cfg.OfferLifetime)
}

// Pending gives how many people approved a request of the workload, for a workload that polls.
func (s *Service) Pending(ctx context.Context, token, id string) (Request, error) {
	spiffeID, err := s.enrolled(ctx, token)
	if err != nil {
		return Request{}, err
	}
	r, err := s.requests.Get(ctx, id)
	if err == nil && r.Workload != spiffeID {
		return Request{}, ErrNotFound
	}
	return r, err
}

// Required is how many different people must approve a request.
func (s *Service) Required() int { return s.cfg.Required }

// CredentialLifetime is how long the Approval credential lasts once the workload has taken it.
func (s *Service) CredentialLifetime() time.Duration { return s.cfg.CredentialLifetime }

func (s *Service) List(ctx context.Context) ([]Request, error) { return s.requests.List(ctx) }

func (s *Service) Approve(ctx context.Context, id, approver string) (Request, error) {
	return s.requests.Approve(ctx, id, approver)
}

func (s *Service) Reject(ctx context.Context, id string) error { return s.requests.Reject(ctx, id) }
