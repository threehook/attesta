package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestAuthorizedSignInStartsASession(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)

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

func TestSignInStatusIsPendingUntilTheWalletAnswers(t *testing.T) {
	s := newTestServer(&fakeAttesta{answer: outcome{Status: "pending"}})
	call(s, "POST", "/api/sign-in", "")

	rec := call(s, "GET", "/api/sign-in/req-1", "")
	var v submissionView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Status != "pending" || v.Debug.AuthorizationRequest != "openid4vp://?x=1" {
		t.Errorf("view = %s, want pending with the wallet link", rec.Body)
	}
	if sessionOf(rec) != nil {
		t.Error("a pending sign-in started a session")
	}
}

func TestSessionCookieIsSecureBehindHTTPS(t *testing.T) {
	s := newTestServer(authorizedAttesta())
	call(s, "POST", "/api/sign-in", "")
	rec := withCookies(s, "GET", "/api/sign-in/req-1", "", nil, map[string]string{"X-Forwarded-Proto": "https"})
	if cs := rec.Result().Cookies(); len(cs) != 1 || !cs[0].Secure {
		t.Errorf("cookies = %+v, want one Secure cookie", cs)
	}
}

func TestPollingAgainDoesNotStartAnotherSession(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	signedIn(t, s, f)
	if rec := call(s, "GET", "/api/sign-in/req-1", ""); len(rec.Result().Cookies()) != 0 {
		t.Error("a second poll of the same decision set another cookie")
	}
	if len(s.sessions) != 1 {
		t.Errorf("%d sessions, want 1", len(s.sessions))
	}
}

func TestDeniedSignInStartsNoSession(t *testing.T) {
	s := newTestServer(&fakeAttesta{answer: outcome{Status: "done", Allow: false, Reason: "Uitgever niet vertrouwd: did:key:x"}})
	call(s, "POST", "/api/sign-in", "")
	rec := call(s, "GET", "/api/sign-in/req-1", "")
	var v submissionView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Status != "done" || v.Authorized || v.Reason == "" {
		t.Errorf("view = %s, want a denial with its reason", rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 || len(s.sessions) != 0 {
		t.Error("a denial started a session")
	}
}

func TestSubmissionDoesNotStartASession(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)
	id := submitAs(t, s, f, c, freeAddress)
	if rec := call(s, "GET", "/api/request-laadpaal/"+id, ""); len(rec.Result().Cookies()) != 0 || len(s.sessions) != 1 {
		t.Error("an authorized request started another session")
	}
}

func TestSessionExpiresAfterItsLifetime(t *testing.T) {
	clock := time.Now()
	f := authorizedAttesta()
	s := newTestServer(f)
	s.now = func() time.Time { return clock }
	c := signedIn(t, s, f)

	clock = clock.Add(sessionTTL - time.Minute)
	if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: the session ended early", rec.Code)
	}
	// Using the session did not extend it.
	clock = clock.Add(2 * time.Minute)
	if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an expired session", rec.Code)
	}
	var got sessionView
	rec := withCookies(s, "GET", "/api/session", "", []*http.Cookie{c}, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Active {
		t.Errorf("session = %s, want inactive", rec.Body)
	}
}

func TestSignOutEndsTheSession(t *testing.T) {
	f := authorizedAttesta()
	s := newTestServer(f)
	c := signedIn(t, s, f)

	rec := withCookies(s, "DELETE", "/api/session", "", []*http.Cookie{c}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if cs := rec.Result().Cookies(); len(cs) != 1 || cs[0].MaxAge >= 0 {
		t.Errorf("cookies = %+v, want the cookie cleared", cs)
	}
	if rec := withCookies(s, "POST", "/api/request-laadpaal", freeAddress, []*http.Cookie{c}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 after signing out", rec.Code)
	}
}

func TestNoSessionWithoutCookie(t *testing.T) {
	rec := call(newTestServer(&fakeAttesta{}), "GET", "/api/session", "")
	var got sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Active || got.Subject != nil {
		t.Errorf("session = %s, want inactive", rec.Body)
	}
}

func TestConfigNamesTheApplicationWalletsKnow(t *testing.T) {
	s := newTestServer(&fakeAttesta{})
	s.application = "https://api.example"
	rec := call(s, "GET", "/api/config", "")
	var got configView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Application != "https://api.example" {
		t.Errorf("config = %s", rec.Body)
	}
}
