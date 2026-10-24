package statuslist_test

import (
	"attesta/issuer/internal/identity"
	"attesta/issuer/internal/statuslist"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdjwt "github.com/threehook/sdjwt-go"
)

type fake struct {
	revoked []int64
	size    int64
	err     error
}

func (f fake) StatusIndexes(context.Context) ([]int64, int64, error) { return f.revoked, f.size, f.err }

var now = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func get(t *testing.T, s *statuslist.Server) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, statuslist.Path, nil))

	return rec
}

func TestPublishesASignedListWithTheRevokedBitsSet(t *testing.T) {
	me := identity.FromSeed("test")
	s := statuslist.New(me, fake{revoked: []int64{2, 20000}, size: 20001}, "https://issuer.test", func() time.Time { return now })

	rec := get(t, s)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/statuslist+jwt" {
		t.Fatalf("%d %s", rec.Code, rec.Header())
	}

	body, _ := io.ReadAll(rec.Body)

	got, err := sdjwt.VerifyStatusList(string(body), sdjwt.DIDKey{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	if got.Issuer != me.DID || got.URI != "https://issuer.test/status/1" || got.TTL != statuslist.TTL ||
		!got.ExpiresAt.Equal(now.Add(statuslist.Lifetime)) {
		t.Fatalf("%+v", got)
	}

	for i, want := range map[int]bool{0: false, 2: true, 3: false, 20000: true, 20001: false} {
		if r, err := got.List.Revoked(i); err != nil || r != want {
			t.Fatalf("index %d: %v %v", i, r, err)
		}
	}
}

func TestTheListIsNeverSmallerThanItsMinimum(t *testing.T) {
	s := statuslist.New(identity.FromSeed("test"), fake{size: 3}, "https://issuer.test", func() time.Time { return now })
	body, _ := io.ReadAll(get(t, s).Body)

	got, err := sdjwt.VerifyStatusList(string(body), sdjwt.DIDKey{}, func() time.Time { return now })
	if err != nil || got.List.Len() < 16384 {
		t.Fatal(err, got)
	}
}

func TestAnUnreadableDatabaseGivesNoList(t *testing.T) {
	s := statuslist.New(identity.FromSeed("test"), fake{err: errors.New("down")}, "https://issuer.test", nil)
	if rec := get(t, s); rec.Code != http.StatusInternalServerError {
		t.Fatal(rec.Code)
	}
}
