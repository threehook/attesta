// Package role keeps the roles staff can give an employee.
package role

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

// NewStore uses a pool whose database has been opened with postgres.Open.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Names lists the roles, in alphabetical order.
func (s *Store) Names(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT name FROM roles ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
