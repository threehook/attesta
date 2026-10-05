package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const freeAddress = `{"postcode":"1111BB","huisnummer":"2"}`

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

// signedIn runs the wallet flow for jerry and returns the session cookie the status response set.
func signedIn(t *testing.T, s *server) *http.Cookie {
	t.Helper()
	submit(t, s, freeAddress)
	rec := call(s, "GET", "/api/request-laadpaal/req-1", "")
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("the authorized outcome did not start a session")
	return nil
}

func authorizedAttesta() *fakeAttesta {
	return &fakeAttesta{answer: outcome{Status: "done", Allow: true, Reason: "Geautoriseerd", Subject: jerry}}
}

func TestAuthorizedOutcomeStartsASession(t *testing.T) {
	s := newTestServer(authorizedAttesta())
	c := signedIn(t, s)

	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Value == "" {
		t.Errorf("cookie = %+v, want an HttpOnly, SameSite=Lax cookie for the whole app", c)
	}
	if c.Secure {
		t.Error("the cookie is Secure on plain http, so the browser would drop it")
	}
	rec := withCookies(s, "GET", "/api/session", "", []*http.Cookie{c}, nil)
	var got sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.Active || got.Subject == nil || got.Subject.Email != "jerry@example.com" {
		t.Errorf("session = %s", rec.Body)
	}
}

func TestSessionCookieIsSecureBehindHTTPS(t *testing.T) {
	s := newTestServer(authorizedAttesta())
	submit(t, s, freeAddress)
	rec := withCookies(s, "GET", "/api/request-laadpaal/req-1", "", nil, map[string]string{"X-Forwarded-Proto": "https"})
	if cs := rec.Result().Cookies(); len(cs) != 1 || !cs[0].Secure {
		t.Errorf("cookies = %+v, want one Secure cookie", cs)
	}
}

func TestPollingAgainDoesNotStartAnotherSession(t *testing.T) {
	s := newTestServer(authorizedAttesta())
	signedIn(t, s)
	if rec := call(s, "GET", "/api/request-laadpaal/req-1", ""); len(rec.Result().Cookies()) != 0 {
		t.Error("a second poll of the same decision set another cookie")
	}
	if len(s.sessions) != 1 {
		t.Errorf("%d sessions, want 1", len(s.sessions))
	}
}

func TestSubmissionInASessionSkipsAttestaAndRecordsTheEmployee(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s)
	askedBefore := len(f.started)

	rec := withCookies(s, "POST", "/api/request-laadpaal", `{"postcode":"1111AA","huisnummer":"1"}`, []*http.Cookie{c}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var got submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.RequestID == "" || got.Link != "" {
		t.Fatalf("response = %s, want a request id and no wallet link", rec.Body)
	}
	if len(f.started) != askedBefore {
		t.Error("attesta was asked again although the session was valid")
	}

	rec = withCookies(s, "GET", "/api/request-laadpaal/"+got.RequestID, "", []*http.Cookie{c}, nil)
	var v submissionView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Status != "done" || !v.Authorized || v.Subject == nil || *v.Subject != *jerry || v.Result == nil || v.Result.Reason != "Reeds laadpaal aanwezig" {
		t.Errorf("view = %+v, want the freeAddress rules applied for jerry", v)
	}
	if len(s.records) != 2 || s.records[1].Subject != *jerry || s.records[1].Postcode != "1111AA" {
		t.Errorf("records = %+v, want the session submission recorded with its employee", s.records)
	}
}

func TestSessionSubmissionStillChecksTheInput(t *testing.T) {
	s := newTestServer(authorizedAttesta())
	c := signedIn(t, s)
	if rec := withCookies(s, "POST", "/api/request-laadpaal", `{"postcode":"x","huisnummer":"2"}`, []*http.Cookie{c}, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDeniedOutcomeStartsNoSession(t *testing.T) {
	s := newTestServer(&fakeAttesta{answer: outcome{Status: "done", Allow: false, Reason: "Niet geautoriseerd vanwege afdeling"}})
	submit(t, s, freeAddress)
	if rec := call(s, "GET", "/api/request-laadpaal/req-1", ""); len(rec.Result().Cookies()) != 0 || len(s.sessions) != 0 {
		t.Error("a denial started a session")
	}
}

func TestUnknownOrForgedCookieNeedsTheWallet(t *testing.T) {
	f := &fakeAttesta{}
	s := newTestServer(f)
	forged := &http.Cookie{Name: sessionCookie, Value: "guess"}

	rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{forged}, nil)
	var got submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Link == "" || len(f.started) != 1 {
		t.Errorf("response = %s, attesta asked %d times: a forged cookie must lead to the wallet", rec.Body, len(f.started))
	}
}

func TestSessionExpiresAfterItsLifetime(t *testing.T) {
	clock := time.Now()
	f := authorizedAttesta()
	s := newTestServer(f)
	s.now = func() time.Time { return clock }
	c := signedIn(t, s)

	clock = clock.Add(sessionTTL - time.Minute)
	if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil); strings.Contains(rec.Body.String(), "openid4vp") {
		t.Fatal("the session ended early")
	}
	// Using the session did not extend it.
	clock = clock.Add(2 * time.Minute)
	started := len(f.started)
	rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil)
	if !strings.Contains(rec.Body.String(), "openid4vp") || len(f.started) != started+1 {
		t.Errorf("response = %s: an expired session must lead to the wallet", rec.Body)
	}
	var got sessionView
	rec = withCookies(s, "GET", "/api/session", "", []*http.Cookie{c}, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Active {
		t.Errorf("session = %s, want inactive", rec.Body)
	}
}

func TestSignOutEndsTheSession(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s)

	rec := withCookies(s, "DELETE", "/api/session", "", []*http.Cookie{c}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if cs := rec.Result().Cookies(); len(cs) != 1 || cs[0].MaxAge >= 0 {
		t.Errorf("cookies = %+v, want the cookie cleared", cs)
	}
	started := len(f.started)
	rec = withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil)
	if !strings.Contains(rec.Body.String(), "openid4vp") || len(f.started) != started+1 {
		t.Error("a signed-out session still skipped the wallet")
	}
}

func TestNoSessionWithoutCookie(t *testing.T) {
	rec := call(newTestServer(&fakeAttesta{}), "GET", "/api/session", "")
	var got sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Active || got.Subject != nil {
		t.Errorf("session = %s, want inactive", rec.Body)
	}
}
