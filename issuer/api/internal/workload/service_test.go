package workload_test

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
	"attesta/issuer/internal/svid"
	"attesta/issuer/internal/workload"
)

type fakes struct {
	enrolled     map[string]workload.Workload
	created      []entitlement.New
	revoked      []string
	offered      []time.Duration
	verifyErr    error
	verifiedAs   string
	grant        *workload.Grant
	mandateEnded []string
}

func (f *fakes) Enroll(_ context.Context, in workload.Input) (workload.Workload, error) {
	w := workload.Workload{ID: "w-1", SpiffeID: in.SpiffeID, Owner: in.Owner, Environment: in.Environment}
	f.enrolled[in.SpiffeID] = w
	return w, nil
}
func (f *fakes) List(context.Context) ([]workload.Workload, error) {
	var out []workload.Workload
	for _, w := range f.enrolled {
		out = append(out, w)
	}
	return out, nil
}
func (f *fakes) FindEnrolled(_ context.Context, id string) (workload.Workload, error) {
	if w, ok := f.enrolled[id]; ok {
		return w, nil
	}
	return workload.Workload{}, workload.ErrNotFound
}
func (f *fakes) Revoke(_ context.Context, id string) (workload.Workload, error) {
	for _, w := range f.enrolled {
		if w.ID == id {
			delete(f.enrolled, w.SpiffeID)
			return w, nil
		}
	}
	return workload.Workload{}, workload.ErrNotFound
}
func (f *fakes) Create(_ context.Context, n entitlement.New) (entitlement.Entitlement, error) {
	f.created = append(f.created, n)
	return entitlement.Entitlement{ID: "e-1", Subject: n.Subject, Type: n.Type, Claims: n.Claims}, nil
}
func (f *fakes) RevokeAll(_ context.Context, subject entitlement.Subject) (int64, error) {
	f.revoked = append(f.revoked, subject.SPIFFEID)
	return 1, nil
}
func (f *fakes) Verify(_ context.Context, token string) (string, error) {
	if f.verifyErr != nil {
		return "", f.verifyErr
	}
	return f.verifiedAs, nil
}
func (f *fakes) OfferFor(_ context.Context, e entitlement.Entitlement, lifetime time.Duration) (string, error) {
	f.offered = append(f.offered, lifetime)
	return "openid-credential-offer://?for=" + e.ID, nil
}

func (f *fakes) EndFor(_ context.Context, spiffeID string) error {
	f.mandateEnded = append(f.mandateEnded, spiffeID)
	return nil
}

func (f *fakes) GrantFor(context.Context, string) (workload.Grant, bool, error) {
	if f.grant == nil {
		return workload.Grant{}, false, nil
	}
	return *f.grant, true, nil
}

var now = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func newService(f *fakes) *workload.Service {
	return workload.NewService(workload.Config{CredentialLifetime: 30 * time.Minute, OfferLifetime: 2 * time.Minute, Now: func() time.Time { return now }}, f, f, f, f, f)
}

func enrolledAgent() *fakes {
	return &fakes{
		enrolled:   map[string]workload.Workload{agent: {ID: "w-1", SpiffeID: agent, Owner: "platform", Environment: "staging"}},
		verifiedAs: agent,
	}
}

func TestOfferMakesACredentialThatEndsByItself(t *testing.T) {
	f := enrolledAgent()
	offers, err := newService(f).Offer(context.Background(), "token")
	if err != nil || offers.Workload != "openid-credential-offer://?for=e-1" || offers.Mandate != "" {
		t.Fatalf("Offer = %+v, %v", offers, err)
	}
	want := entitlement.New{Subject: entitlement.WorkloadOf(agent), Type: credential.Workload, Claims: map[string]string{
		"workload": agent, "owner": "platform", "environment": "staging", credential.ExpiresAtKey: "2026-10-22T09:30:00Z",
	}}
	if len(f.created) != 1 || f.created[0].Subject != want.Subject || f.created[0].Type != want.Type || len(f.created[0].Claims) != len(want.Claims) {
		t.Fatalf("entitlement = %+v, want %+v", f.created, want)
	}
	for k, v := range want.Claims {
		if f.created[0].Claims[k] != v {
			t.Errorf("claim %s = %q, want %q", k, f.created[0].Claims[k], v)
		}
	}
	if len(f.offered) != 1 || f.offered[0] != 2*time.Minute {
		t.Errorf("the link lives %v, want 2m", f.offered)
	}
}

func TestOfferAddsTheMandateOfferedPeopleApproved(t *testing.T) {
	f := enrolledAgent()
	f.grant = &workload.Grant{Resource: "deploy:prod:v1.*", Approvers: []string{"anna@corp.test", "bob@corp.test"}, EndsAt: now.Add(10 * time.Minute)}
	offers, err := newService(f).Offer(context.Background(), "token")
	if err != nil || offers.Workload == "" || offers.Mandate == "" {
		t.Fatalf("Offer = %+v, %v", offers, err)
	}
	if len(f.created) != 2 || f.created[1].Type != credential.Mandate {
		t.Fatalf("entitlements = %+v, want a Workload and a Mandate", f.created)
	}
	want := map[string]string{
		"workload": agent, "resource": "deploy:prod:v1.*", "approvers": "anna@corp.test,bob@corp.test", credential.ExpiresAtKey: "2026-10-22T09:10:00Z",
	}
	for k, v := range want {
		if f.created[1].Claims[k] != v {
			t.Errorf("mandate claim %s = %q, want %q", k, f.created[1].Claims[k], v)
		}
	}
}

func TestAMandateNeverOutlivesTheCredentialLifetime(t *testing.T) {
	f := enrolledAgent()
	f.grant = &workload.Grant{Resource: "deploy:prod:*", Approvers: []string{"anna@corp.test"}, EndsAt: now.Add(72 * time.Hour)}
	if _, err := newService(f).Offer(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if got := f.created[1].Claims[credential.ExpiresAtKey]; got != "2026-10-22T09:30:00Z" {
		t.Errorf("the mandate credential ends at %s, want 09:30, the credential lifetime", got)
	}
}

func TestOfferRefusesWhatIsNotAnEnrolledWorkload(t *testing.T) {
	f := enrolledAgent()
	f.verifyErr = svid.ErrInvalid
	if _, err := newService(f).Offer(context.Background(), "bad"); !errors.Is(err, svid.ErrInvalid) {
		t.Errorf("a bad token gave %v, want ErrInvalid", err)
	}
	f = enrolledAgent()
	f.verifiedAs = "spiffe://corp.test/ns/other/sa/x"
	if _, err := newService(f).Offer(context.Background(), "token"); !errors.Is(err, workload.ErrNotEnrolled) {
		t.Errorf("an unknown workload gave %v, want ErrNotEnrolled", err)
	}
	if len(f.created) != 0 || len(f.offered) != 0 {
		t.Error("a workload that is not enrolled got an entitlement or a link")
	}
}

func TestRevokeWithdrawsTheCredentialsAlreadyMade(t *testing.T) {
	f := enrolledAgent()
	if err := newService(f).Revoke(context.Background(), "w-1"); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != agent {
		t.Errorf("revoked entitlements of %v, want %s", f.revoked, agent)
	}
	if len(f.mandateEnded) != 1 || f.mandateEnded[0] != agent {
		t.Errorf("ended the mandate of %v, want %s", f.mandateEnded, agent)
	}
	if err := newService(f).Revoke(context.Background(), "w-1"); !errors.Is(err, workload.ErrNotFound) {
		t.Errorf("revoking twice gave %v, want ErrNotFound", err)
	}
}

func do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheOfferEndpoint(t *testing.T) {
	f := enrolledAgent()
	protected := 0
	h := workload.Handler(newService(f), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { protected++; next.ServeHTTP(w, r) })
	})
	if rec := do(h, "POST", "/workloads/offer", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: status %d, want 401", rec.Code)
	}
	rec := do(h, "POST", "/workloads/offer", "token", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"offer":"openid-credential-offer://?for=e-1"`) {
		t.Errorf("with a token: %d %s", rec.Code, rec.Body)
	}
	f.verifyErr = svid.ErrInvalid
	if rec := do(h, "POST", "/workloads/offer", "bad", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("a bad token: status %d, want 401", rec.Code)
	}
	f.verifyErr, f.verifiedAs = nil, "spiffe://corp.test/ns/other/sa/x"
	if rec := do(h, "POST", "/workloads/offer", "token", ""); rec.Code != http.StatusForbidden {
		t.Errorf("an unknown workload: status %d, want 403", rec.Code)
	}
	if protected != 0 {
		t.Errorf("the offer endpoint went through the staff protection %d times", protected)
	}
}

func TestStaffEnrollListAndRevoke(t *testing.T) {
	f := &fakes{enrolled: map[string]workload.Workload{}}
	protected := 0
	h := workload.Handler(newService(f), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { protected++; next.ServeHTTP(w, r) })
	})
	rec := do(h, "POST", "/workloads", "", `{"spiffeId":"`+agent+`","owner":"platform","environment":"staging"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"spiffeId":"`+agent+`"`) {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/workloads", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), agent) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/workloads/w-1", "", ""); rec.Code != http.StatusNoContent {
		t.Errorf("revoke: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "DELETE", "/workloads/w-1", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("revoke again: %d, want 404", rec.Code)
	}
	if rec := do(h, "POST", "/workloads", "", `{`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d, want 400", rec.Code)
	}
	if protected != 5 {
		t.Errorf("staff endpoints went through the protection %d times, want 5", protected)
	}
}
