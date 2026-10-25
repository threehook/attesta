package workload_test

import (
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"attesta/issuer/internal/workload"
	"context"
	"errors"
	"testing"
)

const agent = "spiffe://corp.test/ns/cicd/sa/agent"

func start(t *testing.T) *workload.Store {
	t.Helper()

	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return workload.NewStore(pool)
}

func TestEnrollFindAndList(t *testing.T) {
	s := start(t)
	ctx := context.Background()

	w, err := s.Enroll(ctx, workload.Input{SpiffeID: agent, Owner: "platform", Environment: "staging"})
	if err != nil {
		t.Fatal(err)
	}

	found, err := s.FindEnrolled(ctx, agent)
	if err != nil || found.ID != w.ID || found.Owner != "platform" || found.Environment != "staging" {
		t.Fatalf("FindEnrolled = %+v, %v", found, err)
	}

	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != w.ID {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if _, err := s.FindEnrolled(ctx, "spiffe://corp.test/ns/other/sa/x"); !errors.Is(err, workload.ErrNotFound) {
		t.Errorf("an unknown workload gave %v, want ErrNotFound", err)
	}
}

func TestAWorkloadIsEnrolledOnceAtATime(t *testing.T) {
	s := start(t)
	ctx := context.Background()
	in := workload.Input{SpiffeID: agent, Owner: "platform", Environment: "staging"}

	w, err := s.Enroll(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Enroll(ctx, in); !errors.Is(err, workload.ErrExists) {
		t.Errorf("a second enrollment gave %v, want ErrExists", err)
	}

	if _, err := s.Revoke(ctx, w.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.FindEnrolled(ctx, agent); !errors.Is(err, workload.ErrNotFound) {
		t.Errorf("a revoked workload is still enrolled: %v", err)
	}

	if _, err := s.Revoke(ctx, w.ID); !errors.Is(err, workload.ErrNotFound) {
		t.Errorf("revoking twice gave %v, want ErrNotFound", err)
	}

	if _, err := s.Enroll(ctx, in); err != nil {
		t.Errorf("a revoked SPIFFE ID could not be enrolled again: %v", err)
	}
}

func TestEnrollRefusesWhatIsNotAWorkload(t *testing.T) {
	s := start(t)

	long := make([]byte, 101)
	for i := range long {
		long[i] = 'x'
	}

	for name, in := range map[string]workload.Input{
		"not a SPIFFE ID":   {SpiffeID: "https://example.test", Owner: "o", Environment: "e"},
		"a trust domain":    {SpiffeID: "spiffe://corp.test", Owner: "o", Environment: "e"},
		"no owner":          {SpiffeID: agent, Owner: " ", Environment: "e"},
		"no environment":    {SpiffeID: agent, Owner: "o"},
		"an owner too long": {SpiffeID: agent, Owner: string(long), Environment: "e"},
	} {
		if _, err := s.Enroll(context.Background(), in); !errors.Is(err, workload.ErrInvalid) {
			t.Errorf("%s gave %v, want ErrInvalid", name, err)
		}
	}
}
