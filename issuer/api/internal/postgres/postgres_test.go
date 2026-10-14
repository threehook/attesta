package postgres_test

import (
	"context"
	"sync"
	"testing"

	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
)

func TestCreatesTheDatabaseAndMigratesOnlyOnce(t *testing.T) {
	ctx := context.Background()
	o, name := postgrestest.Server(t), postgrestest.DatabaseName()

	for range 2 {
		pool, err := postgres.Open(ctx, o, name)
		if err != nil {
			t.Fatal(err)
		}
		var migrated int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrated); err != nil {
			t.Fatal(err)
		}
		if migrated != 3 {
			t.Fatalf("%d migrations recorded, want 3", migrated)
		}
		pool.Close()
	}
}

func TestSeveralReplicasCanStartTogether(t *testing.T) {
	o, name := postgrestest.Server(t), postgrestest.DatabaseName()
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool, err := postgres.Open(context.Background(), o, name)
			if err == nil {
				pool.Close()
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
