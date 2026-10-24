package approval_test

import (
	"attesta/issuer/internal/approval"
	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/mail"
	"attesta/issuer/internal/svid"
	"attesta/issuer/internal/workload"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakes struct {
	enrolled  bool
	verifyErr error
	request   *approval.Request
	delivered bool
	created   []entitlement.New
	sent      []mail.Message
	sendErr   error
	nextID    int
}

func (f *fakes) Open(_ context.Context, w, resource string, action map[string]string, _ time.Duration) (approval.Request, bool, error) {
	if f.request != nil && f.request.State == approval.Open {
		return *f.request, false, nil
	}

	f.nextID++
	f.request = &approval.Request{ID: "r-1", Workload: w, Resource: resource, Action: action, State: approval.Open}

	return *f.request, true, nil
}

func (f *fakes) Get(_ context.Context, id string) (approval.Request, error) {
	if f.request == nil || f.request.ID != id {
		return approval.Request{}, approval.ErrNotFound
	}

	return *f.request, nil
}

func (f *fakes) List(context.Context) ([]approval.Request, error) {
	if f.request == nil {
		return nil, nil
	}

	return []approval.Request{*f.request}, nil
}

func (f *fakes) Approve(_ context.Context, _, approver string) (approval.Request, error) {
	f.request.Approvers = append(f.request.Approvers, approver)
	return *f.request, nil
}

func (f *fakes) Reject(context.Context, string) error {
	if f.request == nil || f.request.State != approval.Open {
		return approval.ErrNotFound
	}

	f.request.State = approval.Rejected

	return nil
}

func (f *fakes) Deliver(context.Context, string, int) (bool, error) {
	if f.delivered {
		return false, nil
	}

	f.delivered = true
	f.request.State = approval.Delivered

	return true, nil
}
func (f *fakes) Verify(context.Context, string) (string, error) { return agent, f.verifyErr }
func (f *fakes) FindEnrolled(context.Context, string) (workload.Workload, error) {
	if f.enrolled {
		return workload.Workload{SpiffeID: agent}, nil
	}

	return workload.Workload{}, workload.ErrNotFound
}

func (f *fakes) Create(_ context.Context, n entitlement.New) (entitlement.Entitlement, error) {
	f.created = append(f.created, n)
	return entitlement.Entitlement{ID: "e-1", Type: n.Type, Claims: n.Claims}, nil
}

func (f *fakes) OfferFor(_ context.Context, e entitlement.Entitlement, _ time.Duration) (string, error) {
	return "openid-credential-offer://?for=" + e.ID, nil
}

func (f *fakes) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)
	return f.sendErr
}

var now = time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC)

func newService(f *fakes) *approval.Service {
	cfg := approval.Config{
		PublicURL:          "https://issuer.corp.test/",
		Notify:             []string{"release@corp.test", "lead@corp.test"},
		CredentialLifetime: 10 * time.Minute,
		Now:                func() time.Time { return now },
	}

	return approval.NewService(cfg, f, f, f, f, f, f)
}

func TestARequestMailsThePeopleWhoApproveOnlyWhenItIsNew(t *testing.T) {
	f := &fakes{enrolled: true}

	r, err := newService(f).Request(context.Background(), "t", "deploy:prod:v2.0.0", shop)
	if err != nil || r.ID != "r-1" {
		t.Fatalf("Request = %+v, %v", r, err)
	}

	if len(f.sent) != 2 || f.sent[0].To != "release@corp.test" || !strings.Contains(f.sent[0].Text, "https://issuer.corp.test/approvals") ||
		!strings.Contains(f.sent[0].Subject, "deploy:prod:v2.0.0") {
		t.Fatalf("mails = %+v", f.sent)
	}

	if _, err := newService(f).Request(context.Background(), "t", "deploy:prod:v2.0.0", shop); err != nil || len(f.sent) != 2 {
		t.Errorf("asking again mailed again: %d mails, %v", len(f.sent), err)
	}
}

func TestAMailThatFailsDoesNotStopTheRequest(t *testing.T) {
	f := &fakes{enrolled: true, sendErr: errors.New("smtp down")}
	if _, err := newService(f).Request(context.Background(), "t", "deploy:prod:v2.0.0", shop); err != nil {
		t.Errorf("Request gave %v", err)
	}
}

func TestOnlyAnEnrolledWorkloadWithAValidTokenParksAnAction(t *testing.T) {
	f := &fakes{}
	if _, err := newService(f).Request(context.Background(), "t", "r", nil); !errors.Is(err, approval.ErrNotEnrolled) {
		t.Errorf("not enrolled gave %v", err)
	}

	f = &fakes{enrolled: true, verifyErr: svid.ErrInvalid}
	if _, err := newService(f).Request(context.Background(), "t", "r", nil); !errors.Is(err, svid.ErrInvalid) {
		t.Errorf("a bad token gave %v", err)
	}

	if len(f.sent) != 0 || f.request != nil {
		t.Error("a request without proof was kept or mailed")
	}
}

func TestTheCredentialIsHandedOutOnceAfterEnoughPeopleApproved(t *testing.T) {
	f := &fakes{enrolled: true}

	s := newService(f)
	if _, err := s.Request(context.Background(), "t", "deploy:prod:v2.0.0", shop); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Offer(context.Background(), "t", "r-1"); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatalf("with no approvals Offer gave %v, want ErrNotApproved", err)
	}

	if _, err := s.Approve(context.Background(), "r-1", "anna@corp.test"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Offer(context.Background(), "t", "r-1"); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatalf("with one approval Offer gave %v, want ErrNotApproved", err)
	}

	if _, err := s.Approve(context.Background(), "r-1", "bob@corp.test"); err != nil {
		t.Fatal(err)
	}

	link, err := s.Offer(context.Background(), "t", "r-1")
	if err != nil || link != "openid-credential-offer://?for=e-1" {
		t.Fatalf("Offer = %q, %v", link, err)
	}

	want := map[string]string{
		"workload": agent, "resource": "deploy:prod:v2.0.0", "approvers": "anna@corp.test,bob@corp.test",
		credential.ExpiresAtKey: "2026-10-22T22:10:00Z",
	}

	if len(f.created) != 1 || f.created[0].Type != credential.Approval {
		t.Fatalf("entitlements = %+v", f.created)
	}

	for k, v := range want {
		if f.created[0].Claims[k] != v {
			t.Errorf("claim %s = %q, want %q", k, f.created[0].Claims[k], v)
		}
	}

	if _, err := s.Offer(context.Background(), "t", "r-1"); !errors.Is(err, approval.ErrGone) {
		t.Errorf("the second time Offer gave %v, want ErrGone", err)
	}
}

func call(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestTheWorkloadEndpoints(t *testing.T) {
	f := &fakes{enrolled: true}

	h := approval.WorkloadHandler(newService(f))
	if rec := call(h, "POST", "/workloads/approvals", "", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d, want 401", rec.Code)
	}

	rec := call(h, "POST", "/workloads/approvals", "t", `{"resource":"deploy:prod:v2.0.0","action":{"version":"v2.0.0"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"r-1"`) || !strings.Contains(rec.Body.String(), `"required":2`) {
		t.Fatalf("park: %d %s", rec.Code, rec.Body)
	}

	if rec := call(h, "POST", "/workloads/approvals/r-1/offer", "t", ""); rec.Code != http.StatusAccepted ||
		!strings.Contains(rec.Body.String(), `"approvals":0`) {
		t.Errorf("too early: %d %s", rec.Code, rec.Body)
	}

	f.request.Approvers = []string{"anna@corp.test", "bob@corp.test"}

	if rec := call(h, "POST", "/workloads/approvals/r-1/offer", "t", ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"offer":"openid-credential-offer://`) {
		t.Errorf("approved: %d %s", rec.Code, rec.Body)
	}

	if rec := call(h, "POST", "/workloads/approvals/r-1/offer", "t", ""); rec.Code != http.StatusGone {
		t.Errorf("again: %d, want 410", rec.Code)
	}

	if rec := call(h, "GET", "/workloads/approvals/r-1", "t", ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"status":"delivered"`) {
		t.Errorf("status: %d %s", rec.Code, rec.Body)
	}

	if rec := call(h, "GET", "/workloads/approvals/nope", "t", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", rec.Code)
	}

	if rec := call(h, "POST", "/workloads/approvals", "t", `{`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad body: %d, want 400", rec.Code)
	}
}

func TestThePeopleEndpoints(t *testing.T) {
	f := &fakes{enrolled: true}
	if _, err := newService(f).Request(context.Background(), "t", "deploy:prod:v2.0.0", shop); err != nil {
		t.Fatal(err)
	}

	person := "anna@corp.test"
	protected := 0
	h := approval.PeopleHandler(newService(f), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { protected++; next.ServeHTTP(w, r) })
	}, func(*http.Request) string { return person })

	if rec := call(h, "POST", "/approvals/r-1/approvals", "", ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"approvedByMe":true`) ||
		!strings.Contains(rec.Body.String(), `"validSeconds":600`) {
		t.Errorf("approve: %d %s", rec.Code, rec.Body)
	}

	if rec := call(h, "GET", "/approvals", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "deploy:prod:v2.0.0") ||
		!strings.Contains(rec.Body.String(), `"approvedByMe":true`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}

	person = "carol@corp.test"

	if rec := call(h, "GET", "/approvals", "", ""); !strings.Contains(rec.Body.String(), `"approvedByMe":false`) {
		t.Errorf("list as carol: %s", rec.Body)
	}

	person = ""

	if rec := call(h, "POST", "/approvals/r-1/approvals", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("approving as nobody: %d, want 401", rec.Code)
	}

	if rec := call(h, "DELETE", "/approvals/r-1", "", ""); rec.Code != http.StatusNoContent {
		t.Errorf("reject: %d", rec.Code)
	}

	if rec := call(h, "DELETE", "/approvals/r-1", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("reject again: %d, want 404", rec.Code)
	}

	if protected != 6 {
		t.Errorf("went through the protection %d times, want 6", protected)
	}
}
