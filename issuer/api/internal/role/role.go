// Package role keeps the roles staff can give an employee.
package role

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("de rol bestaat niet")
	ErrExists   = errors.New("er is al een rol met deze naam")
	ErrInUse    = errors.New("de rol is in gebruik bij een medewerker en kan niet worden verwijderd")
	ErrInvalid  = errors.New("een rolnaam is niet leeg en bevat geen komma")
)

// Role is a role that has not been deleted. InUse is true while a credential of an employee that is not revoked holds it.
type Role struct {
	ID        string
	Name      string
	InUse     bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Store struct{ pool *pgxpool.Pool }

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// An employee's roles are kept as one text separated by commas.
const inUse = `EXISTS (SELECT 1 FROM entitlements e WHERE e.type = 'Employee' AND e.status <> 'revoked'
  AND roles.name = ANY(regexp_split_to_array(e.claims->>'roles', '\s*,\s*')))`

const columns = "id, name, " + inUse + ", created_at, updated_at"

func scan(row pgx.Row) (Role, error) {
	var r Role

	err := row.Scan(&r.ID, &r.Name, &r.InUse, &r.CreatedAt, &r.UpdatedAt)

	return r, err
}

func clean(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, ",") {
		return "", ErrInvalid
	}

	return name, nil
}

// Names lists the names of the roles, in alphabetical order.
func (s *Store) Names(ctx context.Context) ([]string, error) {
	roles, err := s.List(ctx)

	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = r.Name
	}

	return names, err
}

// List lists the roles, in alphabetical order.
func (s *Store) List(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM roles WHERE deleted_at IS NULL ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []Role{}

	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}

		roles = append(roles, r)
	}

	return roles, rows.Err()
}

func isDuplicate(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func (s *Store) Create(ctx context.Context, name string) (Role, error) {
	name, err := clean(name)
	if err != nil {
		return Role{}, err
	}

	r, err := scan(s.pool.QueryRow(ctx, "INSERT INTO roles (name) VALUES ($1) RETURNING "+columns, name))
	if isDuplicate(err) {
		return Role{}, ErrExists
	}

	return r, err
}

// Rename changes the name of a role; credentials already issued keep the name they were issued with.
func (s *Store) Rename(ctx context.Context, id, name string) (Role, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Role{}, ErrNotFound
	}

	name, err := clean(name)
	if err != nil {
		return Role{}, err
	}

	r, err := scan(
		s.pool.QueryRow(ctx, "UPDATE roles SET name = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING "+columns, id, name),
	)
	switch {
	case isDuplicate(err):
		return Role{}, ErrExists
	case errors.Is(err, pgx.ErrNoRows):
		return Role{}, ErrNotFound
	}

	return r, err
}

// Delete softly deletes a role that no employee holds.
func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}

	tag, err := s.pool.Exec(ctx, "UPDATE roles SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL AND NOT "+inUse, id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return s.whyNot(ctx, id)
	}

	return nil
}

// whyNot says why a change to the role with this id matched nothing.
func (s *Store) whyNot(ctx context.Context, id string) error {
	var held bool

	err := s.pool.QueryRow(ctx, "SELECT "+inUse+" FROM roles WHERE id = $1 AND deleted_at IS NULL", id).Scan(&held)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	case held:
		return ErrInUse
	}

	return ErrNotFound
}
