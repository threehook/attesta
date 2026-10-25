// Package approval parks an action of a workload that no mandate covers. People approve the request, and then the workload takes one Approval
// credential for exactly that action.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("there is no such request")
	ErrInvalid  = errors.New("a request has a resource and an action of a few texts by name")
)

// State is where a request is.
type State string

const (
	Open      State = "open"
	Delivered State = "delivered"
	Rejected  State = "rejected"
	Expired   State = "expired"
)

const maxResource = 200

// Request is an action that waits for approval.
type Request struct {
	ID        string
	Workload  string
	Resource  string
	Action    map[string]string
	CreatedAt time.Time
	ExpiresAt time.Time
	Approvers []string
	State     State
}

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool, now: time.Now} }

const selectRequests = `SELECT r.id, r.workload, r.resource, r.action, r.created_at, r.expires_at,
  COALESCE(array_agg(a.approver ORDER BY a.approved_at, a.approver) FILTER (WHERE a.approver IS NOT NULL), '{}'),
  CASE WHEN r.delivered_at IS NOT NULL THEN 'delivered' WHEN r.rejected_at IS NOT NULL THEN 'rejected'
    WHEN r.expires_at <= now() THEN 'expired' ELSE 'open' END
  FROM approval_requests r LEFT JOIN approval_request_approvals a ON a.request_id = r.id`

const openOnly = " WHERE r.rejected_at IS NULL AND r.delivered_at IS NULL AND r.expires_at > now()"

func scan(row pgx.Row) (Request, error) {
	var (
		r      Request
		action []byte
		state  string
	)
	if err := row.Scan(&r.ID, &r.Workload, &r.Resource, &action, &r.CreatedAt, &r.ExpiresAt, &r.Approvers, &state); err != nil {
		return Request{}, err
	}

	r.State = State(state)

	return r, json.Unmarshal(action, &r.Action)
}

// Open records the request, or gives the one that waits already for this workload and resource. It also says whether it is new.
func (s *Store) Open(ctx context.Context, workload, resource string, action map[string]string, lifetime time.Duration) (Request, bool, error) {
	if resource == "" || len([]rune(resource)) > maxResource {
		return Request{}, false, ErrInvalid
	}

	if action == nil {
		action = map[string]string{}
	}

	encoded, err := json.Marshal(action)
	if err != nil {
		return Request{}, false, ErrInvalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "UPDATE approval_requests SET rejected_at = now() WHERE workload = $1 AND resource = $2 AND rejected_at IS NULL AND "+
		"delivered_at IS NULL AND expires_at <= now()", workload, resource); err != nil {
		return Request{}, false, err
	}

	var id string

	created := true

	err = tx.QueryRow(ctx,
		"INSERT INTO approval_requests (workload, resource, action, expires_at) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING RETURNING id",
		workload, resource, encoded, s.now().Add(lifetime)).
		Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		err = tx.QueryRow(ctx,
			"SELECT id FROM approval_requests WHERE workload = $1 AND resource = $2 AND rejected_at IS NULL AND delivered_at IS NULL",
			workload, resource).
			Scan(&id)
	}

	if err != nil {
		return Request{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Request{}, false, err
	}

	r, err := s.Get(ctx, id)

	return r, created, err
}

// Get gives a request in any state.
func (s *Store) Get(ctx context.Context, id string) (Request, error) {
	r, err := scan(s.pool.QueryRow(ctx, selectRequests+" WHERE r.id = $1 GROUP BY r.id", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}

	return r, err
}

// List gives the requests that wait, newest first.
func (s *Store) List(ctx context.Context) ([]Request, error) {
	rows, err := s.pool.Query(ctx, selectRequests+openOnly+" GROUP BY r.id ORDER BY r.created_at DESC, r.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Request

	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// Approve adds a person to the approvers of a request that waits. Approving twice changes nothing.
func (s *Store) Approve(ctx context.Context, id, approver string) (Request, error) {
	if _, err := s.pool.Exec(ctx,
		"INSERT INTO approval_request_approvals (request_id, approver) SELECT r.id, $2 FROM approval_requests r"+openOnly+
			" AND r.id = $1 ON CONFLICT DO NOTHING", id, approver); err != nil {
		return Request{}, err
	}

	r, err := s.Get(ctx, id)
	if err == nil && r.State != Open {
		return Request{}, ErrNotFound
	}

	return r, err
}

// Reject ends a request that waits.
func (s *Store) Reject(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, "UPDATE approval_requests r SET rejected_at = now()"+openOnly+" AND r.id = $1", id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return err
}

// Deliver marks an open request as handed out, once, when at least `required` people approved it. It reports false when it is not so.
func (s *Store) Deliver(ctx context.Context, id string, required int) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE approval_requests r SET delivered_at = now()`+openOnly+` AND r.id = $1
	  AND (SELECT count(*) FROM approval_request_approvals a WHERE a.request_id = r.id) >= $2`, id, required)

	return tag.RowsAffected() == 1, err
}
