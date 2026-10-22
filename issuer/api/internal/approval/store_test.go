package approval_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"attesta/issuer/internal/approval"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
)

const agent = "spiffe://corp.test/ns/cicd/sa/agent"

func start(t *testing.T) *approval.Store {
	t.Helper()
	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return approval.NewStore(pool)
}

var shop = map[string]string{"application": "shop", "version": "v2.0.0"}

func TestOpenGivesTheSameRequestWhileItWaits(t *testing.T) {
	s, ctx := start(t), context.Background()
	r, created, err := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour)
	if err != nil || !created || r.State != approval.Open || r.Action["version"] != "v2.0.0" || len(r.Approvers) != 0 {
		t.Fatalf("Open = %+v, %v, %v", r, created, err)
	}
	again, created, err := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour)
	if err != nil || created || again.ID != r.ID {
		t.Fatalf("Open again = %+v, %v, %v; want the same request", again, created, err)
	}
	other, created, err := s.Open(ctx, agent, "deploy:prod:v2.1.0", shop, time.Hour)
	if err != nil || !created || other.ID == r.ID {
		t.Errorf("another resource = %+v, %v, %v", other, created, err)
	}
	if list, err := s.List(ctx); err != nil || len(list) != 2 {
		t.Errorf("List = %d, %v", len(list), err)
	}
}

func TestApproveRejectAndDeliver(t *testing.T) {
	s, ctx := start(t), context.Background()
	r, _, _ := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour)

	if ok, err := s.Deliver(ctx, r.ID, 2); err != nil || ok {
		t.Fatalf("Deliver without approvals = %v, %v", ok, err)
	}
	if r, err := s.Approve(ctx, r.ID, "anna@corp.test"); err != nil || !slices.Equal(r.Approvers, []string{"anna@corp.test"}) {
		t.Fatalf("Approve = %+v, %v", r, err)
	}
	if r, err := s.Approve(ctx, r.ID, "anna@corp.test"); err != nil || len(r.Approvers) != 1 {
		t.Fatalf("approving twice = %+v, %v; it must change nothing", r, err)
	}
	if ok, _ := s.Deliver(ctx, r.ID, 2); ok {
		t.Fatal("one person delivered a request that needs two")
	}
	s.Approve(ctx, r.ID, "bob@corp.test")
	if ok, err := s.Deliver(ctx, r.ID, 2); err != nil || !ok {
		t.Fatalf("Deliver = %v, %v", ok, err)
	}
	if ok, _ := s.Deliver(ctx, r.ID, 2); ok {
		t.Error("a request was handed out twice")
	}
	if got, _ := s.Get(ctx, r.ID); got.State != approval.Delivered {
		t.Errorf("state = %s, want delivered", got.State)
	}
	if _, err := s.Approve(ctx, r.ID, "carol@corp.test"); !errors.Is(err, approval.ErrNotFound) {
		t.Errorf("approving a handed out request gave %v, want ErrNotFound", err)
	}
	if list, _ := s.List(ctx); len(list) != 0 {
		t.Errorf("a handed out request is still listed: %+v", list)
	}
}

func TestRejectEndsTheRequestAndMakesRoomForANewOne(t *testing.T) {
	s, ctx := start(t), context.Background()
	r, _, _ := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour)
	if err := s.Reject(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, r.ID); got.State != approval.Rejected {
		t.Errorf("state = %s, want rejected", got.State)
	}
	if err := s.Reject(ctx, r.ID); !errors.Is(err, approval.ErrNotFound) {
		t.Errorf("rejecting twice gave %v, want ErrNotFound", err)
	}
	next, created, err := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour)
	if err != nil || !created || next.ID == r.ID {
		t.Errorf("a new request after a rejection = %+v, %v, %v", next, created, err)
	}
}

func TestARequestRunsOut(t *testing.T) {
	s, ctx := start(t), context.Background()
	r, _, _ := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, -time.Minute)
	if got, _ := s.Get(ctx, r.ID); got.State != approval.Expired {
		t.Fatalf("state = %s, want expired", got.State)
	}
	if list, _ := s.List(ctx); len(list) != 0 {
		t.Errorf("an expired request is listed: %+v", list)
	}
	if ok, _ := s.Deliver(ctx, r.ID, 0); ok {
		t.Error("an expired request was handed out")
	}
	if _, created, err := s.Open(ctx, agent, "deploy:prod:v2.0.0", shop, time.Hour); err != nil || !created {
		t.Errorf("a new request after it ran out: %v, %v", created, err)
	}
}

func TestARequestNeedsAResource(t *testing.T) {
	s := start(t)
	if _, _, err := s.Open(context.Background(), agent, "", nil, time.Hour); !errors.Is(err, approval.ErrInvalid) {
		t.Errorf("an empty resource gave %v, want ErrInvalid", err)
	}
}
