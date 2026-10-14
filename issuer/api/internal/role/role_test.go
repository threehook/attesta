package role_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"attesta/issuer/internal/role"
)

func open(t *testing.T) (*role.Store, func(string) error) {
	t.Helper()
	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return role.NewStore(pool), func(name string) error {
		_, err := pool.Exec(context.Background(), "INSERT INTO roles (name) VALUES ($1)", name)
		return err
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
