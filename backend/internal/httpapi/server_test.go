package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zk-puoi/backend/internal/auth"
	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/scripts"
)

// fakeVerifier and fakeEvaluator isolate httpapi's own routing/decoding/status-code logic from the real
// gnark-crypto and gnovm implementations, which have their own dedicated tests (internal/proof, internal/authz).
type fakeVerifier struct {
	valid bool
	err   error
}

func (f fakeVerifier) Verify([]byte, []string) (bool, error) { return f.valid, f.err }

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

func encodeFieldString(s string) string {
	return new(big.Int).SetBytes([]byte(s)).String()
}

// newTestServer wires a Server with fakes standing in for proof/authz, plus a real scripts.Store (lightweight,
// no gnovm involved since the fake Evaluator also serves as its Validator) pre-loaded with one policy.
func newTestServer(t *testing.T, verifier fakeVerifier, evaluator *fakeEvaluator) *Server {
	t.Helper()
	policies := scripts.NewStore(evaluator)
	if err := policies.Put("p1", "package policy"); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	return &Server{
		Proof:        verifier,
		Authz:        evaluator,
		Policies:     policies,
		Auth:         auth.NewIssuer("test-secret"),
		AdminToken:   "admin-tok",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		RegistryRoot: "123",
		CORSOrigins:  "http://allowed.example",
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
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})
	rec := doRequest(s, "GET", "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestWithCORS(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "OPTIONS", "/v1/authorize", "", map[string]string{"Origin": "http://allowed.example"})
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
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "GET", "/healthz", "", map[string]string{"Origin": "http://evil.example"})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin for disallowed origin = %q, want empty", got)
	}
}

func TestHandleLogin(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/login", `{"subject":"alice","roles":["student"]}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	claims, err := s.Auth.Verify(resp.Token)
	if err != nil {
		t.Fatalf("Verify issued token: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "alice")
	}
}

func TestHandleLoginRequiresSubject(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/login", `{}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func authorizeBody(resource, policyID string, publicSignals []string) string {
	b, _ := json.Marshal(authorizeRequest{
		Resource:      resource,
		PolicyID:      policyID,
		Proof:         json.RawMessage(`{}`),
		PublicSignals: publicSignals,
	})
	return string(b)
}

func TestHandleAuthorizeRequiresResourceAndPolicyID(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("", "p1", nil), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing resource: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "", nil), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing policyId: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleAuthorizeInvalidProofIsBadRequest(t *testing.T) {
	s := newTestServer(t, fakeVerifier{err: errBoom}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "p1", nil), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
	}
}

func TestHandleAuthorizeProofDidNotVerify(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: false}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "p1", nil), nil)
	assertAuthorizeResponse(t, rec, false, "proof did not verify")
}

func TestHandleAuthorizeRootMismatch(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})
	signals := []string{"999", encodeFieldString("Diploma"), encodeFieldString("trusted-university")}

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "p1", signals), nil)
	assertAuthorizeResponse(t, rec, false, "proof is not for the expected credential registry")
}

func TestHandleAuthorizeMalformedPublicSignals(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "p1", []string{"only-one"}), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
	}
}

func TestHandleAuthorizeUnknownPolicy(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})
	signals := []string{"123", encodeFieldString("Diploma"), encodeFieldString("trusted-university")}

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "no-such-policy", signals), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleAuthorizeSuccess(t *testing.T) {
	evaluator := &fakeEvaluator{result: authz.Result{Allow: true, Reason: "ok"}}
	s := newTestServer(t, fakeVerifier{valid: true}, evaluator)
	signals := []string{"123", encodeFieldString("Diploma"), encodeFieldString("trusted-university")}

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("diploma-vault", "p1", signals), nil)
	assertAuthorizeResponse(t, rec, true, "ok")
}

func TestHandleAuthorizePolicyEvaluationError(t *testing.T) {
	evaluator := &fakeEvaluator{err: errBoom}
	s := newTestServer(t, fakeVerifier{valid: true}, evaluator)
	signals := []string{"123", encodeFieldString("Diploma"), encodeFieldString("trusted-university")}

	rec := doRequest(s, "POST", "/v1/authorize", authorizeBody("r", "p1", signals), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func assertAuthorizeResponse(t *testing.T, rec *httptest.ResponseRecorder, wantAllow bool, wantReason string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body)
	}
	var resp authorizeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Allow != wantAllow || resp.Reason != wantReason {
		t.Fatalf("response = %+v, want allow=%v reason=%q", resp, wantAllow, wantReason)
	}
}

func TestHandlePutPolicyRequiresAdminToken(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

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
	s := newTestServer(t, fakeVerifier{valid: true}, evaluator)

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
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})

	rec := doRequest(s, "POST", "/admin/policies", `{"id":"","source":""}`, map[string]string{"X-Admin-Token": "admin-tok"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuditSubject(t *testing.T) {
	s := newTestServer(t, fakeVerifier{valid: true}, &fakeEvaluator{})
	token, err := s.Auth.MockLogin("alice", nil)
	if err != nil {
		t.Fatalf("MockLogin: %v", err)
	}

	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"no header", "", "anonymous"},
		{"invalid token", "Bearer not-a-token", "anonymous"},
		{"valid token", "Bearer " + token, "alice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/authorize", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			if got := s.auditSubject(req); got != c.want {
				t.Errorf("auditSubject() = %q, want %q", got, c.want)
			}
		})
	}
}

var errBoom = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
