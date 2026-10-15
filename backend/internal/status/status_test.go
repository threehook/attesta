package status_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/threehook/sdjwt-go"

	"attesta/backend/internal/status"
)

var epoch = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// issuer is an issuer's status list endpoint, with a clock the tests move.
type issuer struct {
	key     ed25519.PrivateKey
	did     string
	server  *httptest.Server
	mu      sync.Mutex
	list    *sdjwt.StatusList
	ttl     time.Duration
	expires time.Duration
	hits    int
	status  int
	now     time.Time
	uri     string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	i := &issuer{key: key, did: sdjwt.DIDKeyFromEd25519(pub), list: sdjwt.NewStatusList(16), ttl: time.Minute, expires: time.Hour, status: 200, now: epoch}
	i.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i.mu.Lock()
		defer i.mu.Unlock()
		i.hits++
		if i.status != 200 {
			w.WriteHeader(i.status)
			return
		}
		uri := i.uri
		if uri == "" {
			uri = i.server.URL + "/status/1"
		}
		token, _ := sdjwt.SignStatusList(i.key, uri, i.list, i.ttl, i.now.Add(i.expires), func() time.Time { return i.now })
		w.Write([]byte(token))
	}))
	t.Cleanup(i.server.Close)
	return i
}

func (i *issuer) ref(index int) *sdjwt.StatusReference {
	return &sdjwt.StatusReference{Index: index, URI: i.server.URL + "/status/1"}
}

func (i *issuer) revoke(index int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.list.SetRevoked(index, true)
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func checker(t *testing.T, i *issuer, clk *clock, edit func(*status.Options)) *status.Checker {
	t.Helper()
	o := status.Options{Prefixes: []string{i.server.URL + "/status/"}, Keys: sdjwt.DIDKey{}, Now: clk.Now}
	if edit != nil {
		edit(&o)
	}
	c, err := status.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAcceptsACredentialWhoseBitIsClearAndRefusesARevokedOne(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	i.revoke(3)
	c := checker(t, i, clk, nil)

	if err := c.Check(context.Background(), i.did, i.ref(2)); err != nil {
		t.Fatal(err)
	}
	if err := c.Check(context.Background(), i.did, i.ref(3)); !errors.Is(err, status.ErrRevoked) {
		t.Fatalf("err %v", err)
	}
}

func TestRefusesACredentialWithoutAStatusReference(t *testing.T) {
	i := newIssuer(t)
	if err := checker(t, i, &clock{epoch}, nil).Check(context.Background(), i.did, nil); !errors.Is(err, status.ErrNoStatus) {
		t.Fatalf("err %v", err)
	}
}

func TestSeesARevocationWhenTheTTLHasPassedButNotBefore(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	c := checker(t, i, clk, nil)
	if err := c.Check(context.Background(), i.did, i.ref(5)); err != nil {
		t.Fatal(err)
	}
	i.revoke(5)

	clk.now = epoch.Add(30 * time.Second)
	if err := c.Check(context.Background(), i.did, i.ref(5)); err != nil {
		t.Fatalf("within the ttl the list we have counts: %v", err)
	}
	if i.hits != 1 {
		t.Fatalf("%d fetches within the ttl", i.hits)
	}
	clk.now = epoch.Add(61 * time.Second)
	i.now = clk.now
	if err := c.Check(context.Background(), i.did, i.ref(5)); !errors.Is(err, status.ErrRevoked) {
		t.Fatalf("after the ttl: %v", err)
	}
}

func TestFetchesAgainForACredentialBeyondTheListItHas(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	c := checker(t, i, clk, nil)
	c.Check(context.Background(), i.did, i.ref(1))

	i.mu.Lock()
	i.list.SetRevoked(40, true) // the issuer's list has grown since
	i.mu.Unlock()
	clk.now = epoch.Add(10 * time.Second)
	i.now = clk.now
	if err := c.Check(context.Background(), i.did, i.ref(40)); !errors.Is(err, status.ErrRevoked) {
		t.Fatalf("err %v", err)
	}
	if i.hits != 2 {
		t.Fatalf("%d fetches", i.hits)
	}
}

func TestDoesNotFetchOverAndOverForAnIndexThatIsNotInTheList(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	c := checker(t, i, clk, nil)
	for range 5 {
		if err := c.Check(context.Background(), i.did, i.ref(100000)); !errors.Is(err, status.ErrUnavailable) {
			t.Fatalf("err %v", err)
		}
	}
	if i.hits != 1 {
		t.Fatalf("%d fetches for one missing index", i.hits)
	}
}

func TestFailsClosed(t *testing.T) {
	cases := map[string]func(i *issuer, clk *clock){
		"the list cannot be reached":  func(i *issuer, _ *clock) { i.status = 503 },
		"the list expired":            func(i *issuer, _ *clock) { i.expires = -time.Hour },
		"another issuer signed it":    nil,
		"the list is for another uri": func(i *issuer, _ *clock) { i.uri = "https://elsewhere.test/status/1" },
	}
	for name, setup := range cases {
		i, clk := newIssuer(t), &clock{epoch}
		if setup != nil {
			setup(i, clk)
		}
		who := i.did
		if setup == nil {
			who = "did:key:z6MkSomeoneElse"
		}
		err := checker(t, i, clk, nil).Check(context.Background(), who, i.ref(1))
		if !errors.Is(err, status.ErrUnavailable) {
			t.Errorf("%s: err %v", name, err)
		}
	}
}

func TestOnlyFetchesFromAllowedPrefixes(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	c := checker(t, i, clk, func(o *status.Options) { o.Prefixes = []string{"https://issuer.example/status/"} })

	if err := c.Check(context.Background(), i.did, i.ref(1)); !errors.Is(err, status.ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if i.hits != 0 {
		t.Fatal("fetched from a place that is not allowed")
	}
	if _, err := status.New(status.Options{Prefixes: []string{"https://issuer.example"}}); err == nil {
		t.Fatal("a prefix without a closing slash would match other hosts")
	}
}

func TestReachesAListAnotherWayThanItsPublicAddress(t *testing.T) {
	i, clk := newIssuer(t), &clock{epoch}
	i.uri = "https://issuer.example/status/1"
	c := checker(t, i, clk, func(o *status.Options) {
		o.Prefixes = []string{"https://issuer.example/status/"}
		o.Hosts = map[string]string{"https://issuer.example": i.server.URL}
	})

	if err := c.Check(context.Background(), i.did, &sdjwt.StatusReference{Index: 1, URI: "https://issuer.example/status/1"}); err != nil {
		t.Fatal(err)
	}
}
