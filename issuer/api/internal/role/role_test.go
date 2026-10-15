package role_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"attesta/issuer/internal/role"
)

func openPool(t *testing.T) (*role.Store, *pgxpool.Pool) {
	t.Helper()
	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return role.NewStore(pool), pool
}

func open(t *testing.T) (*role.Store, func(string) error) {
	t.Helper()
	store, pool := openPool(t)
	return store, func(name string) error {
		_, err := pool.Exec(context.Background(), "INSERT INTO roles (name) VALUES ($1)", name)
		return err
	}
}

// hold gives a person an Employee entitlement with these roles, as the service records it.
func hold(t *testing.T, pool *pgxpool.Pool, status, roles string) {
	t.Helper()
	claims, _ := json.Marshal(map[string]string{"roles": roles})
	_, err := pool.Exec(context.Background(),
		"INSERT INTO entitlements (id, email, type, claims, status) VALUES (gen_random_uuid(), $1, 'Employee', $2, $3)", uuid.NewString()+"@example.com", claims, status)
	if err != nil {
		t.Fatal(err)
	}
}

func unique(prefix string) string { return prefix + uuid.NewString() }

func TestCreatesRenamesAndSoftDeletesARoleNobodyHolds(t *testing.T) {
	ctx := context.Background()
	store, pool := openPool(t)
	name := unique("a-")

	created, err := store.Create(ctx, "  "+name+" ")
	if err != nil || created.Name != name || created.InUse {
		t.Fatalf("%+v %v", created, err)
	}
	renamed, err := store.Rename(ctx, created.ID, name+"-2")
	if err != nil || renamed.Name != name+"-2" || !renamed.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("%+v %v", renamed, err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	names, _ := store.Names(ctx)
	if slices.Contains(names, name+"-2") {
		t.Fatalf("a deleted role is listed: %v", names)
	}
	var deleted *time.Time
	if err := pool.QueryRow(ctx, "SELECT deleted_at FROM roles WHERE id = $1", created.ID).Scan(&deleted); err != nil || deleted == nil {
		t.Fatalf("the row is gone or not marked: %v %v", deleted, err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, role.ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	if _, err := store.Create(ctx, name+"-2"); err != nil {
		t.Fatalf("the name of a deleted role cannot be used again: %v", err)
	}
}

func TestRefusesToDeleteARoleAnEmployeeHoldsButRenamesIt(t *testing.T) {
	ctx := context.Background()
	store, pool := openPool(t)
	name := unique("held-")
	r, _ := store.Create(ctx, name)
	hold(t, pool, "issued", "other-role, "+name)

	if err := store.Delete(ctx, r.ID); !errors.Is(err, role.ErrInUse) {
		t.Fatalf("delete: %v", err)
	}
	list, _ := store.List(ctx)
	for _, l := range list {
		if l.ID == r.ID && !l.InUse {
			t.Fatal("the list does not say the role is in use")
		}
	}
	renamed, err := store.Rename(ctx, r.ID, name+"-2")
	if err != nil || renamed.ID != r.ID || renamed.Name != name+"-2" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
}

func TestARoleOnlyARevokedCredentialHoldsCanBeDeleted(t *testing.T) {
	ctx := context.Background()
	store, pool := openPool(t)
	name := unique("gone-")
	r, _ := store.Create(ctx, name)
	hold(t, pool, "revoked", name)
	hold(t, pool, "issued", name+"-longer")

	if err := store.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesADuplicateAnInvalidNameAndAnUnknownRole(t *testing.T) {
	ctx := context.Background()
	store, _ := openPool(t)
	name := unique("dup-")
	r, _ := store.Create(ctx, name)

	if _, err := store.Create(ctx, name); !errors.Is(err, role.ErrExists) {
		t.Errorf("create twice: %v", err)
	}
	other, _ := store.Create(ctx, name+"-x")
	if _, err := store.Rename(ctx, other.ID, name); !errors.Is(err, role.ErrExists) {
		t.Errorf("rename to a taken name: %v", err)
	}
	for _, bad := range []string{"", "  ", "a,b"} {
		if _, err := store.Create(ctx, bad); !errors.Is(err, role.ErrInvalid) {
			t.Errorf("create %q: %v", bad, err)
		}
		if _, err := store.Rename(ctx, r.ID, bad); !errors.Is(err, role.ErrInvalid) {
			t.Errorf("rename to %q: %v", bad, err)
		}
	}
	for _, id := range []string{"nope", uuid.NewString()} {
		if err := store.Delete(ctx, id); !errors.Is(err, role.ErrNotFound) {
			t.Errorf("delete %q: %v", id, err)
		}
	}
}

func TestListsTheRolesInTheTableInOrderStartingWithTheOneLaadpalenNeeds(t *testing.T) {
	roles, add := open(t)
	name := "zz-" + uuid.NewString()
	if err := add(name); err != nil {
		t.Fatal(err)
	}

	names, err := roles.Names(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(names) || !slices.Contains(names, "laadpalen-aanvrager") || slices.Contains(names, "reader") || !slices.Contains(names, name) {
		t.Fatalf("roles %v", names)
	}
}

func TestTheTableRefusesANameThatCannotBeKeptInAList(t *testing.T) {
	_, add := open(t)
	for _, name := range []string{"", " padded ", "a,b"} {
		if err := add(name); err == nil {
			t.Errorf("the role %q was accepted", name)
		}
	}
}
