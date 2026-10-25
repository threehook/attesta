// Package mandate keeps what named people allow a workload to do. Each time the workload asks for its credentials, the active mandate becomes a
// Mandate credential, so the policy sees who approved what, and until when.
package mandate

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("there is no such mandate")
	ErrExists   = errors.New("this workload has a mandate already: end it first")
	ErrInvalid  = errors.New("a mandate has a resource and an end in the future, at most 31 days away")
)

const (
	maxResource = 200
	maxLifetime = 31 * 24 * time.Hour
)

// Mandate is what people allow a workload to do. Resource is a glob over the resource an application asks about, with ":" as separator.
type Mandate struct {
	ID        string
	Workload  string
	Resource  string
	ExpiresAt time.Time
	Approvers []string
	CreatedAt time.Time
}

// Input is what the first approver gives.
type Input struct {
	Workload  string
	Resource  string
	ExpiresAt time.Time
}

func clean(in Input, now time.Time) (Input, error) {
	in.Resource = strings.TrimSpace(in.Resource)
	if in.Resource == "" || len([]rune(in.Resource)) > maxResource || strings.ContainsAny(in.Resource, " \t\r\n") {
		return Input{}, ErrInvalid
	}

	if !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(maxLifetime)) {
		return Input{}, ErrInvalid
	}

	in.ExpiresAt = in.ExpiresAt.UTC()

	return in, nil
}

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, now: time.Now} }

// active is a mandate that has not been ended or run out.
const active = "ended_at IS NULL AND expires_at > now()"

const selectActive = `SELECT m.id, m.workload, m.resource, m.expires_at, m.created_at,
  COALESCE(array_agg(a.approver ORDER BY a.approved_at, a.approver) FILTER (WHERE a.approver IS NOT NULL), '{}')
  FROM mandates m LEFT JOIN mandate_approvals a ON a.mandate_id = m.id WHERE m.ended_at IS NULL AND m.expires_at > now()`

func scan(row pgx.Row) (Mandate, error) {
	var m Mandate

	err := row.Scan(&m.ID, &m.Workload, &m.Resource, &m.ExpiresAt, &m.CreatedAt, &m.Approvers)

	return m, err
}

// Create records a mandate that its first approver gave. An ended or run-out mandate of the workload makes room for it.
func (s *Store) Create(ctx context.Context, in Input, approver string) (Mandate, error) {
	in, err := clean(in, s.now())
	if err != nil {
		return Mandate{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Mandate{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "UPDATE mandates SET ended_at = now() WHERE workload = $1 AND ended_at IS NULL AND expires_at <= now()",
		in.Workload); err != nil {
		return Mandate{}, err
	}

	var id string

	err = tx.QueryRow(ctx, "INSERT INTO mandates (workload, resource, expires_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING RETURNING id",
		in.Workload,
		in.Resource, in.ExpiresAt).
		Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mandate{}, ErrExists
	}

	if err != nil {
		return Mandate{}, err
	}

	if _, err := tx.Exec(ctx, "INSERT INTO mandate_approvals (mandate_id, approver) VALUES ($1, $2)", id, approver); err != nil {
		return Mandate{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Mandate{}, err
	}

	return s.Find(ctx, id)
}

// Find finds an active mandate.
func (s *Store) Find(ctx context.Context, id string) (Mandate, error) {
	m, err := scan(s.pool.QueryRow(ctx, selectActive+" AND m.id = $1 GROUP BY m.id", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mandate{}, ErrNotFound
	}

	return m, err
}

// Approve adds a person to the approvers of an active mandate. Approving twice changes nothing.
func (s *Store) Approve(ctx context.Context, id, approver string) (Mandate, error) {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO mandate_approvals (mandate_id, approver) SELECT id, $2 FROM mandates WHERE id = $1 AND "+active+" ON "+
			"CONFLICT DO NOTHING", id, approver); err != nil {
		return Mandate{}, err
	}

	return s.Find(ctx, id)
}

// End ends an active mandate and gives it.
func (s *Store) End(ctx context.Context, id string) (Mandate, error) {
	m, err := s.Find(ctx, id)
	if err != nil {
		return Mandate{}, err
	}

	if _, err := s.pool.Exec(ctx, "UPDATE mandates SET ended_at = now() WHERE id = $1 AND ended_at IS NULL", id); err != nil {
		return Mandate{}, err
	}

	return m, nil
}

// List gives the active mandates, newest first.
func (s *Store) List(ctx context.Context) ([]Mandate, error) {
	rows, err := s.pool.Query(ctx, selectActive+" GROUP BY m.id ORDER BY m.created_at DESC, m.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Mandate

	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// ActiveFor finds the active mandate of a workload.
func (s *Store) ActiveFor(ctx context.Context, spiffeID string) (Mandate, error) {
	m, err := scan(s.pool.QueryRow(ctx, selectActive+" AND m.workload = $1 GROUP BY m.id", spiffeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mandate{}, ErrNotFound
	}

	return m, err
}
