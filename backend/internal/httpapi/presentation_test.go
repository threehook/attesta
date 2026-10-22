package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"attesta/backend/internal/adl"
	"attesta/backend/internal/authz"
	"attesta/backend/internal/presentation"
	"attesta/backend/internal/status"
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
	want := presentation.Request{Resource: "vault", PolicyID: "p1", Credentials: []presentation.CredentialQuery{{CredentialType: "Diploma", Claims: []string{"degree"}}}}
	if !reflect.DeepEqual(p.gotRequest.Credentials, want.Credentials) || p.gotRequest.Resource != want.Resource || p.gotRequest.PolicyID != want.PolicyID {
		t.Errorf("request = %+v, want %+v", p.gotRequest, want)
	}
}

func TestHandlePresentationRequestPassesTheIdentityClaim(t *testing.T) {
	for claim, want := range map[string]string{"": "", "workload": "workload"} {
		p := &fakePresenter{}
		s := newPresentationServer(t, p, &fakeEvaluator{})
		body := requestBody
		if claim != "" {
			body = strings.Replace(requestBody, `"claims"`, `"identityClaim":"`+claim+`","claims"`, 1)
		}
		if rec := doRequest(s, "POST", "/v1/authorize/requests", body, nil); rec.Code != http.StatusOK {
			t.Fatalf("claim %q: status = %d: %s", claim, rec.Code, rec.Body)
		}
		if p.gotRequest.IdentityClaim != want {
			t.Errorf("claim %q: request has identity claim %q, want %q", claim, p.gotRequest.IdentityClaim, want)
		}
	}
}

func TestHandlePresentationRequestErrors(t *testing.T) {
	cases := map[string]struct {
		body   string
		newErr error
		status int
	}{
		"bad json":                             {body: `{`, status: http.StatusBadRequest},
		"missing fields":                       {body: `{"resource":"vault"}`, status: http.StatusBadRequest},
		"unknown policy":                       {body: strings.Replace(requestBody, `"p1"`, `"nope"`, 1), status: http.StatusNotFound},
		"verifier fails":                       {body: requestBody, newErr: errors.New("boom"), status: http.StatusInternalServerError},
		"identity claim is a registered claim": {body: strings.Replace(requestBody, `"claims"`, `"identityClaim":"iss","claims"`, 1), status: http.StatusBadRequest},
		"identity claim is not a plain name":   {body: strings.Replace(requestBody, `"claims"`, `"identityClaim":"Workload ID","claims"`, 1), status: http.StatusBadRequest},
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
		Request:     presentation.Request{Resource: "vault", PolicyID: "p1", Credentials: []presentation.CredentialQuery{{CredentialType: "Diploma"}}},
		Subject:     presentation.Subject{Issuer: "did:key:issuer", ID: "ada@example.com", Claim: "email"},
		Credentials: []presentation.PresentedCredential{{Issuer: "did:key:issuer", Type: "Diploma"}},
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
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{result: authz.Result{Decision: authz.Permit, Reason: "ok"}},
			status: http.StatusOK, completed: &presentation.Outcome{Decision: authz.Permit, Reason: "ok", Subject: &presentation.Subject{Issuer: "did:key:issuer", ID: "ada@example.com", Claim: "email"}},
		},
		"denied by the policy": {
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{result: authz.Result{Decision: authz.Deny, Reason: "issuer not trusted"}},
			status: http.StatusOK, completed: &presentation.Outcome{Decision: authz.Deny, Reason: "issuer not trusted"},
		},
		"does not verify": {
			p: &fakePresenter{respondErr: presentation.ErrInvalidPresentation}, e: &fakeEvaluator{},
			status: http.StatusBadRequest, completed: &presentation.Outcome{Decision: authz.Deny, Reason: "presentation did not verify"},
		},
		"unknown request":  {p: &fakePresenter{respondErr: presentation.ErrUnknownRequest}, e: &fakeEvaluator{}, status: http.StatusNotFound},
		"already answered": {p: &fakePresenter{respondErr: presentation.ErrAlreadyAnswered}, e: &fakeEvaluator{}, status: http.StatusBadRequest},
		"policy fails": {
			p: &fakePresenter{presented: presented()}, e: &fakeEvaluator{err: errors.New("boom")},
			status: http.StatusInternalServerError, completed: &presentation.Outcome{Decision: authz.Indeterminate, Reason: "policy evaluation failed"},
		},
		"policy disappeared": {
			p: &fakePresenter{presented: &presentation.Presented{Request: presentation.Request{PolicyID: "gone"}, Credentials: []presentation.PresentedCredential{{Type: "Diploma"}}}}, e: &fakeEvaluator{},
			status: http.StatusNotFound, completed: &presentation.Outcome{Decision: authz.NotApplicable, Reason: "unknown policy"},
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
	p.Credentials[0].Claims = map[string]any{"department": "burgerzaken", "email": "ada@example.com", "level": float64(3), "courses": []any{"a", "b"}}
	e := &fakeEvaluator{result: authz.Result{Decision: authz.Permit}}
	s := newPresentationServer(t, &fakePresenter{presented: p}, e)

	postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})

	want := map[string]string{"department": "burgerzaken", "email": "ada@example.com", "level": "3", "courses": `["a","b"]`}
	got := e.evaluated.Credentials[p.Credentials[0].Type].Claims
	if len(got) != len(want) {
		t.Fatalf("policy got claims %v, want %v", got, want)
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("claim %s = %q, want %q", name, got[name], value)
		}
	}
}

func TestHandlePresentationOutcome(t *testing.T) {
	cases := map[string]struct {
		p      *fakePresenter
		status int
		want   presentationOutcomeResponse
	}{
		"pending": {p: &fakePresenter{}, status: http.StatusOK, want: presentationOutcomeResponse{Status: "pending"}},
		"allowed": {
			p:      &fakePresenter{outcome: &presentation.Outcome{Decision: authz.Permit, Reason: "ok", Subject: &presentation.Subject{Issuer: "did:key:i", ID: "ada@example.com", Claim: "email"}}},
			status: http.StatusOK,
			want:   presentationOutcomeResponse{Status: "done", Decision: "Permit", Reason: "ok", Subject: &subjectResponse{Issuer: "did:key:i", ID: "ada@example.com", Email: "ada@example.com"}},
		},
		"allowed by a workload": {
			p:      &fakePresenter{outcome: &presentation.Outcome{Decision: authz.Permit, Reason: "ok", Subject: &presentation.Subject{Issuer: "did:key:i", ID: "spiffe://corp/ns/cicd/sa/agent", Claim: "workload"}}},
			status: http.StatusOK,
			want:   presentationOutcomeResponse{Status: "done", Decision: "Permit", Reason: "ok", Subject: &subjectResponse{Issuer: "did:key:i", ID: "spiffe://corp/ns/cicd/sa/agent"}},
		},
		"denied": {
			p: &fakePresenter{outcome: &presentation.Outcome{Decision: authz.Deny, Reason: "issuer not trusted"}}, status: http.StatusOK,
			want: presentationOutcomeResponse{Status: "done", Decision: "Deny", Reason: "issuer not trusted"},
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
			if got.Status != tc.want.Status || got.Reason != tc.want.Reason || got.Decision != tc.want.Decision ||
				(got.Subject == nil) != (tc.want.Subject == nil) ||
				(got.Subject != nil && *got.Subject != *tc.want.Subject) {
				t.Errorf("response = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func sameOutcome(a, b presentation.Outcome) bool {
	if a.Decision != b.Decision || a.Reason != b.Reason || (a.Subject == nil) != (b.Subject == nil) {
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
	e := &fakeEvaluator{result: authz.Result{Decision: authz.Permit}}
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

type fakeDecisionLog struct {
	decisions []adl.Decision
	err       error
}

func (f *fakeDecisionLog) Log(_ context.Context, d adl.Decision) error {
	f.decisions = append(f.decisions, d)
	return f.err
}

const callersTraceParent = "00-28dbeec32e77635cc19bc3204ec56c41-dec5220770f8f4f4-01"

func answer(s *Server) *httptest.ResponseRecorder {
	return postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})
}

func TestTheRequestKeepsTheCallersTraceparent(t *testing.T) {
	p := &fakePresenter{}
	s := newPresentationServer(t, p, &fakeEvaluator{})

	doRequest(s, "POST", "/v1/authorize/requests", requestBody, map[string]string{"traceparent": callersTraceParent})
	if p.gotRequest.TraceParent != callersTraceParent {
		t.Errorf("traceparent = %q", p.gotRequest.TraceParent)
	}
	doRequest(s, "POST", "/v1/authorize/requests", requestBody, map[string]string{"traceparent": "garbage"})
	if p.gotRequest.TraceParent != "" {
		t.Errorf("a malformed traceparent was kept: %q", p.gotRequest.TraceParent)
	}
}

func TestEveryDecisionIsLoggedBeforeItsOutcomeIsRecorded(t *testing.T) {
	for name, decision := range map[string]authz.Decision{"permit": authz.Permit, "deny": authz.Deny} {
		t.Run(name, func(t *testing.T) {
			pr := presented()
			pr.Request.TraceParent, pr.Request.UserRoles = callersTraceParent, []string{"laadpalen-aanvrager"}
			p := &fakePresenter{presented: pr}
			log := &fakeDecisionLog{}
			s := newPresentationServer(t, p, &fakeEvaluator{result: authz.Result{Decision: decision, Reason: "r"}})
			s.Decisions = log

			if rec := answer(s); rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			if len(log.decisions) != 1 {
				t.Fatalf("logged %d decisions, want 1", len(log.decisions))
			}
			d := log.decisions[0]
			if d.Allow != (decision == authz.Permit) || d.Reason != "r" || d.DecidedBy != "policy" || d.TraceParent != callersTraceParent || d.RequestID != "req-1" ||
				d.SubjectID != pr.Subject.ID || d.Issuer != pr.Credentials[0].Issuer || len(d.UserRoles) != 1 || d.Err != nil {
				t.Errorf("decision = %+v", d)
			}
			if p.completed == nil || p.completed.Decision != decision {
				t.Errorf("outcome = %+v", p.completed)
			}
		})
	}
}

func TestAnAnswerThatDoesNotVerifyIsLogged(t *testing.T) {
	req := presentation.Request{Resource: "vault", PolicyID: "p1", Credentials: []presentation.CredentialQuery{{CredentialType: "Diploma"}}, TraceParent: callersTraceParent}
	p := &fakePresenter{respondErr: &presentation.Rejected{Request: req, Err: errors.New("invalid presentation: state does not match the request")}}
	log := &fakeDecisionLog{}
	s := newPresentationServer(t, p, &fakeEvaluator{})
	s.Decisions = log

	if rec := answer(s); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(log.decisions) != 1 {
		t.Fatalf("logged %d decisions, want 1", len(log.decisions))
	}
	d := log.decisions[0]
	if d.Allow || d.DecidedBy != "attesta" || d.Reason != "credential could not be verified" || d.Detail == "" || d.Resource != "vault" ||
		d.TraceParent != callersTraceParent || d.SubjectID != "" || d.Err != nil {
		t.Errorf("decision = %+v", d)
	}
	if p.completed == nil || p.completed.Decision != authz.Deny || p.completed.Reason != "presentation did not verify" {
		t.Errorf("outcome = %+v", p.completed)
	}
}

func TestARevokedCredentialIsDeniedWithItsOwnReason(t *testing.T) {
	req := presentation.Request{Resource: "vault", PolicyID: "p1", Credentials: []presentation.CredentialQuery{{CredentialType: "Diploma"}}}
	p := &fakePresenter{respondErr: &presentation.Rejected{Request: req, Err: fmt.Errorf("%w: the Diploma credential: %w", presentation.ErrInvalidPresentation, status.ErrRevoked)}}
	log := &fakeDecisionLog{}
	s := newPresentationServer(t, p, &fakeEvaluator{})
	s.Decisions = log

	if rec := answer(s); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(log.decisions) != 1 || log.decisions[0].Allow || log.decisions[0].Reason != "credential has been revoked" || log.decisions[0].Detail == "" {
		t.Errorf("decisions = %+v", log.decisions)
	}
	if p.completed == nil || p.completed.Decision != authz.Deny || p.completed.Reason != "credential has been revoked" {
		t.Errorf("outcome = %+v", p.completed)
	}
}

func TestAnAnswerToAnUnknownOrAnsweredRequestIsNotADecision(t *testing.T) {
	for _, err := range []error{presentation.ErrUnknownRequest, presentation.ErrAlreadyAnswered} {
		log := &fakeDecisionLog{}
		s := newPresentationServer(t, &fakePresenter{respondErr: err}, &fakeEvaluator{})
		s.Decisions = log
		answer(s)
		if len(log.decisions) != 0 {
			t.Errorf("%v: logged %+v", err, log.decisions)
		}
	}
}

func TestAFailedEvaluationIsLoggedAsAnError(t *testing.T) {
	p := &fakePresenter{presented: presented()}
	log := &fakeDecisionLog{}
	s := newPresentationServer(t, p, &fakeEvaluator{err: errors.New("policy error")})
	s.Decisions = log

	if rec := answer(s); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(log.decisions) != 1 || log.decisions[0].Err == nil || log.decisions[0].SubjectID == "" {
		t.Errorf("decisions = %+v", log.decisions)
	}
	if p.completed == nil || p.completed.Decision != authz.Indeterminate {
		t.Errorf("outcome = %+v", p.completed)
	}
}

func TestNoDecisionIsReturnedWhenItCannotBeLogged(t *testing.T) {
	p := &fakePresenter{presented: presented()}
	s := newPresentationServer(t, p, &fakeEvaluator{result: authz.Result{Decision: authz.Permit, Reason: "ok"}})
	s.Decisions = &fakeDecisionLog{err: errors.New("disk full")}

	if rec := answer(s); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if p.completed == nil || p.completed.Decision != authz.Indeterminate || p.completed.Reason != "decision could not be logged" || p.completed.Subject != nil {
		t.Errorf("outcome = %+v, want a denial", p.completed)
	}
}

func TestOutcomeResponseCarriesObligationsAndAdvice(t *testing.T) {
	p := &fakePresenter{outcome: &presentation.Outcome{
		Decision: authz.Permit, Reason: "ok",
		Obligations: []authz.Statement{{ID: "log-access", Attributes: []authz.Attribute{{ID: "retention-days", Value: "30"}}}, {ID: "notify-owner"}},
		Advice:      []authz.Statement{{ID: "show-banner", Attributes: []authz.Attribute{{ID: "text", Value: "welcome"}}}},
	}}
	s := newPresentationServer(t, p, &fakeEvaluator{})
	rec := doRequest(s, "GET", "/v1/authorize/requests/req-1", "", nil)
	const want = `{"status":"done","decision":"Permit","reason":"ok",` +
		`"obligations":[{"id":"log-access","attributeAssignments":[{"attributeId":"retention-days","value":"30"}]},{"id":"notify-owner","attributeAssignments":[]}],` +
		`"advice":[{"id":"show-banner","attributeAssignments":[{"attributeId":"text","value":"welcome"}]}]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestSeveralCredentialsTravelFromTheRequestToThePolicyAndTheLog(t *testing.T) {
	p := &fakePresenter{}
	s := newPresentationServer(t, p, &fakeEvaluator{})
	body := `{"resource":"vault","policyId":"p1","credentials":[{"credentialType":"Employee","claims":["department"]},{"credentialType":"Certificate","claims":["course"]}]}`
	if rec := doRequest(s, "POST", "/v1/authorize/requests", body, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	want := []presentation.CredentialQuery{{CredentialType: "Employee", Claims: []string{"department"}}, {CredentialType: "Certificate", Claims: []string{"course"}}}
	if !reflect.DeepEqual(p.gotRequest.Credentials, want) {
		t.Fatalf("request = %+v", p.gotRequest)
	}

	answered := presented()
	answered.Credentials = append(answered.Credentials, presentation.PresentedCredential{Type: "Certificate", Issuer: "did:key:z6Mkc", Claims: map[string]any{"course": "laadpalen-management"}})
	e := &fakeEvaluator{result: authz.Result{Decision: authz.Permit}}
	log := &fakeDecisionLog{}
	s = newPresentationServer(t, &fakePresenter{presented: answered}, e)
	s.Decisions = log
	postForm(s, "/v1/authorize/requests/req-1/response", url.Values{"vp_token": {"{}"}, "state": {"s"}})

	got := e.evaluated.Credentials["Certificate"]
	if got.Issuer != "did:key:z6Mkc" || got.Claims["course"] != "laadpalen-management" || len(e.evaluated.Credentials) != 2 {
		t.Errorf("policy got %+v", e.evaluated)
	}
	if len(log.decisions) != 1 || len(log.decisions[0].Also) != 1 || log.decisions[0].Also[0].Type != "Certificate" {
		t.Errorf("log got %+v", log.decisions)
	}
}

func TestRefusesRequestsThatAskForCredentialsTheWrongWay(t *testing.T) {
	s := newPresentationServer(t, &fakePresenter{}, &fakeEvaluator{})
	for name, body := range map[string]string{
		"two of one type":      `{"resource":"v","policyId":"p1","credentials":[{"credentialType":"Employee"},{"credentialType":"Employee"}]}`,
		"a credential no type": `{"resource":"v","policyId":"p1","credentials":[{"credentialType":"Employee"},{"credentialType":""}]}`,
		"both ways":            `{"resource":"v","policyId":"p1","credentialType":"Employee","credentials":[{"credentialType":"Certificate"}]}`,
		"none":                 `{"resource":"v","policyId":"p1"}`,
	} {
		if rec := doRequest(s, "POST", "/v1/authorize/requests", body, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", name, rec.Code)
		}
	}
}
