// Package postgrestest gives tests a real Postgres in a container.
package postgrestest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"attesta/issuer/internal/postgres"
)

var (
	once      sync.Once
	options   postgres.Options
	startErr  error
	databases atomic.Int64
)

// Server starts one Postgres per test binary, which the container reaper removes afterwards, and skips the test when Docker is not available.
func Server(t *testing.T) postgres.Options {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	once.Do(func() {
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test"), tcpostgres.BasicWaitStrategies())
		if err != nil {
			startErr = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			startErr = err
			return
		}
		port, err := c.MappedPort(ctx, "5432/tcp")
		if err != nil {
			startErr = err
			return
		}
		options = postgres.Options{Host: host + ":" + port.Port(), User: "postgres", Password: "test"}
	})
	if startErr != nil {
		t.Fatalf("start postgres: %v", startErr)
	}
	return options
}

// DatabaseName is unique within the test binary, so tests do not share data.
func DatabaseName() string {
	return fmt.Sprintf("test_%d", databases.Add(1))
}
