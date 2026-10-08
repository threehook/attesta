package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAttesta stands in for the sidecar. Its requests are numbered req-1, req-2, ... in the order they were started.
type fakeAttesta struct {
	startErr   error
	outcomeErr error
	answer     outcome
	// answers overrides answer for a request id.
	answers map[string]outcome
	raw     string

	started      []startedWith
	outcomeCalls int
}

type startedWith struct {
	resource, policyID, credentialType string
	claims, userRoles                  []string
}

func (f *fakeAttesta) start(_ context.Context, resource, policyID, credentialType string, claims, userRoles []string) (authorizationRequest, error) {
	f.started = append(f.started, startedWith{resource, policyID, credentialType, claims, userRoles})
	if f.startErr != nil {
		return authorizationRequest{}, f.startErr
	}
	return authorizationRequest{RequestID: fmt.Sprintf("req-%d", len(f.started)), Link: "openid4vp://?x=1"}, nil
}

func (f *fakeAttesta) outcome(_ context.Context, id string) (outcome, json.RawMessage, error) {
	f.outcomeCalls++
	if a, ok := f.answers[id]; ok {
		return a, json.RawMessage(f.raw), f.outcomeErr
	}
	return f.answer, json.RawMessage(f.raw), f.outcomeErr
}

var (
	jerry = &subject{Issuer: "did:key:issuer", Email: "jerry@example.com"}
	tom   = &subject{Issuer: "did:key:issuer", Email: "tom@example.com"}
)

const freeAddress = `{"postcode":"1111BB","huisnummer":"2"}`

func authorizedAttesta() *fakeAttesta {
	return &fakeAttesta{answer: outcome{Status: "done", Allow: true, Reason: "Geautoriseerd", Subject: jerry}}
}

func newTestServer(f *fakeAttesta) *server {
	return newServer(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// withCookies is like call, but the request carries the given cookies and extra headers.
func withCookies(s *server, method, path, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func call(s *server, method, path, body string) *httptest.ResponseRecorder {
	return withCookies(s, method, path, body, nil, nil)
}

func sessionOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

// signedIn runs the sign-in for the answer the fake gives, and returns the session cookie. The sign-in is the next request the fake numbers.
func signedIn(t *testing.T, s *server, f *fakeAttesta) *http.Cookie {
	t.Helper()
	rec := call(s, "POST", "/api/sign-in", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: status = %d: %s", rec.Code, rec.Body)
	}
	id := fmt.Sprintf("req-%d", len(f.started))
	c := sessionOf(call(s, "GET", "/api/sign-in/"+id, ""))
	if c == nil {
		t.Fatal("the authorized sign-in did not start a session")
	}
	return c
}

// submitAs makes a laadpaal request in the session and returns its id.
func submitAs(t *testing.T, s *server, f *fakeAttesta, c *http.Cookie, body string) string {
	t.Helper()
	if rec := withCookies(s, "POST", "/api/request-laadpaal", body, []*http.Cookie{c}, nil); rec.Code != http.StatusOK {
		t.Fatalf("submit: status = %d: %s", rec.Code, rec.Body)
	}
	return fmt.Sprintf("req-%d", len(f.started))
}

func statusOf(t *testing.T, s *server, id string) submissionView {
	t.Helper()
	rec := call(s, "GET", "/api/request-laadpaal/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", rec.Code, rec.Body)
	}
	var v submissionView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSignInAsksAttestaForTheIdentityOnly(t *testing.T) {
	f := &fakeAttesta{}
	s := newTestServer(f)

	rec := call(s, "POST", "/api/sign-in", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.RequestID != "req-1" || got.Link != "openid4vp://?x=1" {
		t.Errorf("response = %s", rec.Body)
	}
	if len(f.started) != 1 {
		t.Fatalf("attesta was asked %d times, want once", len(f.started))
	}
	if g := f.started[0]; g.resource != "sign_in" || g.policyID != "sign_in" || g.credentialType != "Employee" || len(g.claims) != 0 {
		t.Errorf("asked attesta for %+v, want the Employee identity for sign_in and no other claims", g)
	}
}

func TestSignInWhenAttestaFails(t *testing.T) {
	s := newTestServer(&fakeAttesta{startErr: errors.New("connection refused")})
	if rec := call(s, "POST", "/api/sign-in", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestSubmitNeedsASignIn(t *testing.T) {
	f := &fakeAttesta{}
	s := newTestServer(f)
	forged := &http.Cookie{Name: sessionCookie, Value: "guess"}

	for name, cookies := range map[string][]*http.Cookie{"no cookie": nil, "forged cookie": {forged}} {
		t.Run(name, func(t *testing.T) {
			if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, cookies, nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}
	if len(f.started) != 0 {
		t.Error("attesta was asked for a request nobody was signed in for")
	}
}

func TestSubmitAsksAttestaForTheEmployeeClaims(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)

	rec := withCookies(s, "POST", "/api/request-laadpaal", `{"postcode":" 1111 bb ","huisnummer":"2"}`, []*http.Cookie{c}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.RequestID != "req-2" || got.Link != "openid4vp://?x=1" {
		t.Errorf("response = %s", rec.Body)
	}
	if len(f.started) != 2 {
		t.Fatalf("attesta was asked %d times, want a sign-in and a request", len(f.started))
	}
	if g := f.started[1]; g.resource != "request_laadpaal" || g.policyID != "request_laadpaal" || g.credentialType != "Employee" || strings.Join(g.claims, ",") != "department,diploma" {
		t.Errorf("asked attesta for %+v, want the Employee department and diploma for request_laadpaal", g)
	}
}

func TestSubmitRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"not json":          `{`,
		"no postcode":       `{"huisnummer":"1"}`,
		"short postcode":    `{"postcode":"1111A","huisnummer":"1"}`,
		"no house number":   `{"postcode":"1111AA"}`,
		"house number text": `{"postcode":"1111AA","huisnummer":"x"}`,
		"house number zero": `{"postcode":"1111AA","huisnummer":"0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := authorizedAttesta()
			s := newTestServer(f)
			c := signedIn(t, s, f)
			if rec := withCookies(s, "POST", "/api/request-laadpaal", body, []*http.Cookie{c}, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if len(f.started) != 1 {
				t.Error("attesta was asked for a request with bad input")
			}
		})
	}
}

func TestSubmitWhenAttestaFails(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	f.startErr = errors.New("connection refused")
	if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestStatusIsPendingUntilTheWalletAnswers(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	f.answers = map[string]outcome{"req-2": {Status: "pending"}}
	id := submitAs(t, s, f, c, freeAddress)

	v := statusOf(t, s, id)
	if v.Status != "pending" || v.Result != nil || v.Debug.AuthorizationRequest != "openid4vp://?x=1" {
		t.Errorf("view = %+v, want pending with the wallet link", v)
	}
	if len(s.records) != 0 {
		t.Error("something was recorded before attesta decided")
	}
}

func TestAuthorizedSubmissionIsCarriedOutOnceAndRecorded(t *testing.T) {
	f := authorizedAttesta()
	f.raw = `{"status":"done"}`
	s := newTestServer(f)
	c := signedIn(t, s, f)
	id := submitAs(t, s, f, c, freeAddress)
	callsBefore := f.outcomeCalls

	for i := 0; i < 3; i++ {
		v := statusOf(t, s, id)
		if v.Status != "done" || !v.Authorized || v.Subject == nil || v.Subject.Email != "jerry@example.com" ||
			v.Result == nil || !v.Result.Granted || v.Result.Reason != "Toegekend" || string(v.Debug.Outcome) != `{"status":"done"}` {
			t.Fatalf("poll %d: view = %+v", i, v)
		}
	}
	if len(s.records) != 1 {
		t.Fatalf("recorded %d requests after three polls, want 1", len(s.records))
	}
	if r := s.records[0]; r.Subject != *jerry || r.Postcode != "1111BB" || r.HouseNumber != 2 || !r.Result.Granted {
		t.Errorf("record = %+v", r)
	}
	if n := f.outcomeCalls - callsBefore; n != 1 {
		t.Errorf("attesta was asked for the decision %d times, want once: it is final", n)
	}
}

func TestAuthorizedSubmissionAppliesTheAddressRules(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		granted bool
		reason  string
	}{
		"free address":          {`{"postcode":"1111BB","huisnummer":"2"}`, true, "Toegekend"},
		"laadpaal present":      {`{"postcode":"1111AA","huisnummer":"1"}`, false, "Reeds laadpaal aanwezig"},
		"no electric vehicle":   {`{"postcode":"1111DD","huisnummer":"4"}`, false, "Geen elektrisch voertuig gevonden op adres"},
		"unknown address":       {`{"postcode":"9999ZZ","huisnummer":"99"}`, false, "Postcode/huisnummer niet gevonden"},
		"lower case, spaced in": {`{"postcode":"1111 bb","huisnummer":" 2 "}`, true, "Toegekend"},
	} {
		t.Run(name, func(t *testing.T) {
			f := authorizedAttesta()
			s := newTestServer(f)
			id := submitAs(t, s, f, signedIn(t, s, f), tc.body)

			v := statusOf(t, s, id)
			if !v.Authorized || v.Result == nil || v.Result.Granted != tc.granted || v.Result.Reason != tc.reason {
				t.Errorf("view = %+v, want granted=%v reason=%q", v, tc.granted, tc.reason)
			}
		})
	}
}

func TestSubmissionAttestaDeniesGetsNoResult(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	f.answers = map[string]outcome{"req-2": {Status: "done", Allow: false, Reason: "Niet geautoriseerd vanwege afdeling"}}
	id := submitAs(t, s, f, c, freeAddress)

	v := statusOf(t, s, id)
	if v.Status != "done" || v.Authorized || v.Reason != "Niet geautoriseerd vanwege afdeling" || v.Result != nil || v.Subject != nil {
		t.Errorf("view = %+v, want a denial with no result and no subject", v)
	}
	if len(s.records) != 0 {
		t.Error("a request nobody was authorized to submit was recorded")
	}
}

func TestSubmissionWithAnotherEmployeesCredentialIsDenied(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	f.answers = map[string]outcome{"req-2": {Status: "done", Allow: true, Reason: "Geautoriseerd", Subject: tom}}
	id := submitAs(t, s, f, c, freeAddress)

	v := statusOf(t, s, id)
	if v.Status != "done" || v.Authorized || v.Result != nil || v.Subject != nil || v.Reason == "" {
		t.Errorf("view = %+v, want a denial: the credential is not the signed-in employee's", v)
	}
	if len(s.records) != 0 {
		t.Error("a request with another employee's credential was recorded")
	}
}

func TestStatusWhenAttestaForgotTheRequest(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	f.answers = map[string]outcome{}
	f.outcomeErr = errUnknownRequest
	id := submitAs(t, s, f, c, freeAddress)

	if v := statusOf(t, s, id); v.Status != "expired" || v.Result != nil {
		t.Errorf("view = %+v, want expired", v)
	}
}

func TestStatusErrors(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	id := submitAs(t, s, f, c, freeAddress)

	for name, path := range map[string]string{
		"unknown id":                 "/api/request-laadpaal/nope",
		"a sign-in is not a request": "/api/request-laadpaal/req-1",
		"a request is not a sign-in": "/api/sign-in/" + id,
		"unknown id for a sign-in":   "/api/sign-in/nope",
	} {
		if rec := call(s, "GET", path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, rec.Code)
		}
	}

	down := &fakeAttesta{outcomeErr: errors.New("connection refused")}
	s = newTestServer(down)
	call(s, "POST", "/api/sign-in", "")
	if rec := call(s, "GET", "/api/sign-in/req-1", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("attesta down: status = %d, want 502", rec.Code)
	}
}

func TestOldSubmissionsAreForgotten(t *testing.T) {
	clock := time.Now()
	f := authorizedAttesta()
	s := newTestServer(f)
	s.now = func() time.Time { return clock }
	c := signedIn(t, s, f)
	submitAs(t, s, f, c, freeAddress)

	clock = clock.Add(submissionTTL + time.Minute)
	call(s, "POST", "/api/sign-in", "")

	if len(s.submissions) != 1 {
		t.Errorf("%d submissions are kept, want only the new one", len(s.submissions))
	}
}

func TestHealthz(t *testing.T) {
	if rec := call(newTestServer(&fakeAttesta{}), "GET", "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestSubmitSendsTheEmployeesRoles(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	s.userRoles = parseUserRoles("Jerry@Example.com=laadpalen-aanvrager;tom@example.com=reader")
	c := signedIn(t, s, f)

	withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil)

	if g := f.started[0]; len(g.userRoles) != 0 {
		t.Errorf("the sign-in sent roles %v", g.userRoles)
	}
	if g := f.started[1]; strings.Join(g.userRoles, ",") != "laadpalen-aanvrager" {
		t.Errorf("the request sent roles %v, want laadpalen-aanvrager", g.userRoles)
	}
}

func TestSubmitSendsNoRolesForAnEmployeeWithoutAny(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	s.userRoles = parseUserRoles("tom@example.com=laadpalen-aanvrager")
	c := signedIn(t, s, f)

	withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil)

	if g := f.started[1]; len(g.userRoles) != 0 {
		t.Errorf("the request sent roles %v, want none", g.userRoles)
	}
}

func TestParseUserRoles(t *testing.T) {
	got := parseUserRoles(" Ada@Example.com = a, b ;bob@example.com=;;=x;broken")
	if len(got) != 1 || strings.Join(got["ada@example.com"], ",") != "a,b" {
		t.Errorf("parseUserRoles = %v", got)
	}
}
