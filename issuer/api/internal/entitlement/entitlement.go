// Package entitlement keeps the lasting right of one subject, a person (by email) or a workload (by SPIFFE ID), to one credential.
package entitlement

import (
	"context"
	"encoding/json"
	"errors"
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
	// Subject is who holds the right.
	Subject Subject
	// Type is the credential type to issue.
	Type   string
	Claims map[string]string
	// ValidUntil is when the entitlement stops counting; nil means it does not end.
	ValidUntil *time.Time
	Status     Status
	CreatedAt  time.Time
	ClaimedAt  *time.Time
	IssuedAt   *time.Time
	// StatusIndex is the credential's position in the status list; nil until the entitlement is first claimed.
	StatusIndex *int64
}

type New struct {
	Subject    Subject
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

// inserts records an entitlement per kind of subject; one that is waiting with the same subject, type and data is returned instead.
var inserts = map[SubjectKind]string{Person: insert("email"), Workload: insert("workload")}

func insert(column string) string {
	return "INSERT INTO entitlements (id, " + column + ", type, claims, valid_until) VALUES (gen_random_uuid(), $1, $2, $3, $4) " +
		"ON CONFLICT (" + column + ", type, claims) WHERE " + column + " IS NOT NULL AND status IN ('open', 'claimed') " +
		"DO UPDATE SET " + column + " = EXCLUDED." + column + " RETURNING " + columns
}

const columns = "id, COALESCE(email, ''), COALESCE(workload, ''), type, claims, valid_until, status, created_at, claimed_at, issued_at, status_index"

func scan(row pgx.Row) (Entitlement, error) {
	var e Entitlement
	var claims []byte
	var email, spiffeID string
	err := row.Scan(&e.ID, &email, &spiffeID, &e.Type, &claims, &e.ValidUntil, &e.Status, &e.CreatedAt, &e.ClaimedAt, &e.IssuedAt, &e.StatusIndex)
	if err != nil {
		return Entitlement{}, err
	}
	e.Subject = subjectOf(email, spiffeID)
	return e, json.Unmarshal(claims, &e.Claims)
}

func scanAll(rows pgx.Rows) ([]Entitlement, error) {
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

// Create records the entitlement. A subject has one unissued entitlement per type and data: asking again, also at the same moment, gives the one there is.
func (s *Store) Create(ctx context.Context, n New) (Entitlement, error) {
	return create(ctx, s.pool, n)
}

func create(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, n New) (Entitlement, error) {
	if err := n.Subject.Validate(); err != nil {
		return Entitlement{}, err
	}
	claims, err := json.Marshal(n.Claims)
	if err != nil {
		return Entitlement{}, err
	}
	return scan(q.QueryRow(ctx, inserts[n.Subject.Kind], n.Subject.key(), n.Type, claims, n.ValidUntil))
}

func (s *Store) Find(ctx context.Context, id string) (Entitlement, error) {
	e, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM entitlements WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entitlement{}, ErrNotFound
	}
	return e, err
}

// FindOpenFor returns the subject's entitlements that can still be claimed, oldest first.
func (s *Store) FindOpenFor(ctx context.Context, subject Subject) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+columns+" FROM entitlements WHERE "+subject.column()+" = $2 AND "+claimable+" ORDER BY created_at",
		s.lease.Seconds(), subject.key())
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// FindIssuedFor returns the credentials issued to the subject that have not ended, oldest first.
func (s *Store) FindIssuedFor(ctx context.Context, subject Subject) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM entitlements WHERE "+subject.column()+" = $1 AND status = 'issued' AND (valid_until IS NULL OR valid_until > now()) ORDER BY created_at", subject.key())
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// FindActiveFor returns the subject's entitlements that were not revoked and have not ended, issued or not, oldest first.
func (s *Store) FindActiveFor(ctx context.Context, subject Subject) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM entitlements WHERE "+subject.column()+" = $1 AND status IN ('open', 'claimed', 'issued') AND (valid_until IS NULL OR valid_until > now()) ORDER BY created_at", subject.key())
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// FindActive returns the entitlements of every person that were not revoked and have not ended, issued or not, newest first.
func (s *Store) FindActive(ctx context.Context) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM entitlements WHERE email IS NOT NULL AND status IN ('open', 'claimed', 'issued') AND (valid_until IS NULL OR valid_until > now()) ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// Claim takes an entitlement that can be claimed, so that no one else can take it while the credential is made, and gives it its status index if it has none.
// It reports false when it cannot be claimed.
func (s *Store) Claim(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'claimed', claimed_at = now(), status_index = COALESCE(status_index, nextval('status_index_seq')) WHERE id = $2 AND "+claimable, s.lease.Seconds(), id)
	return tag.RowsAffected() == 1, err
}

// MarkIssued records that the credential was issued. It reports false unless the entitlement is claimed.
func (s *Store) MarkIssued(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'issued', issued_at = now() WHERE id = $1 AND status = 'claimed'", id)
	return tag.RowsAffected() == 1, err
}

// Replace withdraws an entitlement and records n in its place, or neither. It gives ErrNotFound when there is nothing to withdraw.
func (s *Store) Replace(ctx context.Context, id string, n New) (Entitlement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Entitlement{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE id = $1 AND status IN ('open', 'claimed', 'issued')", id)
	if err != nil {
		return Entitlement{}, err
	}
	if tag.RowsAffected() != 1 {
		return Entitlement{}, ErrNotFound
	}
	e, err := create(ctx, tx, n)
	if err != nil {
		return Entitlement{}, err
	}
	return e, tx.Commit(ctx)
}

// Revoke withdraws an entitlement, issued or not. It reports false when it was withdrawn already or does not exist.
func (s *Store) Revoke(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE id = $1 AND status IN ('open', 'claimed', 'issued')", id)
	return tag.RowsAffected() == 1, err
}

// RevokeAll withdraws every entitlement of the subject that is not withdrawn yet, so the credentials made from them show as revoked. It gives how many.
func (s *Store) RevokeAll(ctx context.Context, subject Subject) (int64, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE "+subject.column()+" = $1 AND status IN ('open', 'claimed', 'issued')", subject.key())
	return tag.RowsAffected(), err
}

// RevokeType withdraws the subject's entitlements of one credential type that are not withdrawn yet. It gives how many.
func (s *Store) RevokeType(ctx context.Context, subject Subject, credentialType string) (int64, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE "+subject.column()+" = $1 AND type = $2 AND status IN ('open', 'claimed', 'issued')", subject.key(), credentialType)
	return tag.RowsAffected(), err
}

// StatusIndexes gives the status index of every revoked credential, and how many positions the list needs (one past the highest index given out).
func (s *Store) StatusIndexes(ctx context.Context) (revoked []int64, size int64, err error) {
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(max(status_index) + 1, 0) FROM entitlements").Scan(&size); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, "SELECT status_index FROM entitlements WHERE status = 'revoked' AND status_index IS NOT NULL")
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var i int64
		if err := rows.Scan(&i); err != nil {
			return nil, 0, err
		}
		revoked = append(revoked, i)
	}
	return revoked, size, rows.Err()
}
