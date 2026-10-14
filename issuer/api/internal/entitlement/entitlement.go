// Package entitlement keeps the lasting right of one person (by email) to one credential.
package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	Open    Status = "open"
	Claimed Status = "claimed"
	Issued  Status = "issued"
	Revoked Status = "revoked"
)

// DefaultLease is how long a claim holds the entitlement while the credential is made; after that, if it was not issued, it counts as open again.
const DefaultLease = 2 * time.Minute

var ErrNotFound = errors.New("entitlement not found")

type Entitlement struct {
	ID string
	// Email is lowercase and identifies the person to the issuer.
	Email string
	// Type is the credential type to issue.
	Type   string
	Claims map[string]string
	// ValidUntil is when the entitlement stops counting; nil means it does not end.
	ValidUntil *time.Time
	Status     Status
	CreatedAt  time.Time
	ClaimedAt  *time.Time
	IssuedAt   *time.Time
}

type New struct {
	Email      string
	Type       string
	Claims     map[string]string
	ValidUntil *time.Time
}

type Store struct {
	pool  *pgxpool.Pool
	lease time.Duration
}

// NewStore uses a pool whose database has been opened with postgres.Open. A zero lease means DefaultLease.
func NewStore(pool *pgxpool.Pool, lease time.Duration) *Store {
	if lease == 0 {
		lease = DefaultLease
	}
	return &Store{pool: pool, lease: lease}
}

// claimable: open, or claimed longer ago than the lease, and not past its end. $1 is the lease in seconds.
const claimable = `(status = 'open' OR (status = 'claimed' AND claimed_at + make_interval(secs => $1::double precision) <= now()))
  AND (valid_until IS NULL OR valid_until > now())`

const columns = "id, email, type, claims, valid_until, status, created_at, claimed_at, issued_at"

func scan(row pgx.Row) (Entitlement, error) {
	var e Entitlement
	var claims []byte
	err := row.Scan(&e.ID, &e.Email, &e.Type, &claims, &e.ValidUntil, &e.Status, &e.CreatedAt, &e.ClaimedAt, &e.IssuedAt)
	if err != nil {
		return Entitlement{}, err
	}
	return e, json.Unmarshal(claims, &e.Claims)
}

// Create records the entitlement. A person has one unissued entitlement per type and data: asking again, also at the same moment, gives the one there is.
func (s *Store) Create(ctx context.Context, n New) (Entitlement, error) {
	claims, err := json.Marshal(n.Claims)
	if err != nil {
		return Entitlement{}, err
	}
	return scan(s.pool.QueryRow(ctx,
		`INSERT INTO entitlements (id, email, type, claims, valid_until) VALUES (gen_random_uuid(), $1, $2, $3, $4)
		 ON CONFLICT (email, type, claims) WHERE status IN ('open', 'claimed') DO UPDATE SET email = EXCLUDED.email RETURNING `+columns,
		strings.ToLower(n.Email), n.Type, claims, n.ValidUntil))
}

func (s *Store) Find(ctx context.Context, id string) (Entitlement, error) {
	e, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM entitlements WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entitlement{}, ErrNotFound
	}
	return e, err
}

// FindOpenByEmail returns the person's entitlements that can still be claimed, oldest first.
func (s *Store) FindOpenByEmail(ctx context.Context, email string) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+columns+" FROM entitlements WHERE email = $2 AND "+claimable+" ORDER BY created_at",
		s.lease.Seconds(), strings.ToLower(email))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entitlement
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Claim takes an entitlement that can be claimed, so that no one else can take it while the credential is made. It reports false when it cannot be claimed.
func (s *Store) Claim(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'claimed', claimed_at = now() WHERE id = $2 AND "+claimable, s.lease.Seconds(), id)
	return tag.RowsAffected() == 1, err
}

// MarkIssued records that the credential was issued. It reports false unless the entitlement is claimed.
func (s *Store) MarkIssued(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'issued', issued_at = now() WHERE id = $1 AND status = 'claimed'", id)
	return tag.RowsAffected() == 1, err
}

// Revoke withdraws an entitlement that was not issued yet. It reports false when it was.
func (s *Store) Revoke(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE id = $1 AND status IN ('open', 'claimed')", id)
	return tag.RowsAffected() == 1, err
}
