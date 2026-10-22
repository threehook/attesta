// Package workload enrolls the systems that get a credential of their own, and gives each a fresh one when its SPIFFE identity proves who it is.
package workload

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

var (
	ErrNotFound = errors.New("the workload is not enrolled")
	ErrExists   = errors.New("a workload with this SPIFFE ID is already enrolled")
	ErrInvalid  = errors.New("a workload has a SPIFFE ID with a path, an owner and an environment")
)

const maxText = 100

// Workload is a system that is enrolled.
type Workload struct {
	ID          string
	SpiffeID    string
	Owner       string
	Environment string
	CreatedAt   time.Time
}

// Input is what staff give when they enroll a workload.
type Input struct {
	SpiffeID    string
	Owner       string
	Environment string
}

func clean(in Input) (Input, error) {
	in.SpiffeID, in.Owner, in.Environment = strings.TrimSpace(in.SpiffeID), strings.TrimSpace(in.Owner), strings.TrimSpace(in.Environment)
	id, err := spiffeid.FromString(in.SpiffeID)
	if err != nil || id.Path() == "" {
		return Input{}, ErrInvalid
	}
	for _, text := range []string{in.Owner, in.Environment} {
		if text == "" || len([]rune(text)) > maxText {
			return Input{}, ErrInvalid
		}
	}
	return in, nil
}

type Store struct{ pool *pgxpool.Pool }

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const columns = "id, spiffe_id, owner, environment, created_at"

func scan(row pgx.Row) (Workload, error) {
	var w Workload
	err := row.Scan(&w.ID, &w.SpiffeID, &w.Owner, &w.Environment, &w.CreatedAt)
	return w, err
}

func isDuplicate(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// Enroll records a workload. A SPIFFE ID is enrolled once at a time.
func (s *Store) Enroll(ctx context.Context, in Input) (Workload, error) {
	in, err := clean(in)
	if err != nil {
		return Workload{}, err
	}
	w, err := scan(s.pool.QueryRow(ctx, "INSERT INTO workloads (spiffe_id, owner, environment) VALUES ($1, $2, $3) RETURNING "+columns, in.SpiffeID, in.Owner, in.Environment))
	if isDuplicate(err) {
		return Workload{}, ErrExists
	}
	return w, err
}

// List gives the enrolled workloads, newest first.
func (s *Store) List(ctx context.Context) ([]Workload, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM workloads WHERE revoked_at IS NULL ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workload
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// FindEnrolled finds the workload with the SPIFFE ID, if it is enrolled.
func (s *Store) FindEnrolled(ctx context.Context, spiffeID string) (Workload, error) {
	w, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM workloads WHERE spiffe_id = $1 AND revoked_at IS NULL", spiffeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Workload{}, ErrNotFound
	}
	return w, err
}

// Revoke ends the enrollment and gives the workload that was enrolled.
func (s *Store) Revoke(ctx context.Context, id string) (Workload, error) {
	w, err := scan(s.pool.QueryRow(ctx, "UPDATE workloads SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL RETURNING "+columns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Workload{}, ErrNotFound
	}
	return w, err
}
