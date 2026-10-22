package mandate

import (
	"context"
	"errors"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/workload"
)

// ErrNotEnrolled is a mandate for a workload nobody enrolled.
var ErrNotEnrolled = errors.New("this workload is not enrolled")

// Mandates keeps the mandates; Store is the implementation.
type Mandates interface {
	Create(ctx context.Context, in Input, approver string) (Mandate, error)
	Approve(ctx context.Context, id, approver string) (Mandate, error)
	End(ctx context.Context, id string) (Mandate, error)
	List(ctx context.Context) ([]Mandate, error)
	ActiveFor(ctx context.Context, spiffeID string) (Mandate, error)
}

// Workloads tells whether a system is enrolled.
type Workloads interface {
	FindEnrolled(ctx context.Context, spiffeID string) (workload.Workload, error)
}

// Entitlements withdraws the credentials already made.
type Entitlements interface {
	RevokeType(ctx context.Context, subject entitlement.Subject, credentialType string) (int64, error)
}

type Service struct {
	mandates     Mandates
	workloads    Workloads
	entitlements Entitlements
}

func NewService(mandates Mandates, workloads Workloads, entitlements Entitlements) *Service {
	return &Service{mandates: mandates, workloads: workloads, entitlements: entitlements}
}

// Create records a mandate for an enrolled workload with the person as its first approver.
func (s *Service) Create(ctx context.Context, approver string, in Input) (Mandate, error) {
	if _, err := s.workloads.FindEnrolled(ctx, in.Workload); errors.Is(err, workload.ErrNotFound) {
		return Mandate{}, ErrNotEnrolled
	} else if err != nil {
		return Mandate{}, err
	}
	return s.mandates.Create(ctx, in, approver)
}

func (s *Service) Approve(ctx context.Context, id, approver string) (Mandate, error) {
	return s.mandates.Approve(ctx, id, approver)
}

func (s *Service) List(ctx context.Context) ([]Mandate, error) { return s.mandates.List(ctx) }

// End ends the mandate and withdraws the credentials already made from it, so a verifier that reads the status list learns of it.
func (s *Service) End(ctx context.Context, id string) error {
	m, err := s.mandates.End(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.entitlements.RevokeType(ctx, entitlement.WorkloadOf(m.Workload), credential.Mandate)
	return err
}

// GrantFor gives what the workload's active mandate allows, if it has one.
func (s *Service) GrantFor(ctx context.Context, spiffeID string) (workload.Grant, bool, error) {
	m, err := s.mandates.ActiveFor(ctx, spiffeID)
	if errors.Is(err, ErrNotFound) {
		return workload.Grant{}, false, nil
	}
	if err != nil {
		return workload.Grant{}, false, err
	}
	return workload.Grant{Resource: m.Resource, Approvers: m.Approvers, EndsAt: m.ExpiresAt}, true, nil
}
