package entitlement_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/postgres"
	"attesta/issuer/internal/postgres/postgrestest"
)

const lease = 200 * time.Millisecond

var employee = entitlement.New{Type: "Employee", Claims: map[string]string{"name": "Jerry Smith", "department": "burgerzaken"}}

type fixture struct {
	store *entitlement.Store
	email string
}

func start(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgrestest.Server(t), postgrestest.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return fixture{store: entitlement.NewStore(pool, lease), email: uuid.NewString() + "@example.com"}
}

func (f fixture) create(t *testing.T, edit func(*entitlement.New)) entitlement.Entitlement {
	t.Helper()
	n := employee
	n.Email = f.email
	if edit != nil {
		edit(&n)
	}
	e, err := f.store.Create(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func ids(es []entitlement.Entitlement) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func expectIDs(t *testing.T, f fixture, want ...string) {
	t.Helper()
	got := ids(must(f.store.FindOpenByEmail(context.Background(), f.email)))
	if fmt.Sprint(got) != fmt.Sprint(append([]string{}, want...)) {
		t.Fatalf("open entitlements %v, want %v", got, want)
	}
}

func TestKeepsWhatItWasGivenAndFindsItByEmailInAnyCase(t *testing.T) {
	f := start(t)
	created := f.create(t, nil)
	upper := f.create(t, func(n *entitlement.New) { n.Email = strings.ToUpper(f.email); n.Type = "Diploma" })

	if created.Email != f.email || upper.Email != f.email {
		t.Fatalf("emails %q %q, want %q", created.Email, upper.Email, f.email)
	}
	if created.Type != "Employee" || created.Claims["name"] != "Jerry Smith" || created.Status != entitlement.Open || created.IssuedAt != nil {
		t.Fatalf("created %+v", created)
	}
	found := must(f.store.FindOpenByEmail(context.Background(), strings.ToUpper(f.email)))
	if len(found) != 2 {
		t.Fatalf("found %d, want 2", len(found))
	}
}

func TestFindsByIDWhateverItsStatus(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	e := f.create(t, nil)
	must(f.store.Claim(ctx, e.ID))
	must(f.store.MarkIssued(ctx, e.ID))

	found := must(f.store.Find(ctx, e.ID))
	if found.Status != entitlement.Issued || found.IssuedAt == nil {
		t.Fatalf("found %+v", found)
	}
	if _, err := f.store.Find(ctx, uuid.NewString()); !errors.Is(err, entitlement.ErrNotFound) {
		t.Fatalf("err %v, want ErrNotFound", err)
	}
}

func TestFindsOnlyTheEntitlementsOfThatPerson(t *testing.T) {
	f := start(t)
	f.create(t, func(n *entitlement.New) { n.Email = "other-" + f.email })
	expectIDs(t, f)
}

func TestListsOldestFirst(t *testing.T) {
	f := start(t)
	first := f.create(t, nil)
	second := f.create(t, func(n *entitlement.New) { n.Type = "Diploma"; n.Claims = map[string]string{} })
	expectIDs(t, f, first.ID, second.ID)
}

func TestHidesAnEntitlementPastItsEnd(t *testing.T) {
	f := start(t)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	f.create(t, func(n *entitlement.New) { n.ValidUntil = &past })
	current := f.create(t, func(n *entitlement.New) { n.ValidUntil = &future; n.Type = "Diploma" })
	expectIDs(t, f, current.ID)
}

func TestClaimsForOneRequestOnlyAndHidesItWhileClaimed(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	e := f.create(t, nil)

	if !must(f.store.Claim(ctx, e.ID)) || must(f.store.Claim(ctx, e.ID)) {
		t.Fatal("the first claim must succeed and the second must fail")
	}
	found := must(f.store.Find(ctx, e.ID))
	if found.Status != entitlement.Claimed || found.ClaimedAt == nil {
		t.Fatalf("found %+v", found)
	}
	expectIDs(t, f)
}

func TestOnlyOneOfTwoSimultaneousClaimsWins(t *testing.T) {
	f := start(t)
	e := f.create(t, nil)
	results := make([]bool, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = f.store.Claim(context.Background(), e.ID)
		}()
	}
	wg.Wait()
	if results[0] == results[1] {
		t.Fatalf("results %v, want exactly one winner", results)
	}
}

func TestIssuesAClaimedEntitlementOnce(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	e := f.create(t, nil)
	must(f.store.Claim(ctx, e.ID))

	if !must(f.store.MarkIssued(ctx, e.ID)) || must(f.store.MarkIssued(ctx, e.ID)) || must(f.store.Claim(ctx, e.ID)) {
		t.Fatal("it must be issued once and then stay unavailable")
	}
	expectIDs(t, f)
}

func TestDoesNotIssueAnEntitlementThatWasNotClaimed(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	e := f.create(t, nil)

	if must(f.store.MarkIssued(ctx, e.ID)) {
		t.Fatal("issued without a claim")
	}
	if got := must(f.store.Find(ctx, e.ID)); got.Status != entitlement.Open {
		t.Fatalf("status %s", got.Status)
	}
}

func TestCanBeClaimedAgainWhenAClaimWasNotFollowedByIssuing(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	e := f.create(t, nil)
	must(f.store.Claim(ctx, e.ID))

	time.Sleep(lease + 100*time.Millisecond)

	expectIDs(t, f, e.ID)
	if !must(f.store.Claim(ctx, e.ID)) || !must(f.store.MarkIssued(ctx, e.ID)) {
		t.Fatal("the entitlement must be claimable and issuable again")
	}
}

func TestDoesNotGiveBackAClaimedEntitlementPastItsEnd(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	end := time.Now().Add(lease)
	e := f.create(t, func(n *entitlement.New) { n.ValidUntil = &end })
	must(f.store.Claim(ctx, e.ID))

	time.Sleep(lease + 100*time.Millisecond)

	expectIDs(t, f)
	if must(f.store.Claim(ctx, e.ID)) {
		t.Fatal("claimed past its end")
	}
}

func TestRevokesOpenClaimedAndIssuedOnce(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	open := f.create(t, nil)
	claimed := f.create(t, func(n *entitlement.New) { n.Type = "Diploma" })
	issued := f.create(t, func(n *entitlement.New) { n.Type = "Certificate" })
	must(f.store.Claim(ctx, claimed.ID))
	must(f.store.Claim(ctx, issued.ID))
	must(f.store.MarkIssued(ctx, issued.ID))

	if !must(f.store.Revoke(ctx, open.ID)) || !must(f.store.Revoke(ctx, claimed.ID)) || !must(f.store.Revoke(ctx, issued.ID)) {
		t.Fatal("open, claimed and issued must be revoked")
	}
	if must(f.store.Revoke(ctx, issued.ID)) {
		t.Fatal("a revoked entitlement was revoked again")
	}
	if must(f.store.Claim(ctx, open.ID)) || must(f.store.MarkIssued(ctx, claimed.ID)) {
		t.Fatal("a revoked entitlement was used")
	}
	expectIDs(t, f)
}

func TestKnowsNothingOfAnIDItWasNeverGiven(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	if must(f.store.MarkIssued(ctx, uuid.NewString())) || must(f.store.Revoke(ctx, uuid.NewString())) {
		t.Fatal("an unknown id was accepted")
	}
}

func TestTheSamePersonAndDataAtTheSameMomentGivesOneEntitlement(t *testing.T) {
	f := start(t)
	var wg sync.WaitGroup
	got := make([]string, 8)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := f.store.Create(context.Background(), entitlement.New{Email: f.email, Type: employee.Type, Claims: employee.Claims})
			if err != nil {
				t.Error(err)
			}
			got[i] = e.ID
		}()
	}
	wg.Wait()

	for _, id := range got {
		if id != got[0] || id == "" {
			t.Fatalf("ids %v", got)
		}
	}
}

func TestDifferentDataOrAnIssuedOrWithdrawnEntitlementMakesANewOne(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	first := f.create(t, nil)
	other := f.create(t, func(n *entitlement.New) {
		n.Claims = map[string]string{"name": "Jerry Smith", "department": "secretariaat"}
	})
	if other.ID == first.ID {
		t.Fatal("other data gave the same entitlement")
	}

	if ok, err := f.store.Claim(ctx, first.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if again := f.create(t, nil); again.ID != first.ID || again.Status != entitlement.Claimed {
		t.Fatalf("while claimed: %+v", again)
	}
	if ok, err := f.store.MarkIssued(ctx, first.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	second := f.create(t, nil)
	if second.ID == first.ID {
		t.Fatal("an issued entitlement was handed out again")
	}
	if ok, err := f.store.Revoke(ctx, second.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if third := f.create(t, nil); third.ID == second.ID {
		t.Fatal("a withdrawn entitlement was handed out again")
	}
}

func TestGivesAStatusIndexAtTheFirstClaimAndKeepsIt(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	a, b := f.create(t, nil), f.create(t, func(n *entitlement.New) { n.Type = "Diploma" })
	if a.StatusIndex != nil {
		t.Fatal("an entitlement that was not claimed has a status index")
	}
	must(f.store.Claim(ctx, a.ID))
	must(f.store.Claim(ctx, b.ID))
	first, second := must(f.store.Find(ctx, a.ID)).StatusIndex, must(f.store.Find(ctx, b.ID)).StatusIndex
	if first == nil || second == nil || *first == *second {
		t.Fatalf("indexes %v %v", first, second)
	}
	time.Sleep(2 * lease) // the claim lapses and is taken again
	must(f.store.Claim(ctx, a.ID))
	if again := must(f.store.Find(ctx, a.ID)).StatusIndex; *again != *first {
		t.Fatalf("index changed from %d to %d", *first, *again)
	}
}

func TestListsTheStatusIndexOfEveryRevokedCredential(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	kept := f.create(t, nil)
	gone := f.create(t, func(n *entitlement.New) { n.Type = "Diploma" })
	unclaimed := f.create(t, func(n *entitlement.New) { n.Type = "Certificate" })
	must(f.store.Claim(ctx, kept.ID))
	must(f.store.Claim(ctx, gone.ID))
	must(f.store.Revoke(ctx, gone.ID))
	must(f.store.Revoke(ctx, unclaimed.ID))

	revoked, size, err := f.store.StatusIndexes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	goneIndex := *must(f.store.Find(ctx, gone.ID)).StatusIndex
	if len(revoked) != 1 || revoked[0] != goneIndex || size <= goneIndex || size <= *must(f.store.Find(ctx, kept.ID)).StatusIndex {
		t.Fatalf("revoked %v size %d", revoked, size)
	}
}

func TestListsEveryoneWhoseCredentialCanStillBeRevokedNewestFirst(t *testing.T) {
	f := start(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	f.create(t, func(n *entitlement.New) { n.ValidUntil = &past; n.Type = "Ended" })
	revoked := f.create(t, func(n *entitlement.New) { n.Type = "Revoked" })
	must(f.store.Revoke(ctx, revoked.ID))
	first := f.create(t, nil)
	second := f.create(t, func(n *entitlement.New) { n.Type = "Diploma" })
	n := employee
	n.Email = uuid.NewString() + "@example.com"
	other := must(f.store.Create(ctx, n))

	all := ids(must(f.store.FindActive(ctx)))
	position := func(id string) int { return slices.Index(all, id) }
	if position(first.ID) < 0 || position(second.ID) < 0 || position(other.ID) < 0 || position(revoked.ID) >= 0 {
		t.Fatalf("active %v, want %s %s %s", all, first.ID, second.ID, other.ID)
	}
	if position(second.ID) > position(first.ID) {
		t.Fatalf("the older credential comes first: %v", all)
	}
}
