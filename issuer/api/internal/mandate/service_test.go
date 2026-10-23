package mandate_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mandate"
	"attesta/issuer/internal/workload"
)

type fakes struct {
	enrolled bool
	list     []mandate.Mandate
	revoked  []string
}

func (f *fakes) FindEnrolled(context.Context, string) (workload.Workload, error) {
	if f.enrolled {
		return workload.Workload{SpiffeID: agent}, nil
	}
	return workload.Workload{}, workload.ErrNotFound
}
func (f *fakes) Create(_ context.Context, in mandate.Input, approver string) (mandate.Mandate, error) {
	m := mandate.Mandate{ID: "m-1", Workload: in.Workload, Resource: in.Resource, ExpiresAt: in.ExpiresAt, Approvers: []string{approver}}
	f.list = []mandate.Mandate{m}
	return m, nil
}
func (f *fakes) Approve(_ context.Context, id, approver string) (mandate.Mandate, error) {
	if len(f.list) == 0 || f.list[0].ID != id {
		return mandate.Mandate{}, mandate.ErrNotFound
	}
	f.list[0].Approvers = append(f.list[0].Approvers, approver)
	return f.list[0], nil
}
func (f *fakes) End(_ context.Context, id string) (mandate.Mandate, error) {
	if len(f.list) == 0 || f.list[0].ID != id {
		return mandate.Mandate{}, mandate.ErrNotFound
	}
	m := f.list[0]
	f.list = nil
	return m, nil
}
func (f *fakes) List(context.Context) ([]mandate.Mandate, error) { return f.list, nil }
func (f *fakes) ActiveFor(context.Context, string) (mandate.Mandate, error) {
	if len(f.list) == 0 {
		return mandate.Mandate{}, mandate.ErrNotFound
	}
	return f.list[0], nil
}
func (f *fakes) RevokeType(_ context.Context, s entitlement.Subject, credentialType string) (int64, error) {
	f.revoked = append(f.revoked, s.SPIFFEID+" "+credentialType)
	return 1, nil
}

func newService(f *fakes) *mandate.Service { return mandate.NewService(f, f, f) }

func TestOnlyAnEnrolledWorkloadGetsAMandate(t *testing.T) {
	f := &fakes{}
	if _, err := newService(f).Create(context.Background(), "anna@corp.test", in()); !errors.Is(err, mandate.ErrNotEnrolled) {
		t.Fatalf("Create gave %v, want ErrNotEnrolled", err)
	}
	f.enrolled = true
	if _, err := newService(f).Create(context.Background(), "anna@corp.test", in()); err != nil {
		t.Fatal(err)
	}
}

func TestEndingAMandateWithdrawsItsCredentials(t *testing.T) {
	f := &fakes{enrolled: true}
	s := newService(f)
	m, _ := s.Create(context.Background(), "anna@corp.test", in())
	if err := s.End(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != agent+" "+credential.Mandate {
		t.Errorf("revoked %v, want the Mandate credentials of %s", f.revoked, agent)
	}
	if err := s.End(context.Background(), m.ID); !errors.Is(err, mandate.ErrNotFound) {
		t.Errorf("ending twice gave %v, want ErrNotFound", err)
	}
}

func TestGrantForIsWhatThePeopleAllowed(t *testing.T) {
	f := &fakes{enrolled: true}
	s := newService(f)
	if _, ok, err := s.GrantFor(context.Background(), agent); ok || err != nil {
		t.Fatalf("no mandate gave %v, %v", ok, err)
	}
	m, _ := s.Create(context.Background(), "anna@corp.test", in())
	s.Approve(context.Background(), m.ID, "bob@corp.test")
	g, ok, err := s.GrantFor(context.Background(), agent)
	if err != nil || !ok || g.Resource != "deploy:prod:v1.*" || len(g.Approvers) != 2 || !g.EndsAt.Equal(m.ExpiresAt) {
		t.Fatalf("GrantFor = %+v, %v, %v", g, ok, err)
	}
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestThePeopleEndpoints(t *testing.T) {
	f := &fakes{enrolled: true}
	person := "anna@corp.test"
	protected := 0
	h := mandate.Handler(newService(f), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { protected++; next.ServeHTTP(w, r) })
	}, func(*http.Request) string { return person })
	body := `{"workload":"` + agent + `","resource":"deploy:prod:v1.*","expiresAt":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`

	if rec := call(h, "POST", "/mandates", body); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"approvers":["anna@corp.test"]`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	person = "bob@corp.test"
	if rec := call(h, "POST", "/mandates/m-1/approvals", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "bob@corp.test") {
		t.Errorf("approve: %d %s", rec.Code, rec.Body)
	}
	if rec := call(h, "GET", "/mandates", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "deploy:prod:v1.*") || !strings.Contains(rec.Body.String(), `"approvedByMe":true`) {
		t.Errorf("list as bob, who approved: %d %s", rec.Code, rec.Body)
	}
	person = "carol@corp.test"
	if rec := call(h, "GET", "/mandates", ""); !strings.Contains(rec.Body.String(), `"approvedByMe":false`) {
		t.Errorf("list as carol, who did not: %s", rec.Body)
	}
	person = ""
	if rec := call(h, "POST", "/mandates/m-1/approvals", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("approving as nobody: %d, want 401", rec.Code)
	}
	if rec := call(h, "DELETE", "/mandates/m-1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("end: %d", rec.Code)
	}
	if rec := call(h, "DELETE", "/mandates/m-1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("end again: %d, want 404", rec.Code)
	}
	if protected != 7 {
		t.Errorf("went through the protection %d times, want 7", protected)
	}
}

func TestEndForEndsTheMandateOfAWorkload(t *testing.T) {
	f := &fakes{enrolled: true}
	s := mandate.NewService(f, f, f)
	if err := s.EndFor(context.Background(), agent); err != nil {
		t.Fatalf("a workload without a mandate gave %v", err)
	}
	if _, err := s.Create(context.Background(), "anna@corp.test", mandate.Input{Workload: agent, Resource: "deploy:prod:v1.*"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndFor(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	if len(f.list) != 0 || len(f.revoked) != 1 {
		t.Errorf("mandates left %v, revoked %v; want none and its credentials withdrawn", f.list, f.revoked)
	}
}
