// Package credtype keeps the credential types staff define besides Employee.
package credtype

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"attesta/issuer/internal/credential"
)

var (
	ErrNotFound = errors.New("het credentialtype bestaat niet")
	ErrExists   = errors.New("er is al een credentialtype met deze naam")
	ErrInUse    = errors.New("het credentialtype is in gebruik: de velden kunnen niet worden gewijzigd en het kan niet worden verwijderd")
)

// InvalidError is a credential type that cannot be kept; its message says why.
type InvalidError struct{ Message string }

func (e InvalidError) Error() string { return e.Message }

// Type is a credential type that has not been deleted. InUse is true while a credential of the type that is not revoked exists.
type Type struct {
	credential.Type
	ID        string
	InUse     bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input is what staff give: the type and the path follow from the title when the type is created and do not change.
type Input struct {
	Title       string
	Description string
	Fields      []credential.Field
}

type Store struct{ pool *pgxpool.Pool }

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const inUse = `EXISTS (SELECT 1 FROM entitlements e WHERE e.type = credential_types.type AND e.status <> 'revoked')`

const columns = "id, type, path, title, description, fields, " + inUse + ", created_at, updated_at"

func scan(row pgx.Row) (Type, error) {
	var t Type
	var fields []byte
	if err := row.Scan(&t.ID, &t.Type.Type, &t.Path, &t.Title, &t.Description, &fields, &t.InUse, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Type{}, err
	}
	return t, json.Unmarshal(fields, &t.Fields)
}

// clean trims what staff typed and checks it.
func clean(in Input) (Input, error) {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	fields := make([]credential.Field, len(in.Fields))
	for i, f := range in.Fields {
		f.Key, f.Label = strings.TrimSpace(f.Key), strings.TrimSpace(f.Label)
		f.Min, f.Max = strings.ReplaceAll(strings.TrimSpace(f.Min), ",", "."), strings.ReplaceAll(strings.TrimSpace(f.Max), ",", ".")
		if f.Kind != credential.KindNumber {
			f.Min, f.Max, f.Decimals = "", "", 0
		}
		if f.Kind != credential.KindChoice {
			f.Options = nil
		}
		fields[i] = f
	}
	in.Fields = fields
	if err := (credential.Type{Title: in.Title, Description: in.Description, Fields: in.Fields}).Check(); err != nil {
		return Input{}, InvalidError{err.Error()}
	}
	return in, nil
}

func isDuplicate(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// List lists the types, in alphabetical order of their titles.
func (s *Store) List(ctx context.Context) ([]Type, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+columns+" FROM credential_types WHERE deleted_at IS NULL ORDER BY lower(title), id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := []Type{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

// Find finds the credential type by the name verifiers know it by; Employee is always there.
func (s *Store) Find(ctx context.Context, typeName string) (credential.Type, error) {
	if typeName == credential.Employee {
		return credential.EmployeeType, nil
	}
	t, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM credential_types WHERE type = $1 AND deleted_at IS NULL", typeName))
	if errors.Is(err, pgx.ErrNoRows) {
		return credential.Type{}, ErrNotFound
	}
	return t.Type, err
}

// FindByPath finds the type whose form and entitlements live at the path.
func (s *Store) FindByPath(ctx context.Context, path string) (Type, error) {
	t, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM credential_types WHERE path = $1 AND deleted_at IS NULL", path))
	if errors.Is(err, pgx.ErrNoRows) {
		return Type{}, ErrNotFound
	}
	return t, err
}

// Names lists the credential types wallets can get: Employee first, then the others.
func (s *Store) Names(ctx context.Context) ([]string, error) {
	types, err := s.List(ctx)
	names := []string{credential.Employee}
	for _, t := range types {
		names = append(names, t.Type.Type)
	}
	return names, err
}

// Create makes a type; its name (the credential type) is its title and its path follows from the title. Neither changes later.
func (s *Store) Create(ctx context.Context, in Input) (Type, error) {
	in, err := clean(in)
	if err != nil {
		return Type{}, err
	}
	path := credential.Slug(in.Title)
	if path == "" || path == credential.EmployeeType.Path || strings.EqualFold(in.Title, credential.Employee) {
		return Type{}, InvalidError{"de titel bevat letters of cijfers en is niet Employee"}
	}
	fields, _ := json.Marshal(in.Fields)
	t, err := scan(s.pool.QueryRow(ctx, "INSERT INTO credential_types (type, path, title, description, fields) VALUES ($1, $2, $1, $3, $4::jsonb) RETURNING "+columns,
		in.Title, path, in.Description, fields))
	if isDuplicate(err) {
		return Type{}, ErrExists
	}
	return t, err
}

// Update changes the title and description, and the fields too as long as no credential of the type is in use.
func (s *Store) Update(ctx context.Context, id string, in Input) (Type, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Type{}, ErrNotFound
	}
	in, err := clean(in)
	if err != nil {
		return Type{}, err
	}
	fields, _ := json.Marshal(in.Fields)
	t, err := scan(s.pool.QueryRow(ctx, `UPDATE credential_types SET title = $2, description = $3, fields = $4::jsonb, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL AND (fields = $4::jsonb OR NOT `+inUse+`) RETURNING `+columns, id, in.Title, in.Description, fields))
	if errors.Is(err, pgx.ErrNoRows) {
		return Type{}, s.whyNot(ctx, id)
	}
	return t, err
}

// Delete softly deletes a type no credential uses.
func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, "UPDATE credential_types SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL AND NOT "+inUse, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return s.whyNot(ctx, id)
	}
	return nil
}

// whyNot says why a change to the type with this id matched nothing.
func (s *Store) whyNot(ctx context.Context, id string) error {
	var held bool
	err := s.pool.QueryRow(ctx, "SELECT "+inUse+" FROM credential_types WHERE id = $1 AND deleted_at IS NULL", id).Scan(&held)
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
