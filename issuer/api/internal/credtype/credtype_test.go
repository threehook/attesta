package credtype_test

import (
	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/credtype"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func open(t *testing.T) (*credtype.Store, *pgxpool.Pool) {
	t.Helper()

	pool, err := postgres.Open(context.Background(), postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return credtype.NewStore(pool), pool
}

func input(title string) credtype.Input {
	return credtype.Input{Title: title, Description: "d", Fields: []credential.Field{
		{Key: "email", Label: "E-mailadres", Kind: "email", Required: true},
		{Key: "colour", Label: "Kleur", Kind: "choice", Options: []string{"red", "blue"}},
	}}
}

// hold gives a person an entitlement of the type, as the service records it.
func hold(t *testing.T, pool *pgxpool.Pool, typeName, status string) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		"INSERT INTO entitlements (id, email, type, claims, status) VALUES (gen_random_uuid(), $1, $2, '{}', $3)",
		uuid.NewString()+"@example.com",
		typeName,
		status,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartsWithTheLaadpalenDiploma(t *testing.T) {
	store, _ := open(t)

	def, err := store.FindByPath(context.Background(), "laadpalen-management")
	if err != nil || def.Type.Type != "Diploma Laadpalen Management" || len(def.Fields) != 3 || def.Fields[2].Key != "validUntil" ||
		def.Fields[2].Kind != "date" {
		t.Fatalf("%+v %v", def, err)
	}

	found, err := store.Find(context.Background(), "Diploma Laadpalen Management")
	if err != nil || found.Title != "Diploma Laadpalen Management" {
		t.Fatalf("%+v %v", found, err)
	}
}

func TestCreatesAFindableTypeWhoseNameAndPathFollowFromTheTitle(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)
	title := "Bedrijfspas " + uuid.NewString()

	created, err := store.Create(ctx, input("  "+title+" "))
	if err != nil || created.Type.Type != title || created.Path != credential.Slug(title) || created.InUse || len(created.Fields) != 2 ||
		created.Fields[1].Options[1] != "blue" {
		t.Fatalf("%+v %v", created, err)
	}

	byPath, err := store.FindByPath(ctx, created.Path)
	if err != nil || byPath.ID != created.ID {
		t.Fatalf("%+v %v", byPath, err)
	}

	def, err := store.Find(ctx, title)
	if err != nil || def.Title != title {
		t.Fatalf("%+v %v", def, err)
	}

	names, err := store.Names(ctx)
	if err != nil || names[0] != "Employee" || !slices.Contains(names, title) {
		t.Fatalf("%v %v", names, err)
	}

	if _, err := store.Create(ctx, input(title)); !errors.Is(err, credtype.ErrExists) {
		t.Fatalf("the same title again: %v", err)
	}
}

func TestEmployeeIsAlwaysThereAndNeverMadeOrReplaced(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)

	def, err := store.Find(ctx, "Employee")
	if err != nil || def.Title != credential.EmployeeType.Title {
		t.Fatalf("%+v %v", def, err)
	}

	for _, title := range []string{"Employee", "employee", "???"} {
		var invalid credtype.InvalidError
		if _, err := store.Create(ctx, input(title)); !errors.As(err, &invalid) {
			t.Errorf("%q: %v", title, err)
		}
	}

	if _, err := store.Find(ctx, "Unknown "+uuid.NewString()); !errors.Is(err, credtype.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestRefusesAFormThatCannotBeUsed(t *testing.T) {
	store, _ := open(t)
	in := input("Pas " + uuid.NewString())
	in.Fields = in.Fields[1:]

	var invalid credtype.InvalidError
	if _, err := store.Create(context.Background(), in); !errors.As(err, &invalid) {
		t.Fatalf("a form without email: %v", err)
	}
}

func TestChangesATypeNobodyHoldsFreelyButOnlyTheTextOfOneInUse(t *testing.T) {
	ctx := context.Background()
	store, pool := open(t)
	title := "Pas " + uuid.NewString()
	created, _ := store.Create(ctx, input(title))

	changed := input("Nieuwe titel")
	changed.Fields = append(changed.Fields, credential.Field{Key: "floor", Label: "Verdieping", Kind: "text"})

	updated, err := store.Update(ctx, created.ID, changed)
	if err != nil || updated.Title != "Nieuwe titel" || updated.Type.Type != title || updated.Path != created.Path || len(updated.Fields) != 3 {
		t.Fatalf("%+v %v", updated, err)
	}

	hold(t, pool, title, "issued")

	if _, err := store.Update(ctx, created.ID, changed); err != nil {
		t.Fatalf("the same fields of a type in use: %v", err)
	}

	renamed := changed

	renamed.Title = "Weer anders"
	if got, err := store.Update(ctx, created.ID, renamed); err != nil || got.Title != "Weer anders" || !got.InUse {
		t.Fatalf("a new title of a type in use: %+v %v", got, err)
	}

	renamed.Fields = renamed.Fields[:2]
	if _, err := store.Update(ctx, created.ID, renamed); !errors.Is(err, credtype.ErrInUse) {
		t.Fatalf("other fields of a type in use: %v", err)
	}

	if _, err := store.Update(ctx, uuid.NewString(), changed); !errors.Is(err, credtype.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}

	if _, err := store.Update(ctx, "nope", changed); !errors.Is(err, credtype.ErrNotFound) {
		t.Fatalf("bad id: %v", err)
	}
}

func TestDeletesATypeSoftlyUnlessACredentialOfItIsNotRevoked(t *testing.T) {
	ctx := context.Background()
	store, pool := open(t)
	title := "Pas " + uuid.NewString()
	created, _ := store.Create(ctx, input(title))
	hold(t, pool, title, "open")

	if err := store.Delete(ctx, created.ID); !errors.Is(err, credtype.ErrInUse) {
		t.Fatalf("in use: %v", err)
	}

	if _, err := pool.Exec(ctx, "UPDATE entitlements SET status = 'revoked' WHERE type = $1", title); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.FindByPath(ctx, created.Path); !errors.Is(err, credtype.ErrNotFound) {
		t.Fatalf("a deleted type was found: %v", err)
	}

	if err := store.Delete(ctx, created.ID); !errors.Is(err, credtype.ErrNotFound) {
		t.Fatalf("deleted twice: %v", err)
	}

	again, err := store.Create(ctx, input(title))
	if err != nil || again.ID == created.ID {
		t.Fatalf("the name of a deleted type cannot be used again: %+v %v", again, err)
	}
}

func TestKeepsTheBoundsOfANumberAndDropsThemFromOtherFields(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)
	in := input("Prijs " + uuid.NewString())
	in.Fields = append(in.Fields,
		credential.Field{Key: "price", Label: "Prijs", Kind: "number", Decimals: 2, Min: " 0,5 ", Max: "99"},
		credential.Field{Key: "note", Label: "Notitie", Kind: "text", Min: "1", Decimals: 3})

	created, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	price, note := created.Fields[2], created.Fields[3]
	if price.Min != "0.5" || price.Max != "99" || price.Decimals != 2 || note.Min != "" || note.Decimals != 0 {
		t.Fatalf("%+v %+v", price, note)
	}

	found, _ := store.FindByPath(ctx, created.Path)
	if !reflect.DeepEqual(found.Fields[2], price) {
		t.Fatalf("not kept: %+v", found.Fields[2])
	}

	in.Fields[2].Max = "0.1"

	var invalid credtype.InvalidError
	if _, err := store.Update(ctx, created.ID, in); !errors.As(err, &invalid) {
		t.Fatalf("min above max: %v", err)
	}
}
