// Package postgres opens the issuer's databases: it creates a missing one and applies the SQL migrations that have not run yet.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Advisory locks keep replicas that start together from creating the database, or migrating it, at the same moment.
const (
	createLock  = 7010
	migrateLock = 7011
)

type Options struct {
	// Host is `host` or `host:port`.
	Host     string
	User     string
	Password string
}

func (o Options) connString(database string) string {
	host := o.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "5432")
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(o.User, o.Password), Host: host, Path: "/" + database}
	return u.String()
}

// Open connects to the database, creating it and bringing its tables up to date when needed.
func Open(ctx context.Context, o Options, database string) (*pgxpool.Pool, error) {
	if err := create(ctx, o, database); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, o.connString(database))
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func create(ctx context.Context, o Options, database string) error {
	admin, err := pgx.Connect(ctx, o.connString("postgres"))
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", createLock); err != nil {
		return err
	}
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", database).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize())
	return err
}

// migrate applies the migrations in name order, each once.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLock); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrateLock)

	if _, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	rows, err := conn.Query(ctx, "SELECT name FROM schema_migrations")
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		done[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if done[name] {
			continue
		}
		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := apply(ctx, conn, name, string(script)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgxpool.Conn, name, script string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, script); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
