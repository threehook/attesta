package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAttestaClientStartsARequest(t *testing.T) {
	var gotBody map[string]any
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/authorize/requests" {
			t.Errorf("sidecar got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"requestId":"abc","authorizationRequest":"openid4vp://?y=2"}`))
	}))
	defer sidecar.Close()

	got, err := newAttestaClient(sidecar.URL).start(context.Background(), "res", "pol", "Type", []string{"department", "diploma"})
	if err != nil || got.RequestID != "abc" || got.Link != "openid4vp://?y=2" {
		t.Fatalf("start = %+v, %v", got, err)
	}
	if gotBody["resource"] != "res" || gotBody["policyId"] != "pol" || gotBody["credentialType"] != "Type" ||
		strings.Join(toStrings(gotBody["claims"]), ",") != "department,diploma" {
		t.Errorf("sidecar got body %v", gotBody)
	}
}

func TestAttestaClientStartFailures(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"sidecar error": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"unknown policy"}`, http.StatusNotFound)
		},
		"empty answer": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
		"not json":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`oops`)) },
	} {
		t.Run(name, func(t *testing.T) {
			sidecar := httptest.NewServer(handler)
			defer sidecar.Close()
			if _, err := newAttestaClient(sidecar.URL).start(context.Background(), "r", "p", "t", nil); err == nil {
				t.Error("want an error")
			}
		})
	}

	if _, err := newAttestaClient("http://127.0.0.1:1").start(context.Background(), "r", "p", "t", nil); err == nil {
		t.Error("sidecar not running: want an error")
	}
}

func TestAttestaClientReadsTheOutcome(t *testing.T) {
	const answer = `{"status":"done","allow":true,"reason":"Geautoriseerd","subject":{"issuer":"did:key:g","email":"jerry@example.com"}}`
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/authorize/requests/abc":
			_, _ = w.Write([]byte(answer))
		case "/v1/authorize/requests/gone":
			http.Error(w, "unknown or expired request", http.StatusNotFound)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer sidecar.Close()
	c := newAttestaClient(sidecar.URL)

	got, raw, err := c.outcome(context.Background(), "abc")
	if err != nil || got.Status != "done" || !got.Allow || got.Subject == nil || got.Subject.Email != "jerry@example.com" || string(raw) != answer {
		t.Errorf("outcome = %+v, %s, %v", got, raw, err)
	}
	if _, _, err := c.outcome(context.Background(), "gone"); !errors.Is(err, errUnknownRequest) {
		t.Errorf("unknown request: error = %v, want errUnknownRequest", err)
	}
	if _, _, err := c.outcome(context.Background(), "other"); err == nil || errors.Is(err, errUnknownRequest) {
		t.Errorf("server error: error = %v, want a plain error", err)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}
