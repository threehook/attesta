package mandate_test

import (
	"attesta/issuer/internal/mandate"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

const agent = "spiffe://corp.test/ns/cicd/sa/agent"

func start(t *testing.T) *mandate.Store {
	t.Helper()

	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return mandate.NewStore(pool)
}

func in() mandate.Input {
	return mandate.Input{Workload: agent, Resource: "deploy:prod:v1.*", ExpiresAt: time.Now().Add(24 * time.Hour)}
}

func TestCreateApproveAndFind(t *testing.T) {
	s, ctx := start(t), context.Background()

	m, err := s.Create(ctx, in(), "anna@corp.test")
	if err != nil || !slices.Equal(m.Approvers, []string{"anna@corp.test"}) {
		t.Fatalf("Create = %+v, %v", m, err)
	}

	if m, err = s.Approve(ctx, m.ID, "bob@corp.test"); err != nil || !slices.Equal(m.Approvers, []string{"anna@corp.test", "bob@corp.test"}) {
		t.Fatalf("Approve = %+v, %v", m, err)
	}

	if m, err = s.Approve(ctx, m.ID, "bob@corp.test"); err != nil || len(m.Approvers) != 2 {
		t.Fatalf("approving twice = %+v, %v; it must change nothing", m, err)
	}

	found, err := s.ActiveFor(ctx, agent)
	if err != nil || found.ID != m.ID || found.Resource != "deploy:prod:v1.*" || len(found.Approvers) != 2 {
		t.Fatalf("ActiveFor = %+v, %v", found, err)
	}

	if list, err := s.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}
}

func TestAWorkloadHasOneMandateAtATime(t *testing.T) {
	s, ctx := start(t), context.Background()

	m, err := s.Create(ctx, in(), "anna@corp.test")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Create(ctx, in(), "bob@corp.test"); !errors.Is(err, mandate.ErrExists) {
		t.Fatalf("a second mandate gave %v, want ErrExists", err)
	}

	if _, err := s.End(ctx, m.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ActiveFor(ctx, agent); !errors.Is(err, mandate.ErrNotFound) {
		t.Errorf("an ended mandate is still active: %v", err)
	}

	if _, err := s.Approve(ctx, m.ID, "bob@corp.test"); !errors.Is(err, mandate.ErrNotFound) {
		t.Errorf("approving an ended mandate gave %v, want ErrNotFound", err)
	}

	if _, err := s.Create(ctx, in(), "bob@corp.test"); err != nil {
		t.Errorf("a new mandate after the old one ended: %v", err)
	}
}

func TestAMandateNeedsAResourceAndAnEndInTheNearFuture(t *testing.T) {
	s, ctx := start(t), context.Background()

	for name, edit := range map[string]func(*mandate.Input){
		"no resource":           func(i *mandate.Input) { i.Resource = " " },
		"a resource with space": func(i *mandate.Input) { i.Resource = "deploy prod" },
		"ended already":         func(i *mandate.Input) { i.ExpiresAt = time.Now().Add(-time.Minute) },
		"too far away":          func(i *mandate.Input) { i.ExpiresAt = time.Now().Add(60 * 24 * time.Hour) },
	} {
		i := in()
		edit(&i)

		if _, err := s.Create(ctx, i, "anna@corp.test"); !errors.Is(err, mandate.ErrInvalid) {
			t.Errorf("%s gave %v, want ErrInvalid", name, err)
		}
	}
}
