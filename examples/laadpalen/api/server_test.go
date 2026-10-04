package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAttesta stands in for the sidecar.
type fakeAttesta struct {
	startErr   error
	outcomeErr error
	answer     outcome
	raw        string

	started      []startedWith
	outcomeCalls int
}

type startedWith struct {
	resource, policyID, credentialType string
	claims                             []string
}

func (f *fakeAttesta) start(_ context.Context, resource, policyID, credentialType string, claims []string) (authorizationRequest, error) {
	f.started = append(f.started, startedWith{resource, policyID, credentialType, claims})
	if f.startErr != nil {
		return authorizationRequest{}, f.startErr
	}
	return authorizationRequest{RequestID: "req-1", Link: "openid4vp://?x=1"}, nil
}

func (f *fakeAttesta) outcome(context.Context, string) (outcome, json.RawMessage, error) {
	f.outcomeCalls++
	return f.answer, json.RawMessage(f.raw), f.outcomeErr
}

var jerry = &subject{Issuer: "did:key:issuer", Email: "jerry@example.com"}

func newTestServer(f *fakeAttesta) *server {
	return newServer(f, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func call(s *server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func submit(t *testing.T, s *server, body string) {
	t.Helper()
	if rec := call(s, "POST", "/api/request-laadpaal", body); rec.Code != http.StatusOK {
		t.Fatalf("submit: status = %d: %s", rec.Code, rec.Body)
	}
}

func status(t *testing.T, s *server) submissionView {
	t.Helper()
	rec := call(s, "GET", "/api/request-laadpaal/req-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", rec.Code, rec.Body)
	}
	var v submissionView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSubmitAsksAttestaForTheEmployeeCredential(t *testing.T) {
	f := &fakeAttesta{}
	s := newTestServer(f)

	rec := call(s, "POST", "/api/request-laadpaal", `{"postcode":" 1111 bb ","huisnummer":"2"}`)
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
	want := startedWith{"request_laadpaal", "request_laadpaal", "Employee", []string{"department", "diploma"}}
	if g := f.started[0]; g.resource != want.resource || g.policyID != want.policyID || g.credentialType != want.credentialType || strings.Join(g.claims, ",") != "department,diploma" {
		t.Errorf("asked attesta for %+v, want %+v", g, want)
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
			f := &fakeAttesta{}
			if rec := call(newTestServer(f), "POST", "/api/request-laadpaal", body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if len(f.started) != 0 {
				t.Error("attesta was asked for a request with bad input")
			}
		})
	}
}

func TestSubmitWhenAttestaFails(t *testing.T) {
	s := newTestServer(&fakeAttesta{startErr: errors.New("connection refused")})
	if rec := call(s, "POST", "/api/request-laadpaal", `{"postcode":"1111AA","huisnummer":"1"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestStatusIsPendingUntilTheWalletAnswers(t *testing.T) {
	f := &fakeAttesta{answer: outcome{Status: "pending"}}
	s := newTestServer(f)
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	v := status(t, s)
	if v.Status != "pending" || v.Result != nil || v.Debug.AuthorizationRequest != "openid4vp://?x=1" {
		t.Errorf("view = %+v, want pending with the wallet link", v)
	}
	if len(s.records) != 0 {
		t.Error("something was recorded before attesta decided")
	}
}

func TestAuthorizedSubmissionIsCarriedOutOnceAndRecorded(t *testing.T) {
	f := &fakeAttesta{answer: outcome{Status: "done", Allow: true, Reason: "Geautoriseerd", Subject: jerry}, raw: `{"status":"done"}`}
	s := newTestServer(f)
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	for i := 0; i < 3; i++ {
		v := status(t, s)
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
	if f.outcomeCalls != 1 {
		t.Errorf("attesta was asked for the decision %d times, want once: it is final", f.outcomeCalls)
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
			s := newTestServer(&fakeAttesta{answer: outcome{Status: "done", Allow: true, Reason: "Geautoriseerd", Subject: jerry}})
			submit(t, s, tc.body)

			v := status(t, s)
			if !v.Authorized || v.Result == nil || v.Result.Granted != tc.granted || v.Result.Reason != tc.reason {
				t.Errorf("view = %+v, want granted=%v reason=%q", v, tc.granted, tc.reason)
			}
		})
	}
}

func TestSubmissionAttestaDeniesGetsNoResult(t *testing.T) {
	s := newTestServer(&fakeAttesta{answer: outcome{Status: "done", Allow: false, Reason: "Niet geautoriseerd vanwege afdeling"}})
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	v := status(t, s)
	if v.Status != "done" || v.Authorized || v.Reason != "Niet geautoriseerd vanwege afdeling" || v.Result != nil || v.Subject != nil {
		t.Errorf("view = %+v, want a denial with no result and no subject", v)
	}
	if len(s.records) != 0 {
		t.Error("a request nobody was authorized to submit was recorded")
	}
}

func TestStatusWhenAttestaForgotTheRequest(t *testing.T) {
	s := newTestServer(&fakeAttesta{outcomeErr: errUnknownRequest})
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	if v := status(t, s); v.Status != "expired" || v.Result != nil {
		t.Errorf("view = %+v, want expired", v)
	}
}

func TestStatusErrors(t *testing.T) {
	s := newTestServer(&fakeAttesta{})
	if rec := call(s, "GET", "/api/request-laadpaal/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}

	s = newTestServer(&fakeAttesta{outcomeErr: errors.New("connection refused")})
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)
	if rec := call(s, "GET", "/api/request-laadpaal/req-1", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("attesta down: status = %d, want 502", rec.Code)
	}
}

func TestOldSubmissionsAreForgotten(t *testing.T) {
	clock := time.Now()
	f := &fakeAttesta{}
	s := newTestServer(f)
	s.now = func() time.Time { return clock }
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	clock = clock.Add(submissionTTL + time.Minute)
	submit(t, s, `{"postcode":"1111BB","huisnummer":"2"}`)

	if len(s.submissions) != 1 {
		t.Errorf("%d submissions are kept, want only the new one", len(s.submissions))
	}
}

func TestHealthz(t *testing.T) {
	if rec := call(newTestServer(&fakeAttesta{}), "GET", "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
