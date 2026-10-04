package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/scripts"
)

// fakeEvaluator isolates httpapi's own routing/decoding/status-code logic from the real gnovm implementation, which has its own tests
// (internal/authz).
type fakeEvaluator struct {
	result    authz.Result
	err       error
	validated string // last source passed to Validate, for assertions
}

func (f *fakeEvaluator) Validate(source string) error {
	f.validated = source
	return nil
}

func (f *fakeEvaluator) Evaluate(string, authz.Input) (authz.Result, error) {
	return f.result, f.err
}

// newTestServer wires a Server with a fake standing in for authz, plus a real scripts.Store (lightweight,
// no gnovm involved since the fake Evaluator also serves as its Validator) pre-loaded with one policy.
func newTestServer(t *testing.T, evaluator *fakeEvaluator) *Server {
	t.Helper()
	policies := scripts.NewStore(evaluator)
	if err := policies.Put("p1", "package policy"); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	return &Server{
		Authz:       evaluator,
		Policies:    policies,
		AdminToken:  "admin-tok",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		CORSOrigins: "http://allowed.example",
	}
}

func doRequest(s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func TestHandleHealthz(t *testing.T) {
	s := newTestServer(t, &fakeEvaluator{})
	rec := doRequest(s, "GET", "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestWithCORS(t *testing.T) {
	s := newTestServer(t, &fakeEvaluator{})

	rec := doRequest(s, "OPTIONS", "/v1/authorize/requests", "", map[string]string{"Origin": "http://allowed.example"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "http://allowed.example")
	}

	rec = doRequest(s, "GET", "/healthz", "", map[string]string{"Origin": "http://allowed.example"})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.example" {
		t.Fatalf("CORS header on allowed origin = %q, want %q", got, "http://allowed.example")
	}
}

func TestWithCORSRejectsUnlistedOrigin(t *testing.T) {
	s := newTestServer(t, &fakeEvaluator{})

	rec := doRequest(s, "GET", "/healthz", "", map[string]string{"Origin": "http://evil.example"})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin for disallowed origin = %q, want empty", got)
	}
}

func TestHandlePutPolicyRequiresAdminToken(t *testing.T) {
	s := newTestServer(t, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/admin/policies", `{"id":"p2","source":"package policy"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	rec = doRequest(s, "POST", "/admin/policies", `{"id":"p2","source":"package policy"}`, map[string]string{"X-Admin-Token": "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlePutPolicySuccess(t *testing.T) {
	evaluator := &fakeEvaluator{}
	s := newTestServer(t, evaluator)

	rec := doRequest(s, "POST", "/admin/policies", `{"id":"p2","source":"package policy"}`, map[string]string{"X-Admin-Token": "admin-tok"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if _, ok := s.Policies.Get("p2"); !ok {
		t.Fatal("policy was not stored")
	}
	if evaluator.validated != "package policy" {
		t.Fatalf("Validate was called with %q, want %q", evaluator.validated, "package policy")
	}
}

func TestHandlePutPolicyRequiresIDAndSource(t *testing.T) {
	s := newTestServer(t, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/admin/policies", `{"id":"","source":""}`, map[string]string{"X-Admin-Token": "admin-tok"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
