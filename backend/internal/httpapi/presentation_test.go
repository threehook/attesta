package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"attesta/backend/internal/authz"
	"attesta/backend/internal/presentation"
)

// fakePresenter isolates httpapi's routing and status codes from the real verifier, which internal/presentation tests with a recorded wallet answer.
type fakePresenter struct {
	newErr, respondErr error
	presented          *presentation.Presented
	outcome            *presentation.Outcome
	outcomeErr         error

	gotRequest presentation.Request
	gotForm    url.Values
	completed  *presentation.Outcome
}

func (f *fakePresenter) NewRequest(r presentation.Request) (string, string, error) {
	f.gotRequest = r
	return "req-1", "openid4vp://?x=1", f.newErr
}

func (f *fakePresenter) Respond(_ context.Context, _ string, form url.Values) (*presentation.Presented, error) {
	f.gotForm = form
	return f.presented, f.respondErr
}

func (f *fakePresenter) Complete(_ string, o presentation.Outcome) { f.completed = &o }

func (f *fakePresenter) Outcome(string) (*presentation.Outcome, error) {
	return f.outcome, f.outcomeErr
}

func newPresentationServer(t *testing.T, p *fakePresenter, e *fakeEvaluator) *Server {
	t.Helper()
	s := newTestServer(t, e)
	s.Presenter = p
	return s
}

func postForm(s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	return doRequest(s, "POST", path, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

const requestBody = `{"resource":"vault","policyId":"p1","credentialType":"Diploma","claims":["degree"]}`

func TestHandlePresentationRequest(t *testing.T) {
	p := &fakePresenter{}
	s := newPresentationServer(t, p, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/authorize/requests", requestBody, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp presentationRequestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "req-1" || resp.AuthorizationRequest != "openid4vp://?x=1" {
		t.Errorf("response = %+v", resp)
	}
	want := presentation.Request{Resource: "vault", PolicyID: "p1", CredentialType: "Diploma", Claims: []string{"degree"}}
	if p.gotRequest.Resource != want.Resource || p.gotRequest.PolicyID != want.PolicyID || p.gotRequest.CredentialType != want.CredentialType ||
		len(p.gotRequest.Claims) != 1 || p.gotRequest.Claims[0] != "degree" {
		t.Errorf("request = %+v, want %+v", p.gotRequest, want)
	}
}

func TestHandlePresentationRequestErrors(t *testing.T) {
	cases := map[string]struct {
		body   string
		newErr error
		status int
	}{
		"bad json":       {body: `{`, status: http.StatusBadRequest},
		"missing fields": {body: `{"resource":"vault"}`, status: http.StatusBadRequest},
		"unknown policy": {body: strings.Replace(requestBody, `"p1"`, `"nope"`, 1), status: http.StatusNotFound},
		"verifier fails": {body: requestBody, newErr: errors.New("boom"), status: http.StatusInternalServerError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPresentationServer(t, &fakePresenter{newErr: tc.newErr}, &fakeEvaluator{})
			if rec := doRequest(s, "POST", "/v1/authorize/requests", tc.body, nil); rec.Code != tc.status {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
		})
	}

	s := newTestServer(t, &fakeEvaluator{})
	if rec := doRequest(s, "POST", "/v1/authorize/requests", requestBody, nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("without a Presenter: status = %d, want 501", rec.Code)
	}
}

func presented() *presentation.Presented {
	return &presentation.Presented{
		Request: presentation.Request{Resource: "vault", PolicyID: "p1", CredentialType: "Diploma"},
		Issuer:  "did:key:issuer", Type: "Diploma", Subject: presentation.Subject{Issuer: "did:key:issuer", Email: "ada@example.com"},
	}
}

func TestHandlePresentationResponse(t *testing.T) {
	cases := map[string]struct {
		p         *fakePresenter
		e         *fakeEvaluator
		status    int
		completed *presentation.Outcome
	}{
		"allowed": {
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{result: authz.Result{Allow: true, Reason: "ok"}},
			status: http.StatusOK, completed: &presentation.Outcome{Allow: true, Reason: "ok", Subject: &presentation.Subject{Issuer: "did:key:issuer", Email: "ada@example.com"}},
		},
		"denied by the policy": {
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{result: authz.Result{Reason: "issuer not trusted"}},
			status: http.StatusOK, completed: &presentation.Outcome{Reason: "issuer not trusted"},
		},
		"does not verify": {
			p: &fakePresenter{respondErr: presentation.ErrInvalidPresentation}, e: &fakeEvaluator{},
			status: http.StatusBadRequest, completed: &presentation.Outcome{Reason: "presentation did not verify"},
		},
		"unknown request":  {p: &fakePresenter{respondErr: presentation.ErrUnknownRequest}, e: &fakeEvaluator{}, status: http.StatusNotFound},
		"already answered": {p: &fakePresenter{respondErr: presentation.ErrAlreadyAnswered}, e: &fakeEvaluator{}, status: http.StatusBadRequest},
		"policy fails": {
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{err: errors.New("boom")},
			status: http.StatusInternalServerError, completed: &presentation.Outcome{Reason: "policy evaluation failed"},
		},
		"policy disappeared": {
			p: &fakePresenter{presented: &presentation.Presented{Request: presentation.Request{PolicyID: "gone"}}}, e: &fakeEvaluator{},
			status: http.StatusNotFound, completed: &presentation.Outcome{Reason: "unknown policy"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPresentationServer(t, tc.p, tc.e)
			rec := postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
			if tc.p.gotForm.Get("state") != "s" {
				t.Errorf("Respond got form %v, want the posted fields", tc.p.gotForm)
			}
			switch {
			case tc.completed == nil && tc.p.completed != nil:
				t.Errorf("Complete called with %+v, want no decision recorded", *tc.p.completed)
			case tc.completed != nil && (tc.p.completed == nil || !sameOutcome(*tc.p.completed, *tc.completed)):
				t.Errorf("recorded %+v, want %+v", tc.p.completed, *tc.completed)
			}
		})
	}
}

func TestHandlePresentationResponsePassesTheClaimsToThePolicy(t *testing.T) {
	p := presented()
	p.Claims = map[string]any{"department": "burgerzaken", "email": "ada@example.com", "level": float64(3), "courses": []any{"a", "b"}}
	e := &fakeEvaluator{result: authz.Result{Allow: true}}
	s := newPresentationServer(t, &fakePresenter{presented: p}, e)

	postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})

	want := map[string]string{"department": "burgerzaken", "email": "ada@example.com", "level": "3", "courses": `["a","b"]`}
	if len(e.evaluated.Claims) != len(want) {
		t.Fatalf("policy got claims %v, want %v", e.evaluated.Claims, want)
	}
	for name, value := range want {
		if e.evaluated.Claims[name] != value {
			t.Errorf("claim %s = %q, want %q", name, e.evaluated.Claims[name], value)
		}
	}
}

func TestHandlePresentationOutcome(t *testing.T) {
	yes, no := true, false
	cases := map[string]struct {
		p      *fakePresenter
		status int
		want   presentationOutcomeResponse
	}{
		"pending": {p: &fakePresenter{}, status: http.StatusOK, want: presentationOutcomeResponse{Status: "pending"}},
		"allowed": {
			p:      &fakePresenter{outcome: &presentation.Outcome{Allow: true, Reason: "ok", Subject: &presentation.Subject{Issuer: "did:key:i", Email: "ada@example.com"}}},
			status: http.StatusOK,
			want:   presentationOutcomeResponse{Status: "done", Allow: &yes, Reason: "ok", Subject: &subjectResponse{Issuer: "did:key:i", Email: "ada@example.com"}},
		},
		"denied": {
			p: &fakePresenter{outcome: &presentation.Outcome{Reason: "issuer not trusted"}}, status: http.StatusOK,
			want: presentationOutcomeResponse{Status: "done", Allow: &no, Reason: "issuer not trusted"},
		},
		"unknown": {p: &fakePresenter{outcomeErr: presentation.ErrUnknownRequest}, status: http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPresentationServer(t, tc.p, &fakeEvaluator{})
			rec := doRequest(s, "GET", "/v1/authorize/requests/req-1", "", nil)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
			if tc.status != http.StatusOK {
				return
			}
			var got presentationOutcomeResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want.Status || got.Reason != tc.want.Reason || (got.Allow == nil) != (tc.want.Allow == nil) ||
				(got.Allow != nil && *got.Allow != *tc.want.Allow) || (got.Subject == nil) != (tc.want.Subject == nil) ||
				(got.Subject != nil && *got.Subject != *tc.want.Subject) {
				t.Errorf("response = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func sameOutcome(a, b presentation.Outcome) bool {
	if a.Allow != b.Allow || a.Reason != b.Reason || (a.Subject == nil) != (b.Subject == nil) {
		return false
	}
	return a.Subject == nil || *a.Subject == *b.Subject
}

func TestParseRoles(t *testing.T) {
	long := strings.Repeat("x", maxUserRoleBytes+1)
	tests := []struct {
		name, header string
		want         []string
	}{
		{"missing", "", []string{}},
		{"one", "admin", []string{"admin"}},
		{"trimmed and distinct", " admin , editor,admin,, ", []string{"admin", "editor"}},
		{"too long", "admin," + long, []string{"admin"}},
	}
	for _, tt := range tests {
		got := parseUserRoles(tt.header)
		if got == nil || !slices.Equal(got, tt.want) {
			t.Errorf("%s: parseUserRoles(%q) = %#v, want %#v", tt.name, tt.header, got, tt.want)
		}
	}
	many := make([]string, maxUserRoles+5)
	for i := range many {
		many[i] = fmt.Sprintf("r%d", i)
	}
	if got := parseUserRoles(strings.Join(many, ",")); len(got) != maxUserRoles {
		t.Errorf("got %d roles, want the first %d", len(got), maxUserRoles)
	}
}

func TestUserRolesTravelFromTheRequestToThePolicy(t *testing.T) {
	p := &fakePresenter{presented: presented()}
	e := &fakeEvaluator{result: authz.Result{Allow: true}}
	s := newPresentationServer(t, p, e)

	doRequest(s, "POST", "/v1/authorize/requests", requestBody, map[string]string{"Att-User-Roles": "admin, editor"})
	if !slices.Equal(p.gotRequest.UserRoles, []string{"admin", "editor"}) {
		t.Fatalf("request roles = %v", p.gotRequest.UserRoles)
	}

	p.presented.Request.UserRoles = p.gotRequest.UserRoles
	postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})
	if !slices.Equal(e.evaluated.UserRoles, []string{"admin", "editor"}) {
		t.Errorf("policy roles = %v", e.evaluated.UserRoles)
	}
}
