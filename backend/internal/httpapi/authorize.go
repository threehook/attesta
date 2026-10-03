package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"zk-puoi/backend/internal/authz"
)

type authorizeRequest struct {
	Resource string `json:"resource"`
	PolicyID string `json:"policyId"`
	// Proof is a Groth16 proof in snarkjs's native JSON format (its proof.json shape), not re-encoded.
	Proof json.RawMessage `json:"proof"`
	// PublicSignals must cryptographically verify against Proof; it's snarkjs's public.json shape — decimal-string field elements.
	PublicSignals []string `json:"publicSignals"`
}

type authorizeResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason"`
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	var req authorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Resource == "" {
		writeError(w, http.StatusBadRequest, "resource is required")
		return
	}
	if req.PolicyID == "" {
		writeError(w, http.StatusBadRequest, "policyId is required")
		return
	}
	policyID := req.PolicyID

	subject := s.auditSubject(r)
	s.Logger.Info("authorize request", "subject", subject, "resource", req.Resource, "policyId", policyID)

	valid, err := s.Proof.Verify(req.Proof, req.PublicSignals)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid proof: %v", err))
		return
	}
	if !valid {
		s.Logger.Info("authorize result", "subject", subject, "resource", req.Resource, "allow", false, "reason", "proof did not verify")
		writeJSON(w, http.StatusOK, authorizeResponse{Allow: false, Reason: "proof did not verify"})
		return
	}

	input, ok, err := authz.DecodePublicSignals(req.PublicSignals, req.Resource, s.RegistryRoot)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid public signals: %v", err))
		return
	}
	if !ok {
		const reason = "proof is not for the expected credential registry"
		s.Logger.Info("authorize result", "subject", subject, "resource", req.Resource, "allow", false, "reason", reason)
		writeJSON(w, http.StatusOK, authorizeResponse{Allow: false, Reason: reason})
		return
	}

	source, ok := s.Policies.Get(policyID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown policy %q", policyID))
		return
	}

	result, err := s.Authz.Evaluate(source, input)
	if err != nil {
		s.Logger.Error("policy evaluation failed", "subject", subject, "policyId", policyID, "error", err)
		writeError(w, http.StatusInternalServerError, "policy evaluation failed")
		return
	}

	s.Logger.Info("authorize result", "subject", subject, "resource", req.Resource, "allow", result.Allow, "reason", result.Reason)
	writeJSON(w, http.StatusOK, authorizeResponse{Allow: result.Allow, Reason: result.Reason})
}

// auditSubject best-effort extracts the mock-login JWT's subject for logging only. A missing or invalid token never blocks the request — the proof
// and policy are what decide authorization, the JWT is just identity for the audit trail.
func (s *Server) auditSubject(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "anonymous"
	}
	claims, err := s.Auth.Verify(strings.TrimPrefix(h, prefix))
	if err != nil {
		return "anonymous"
	}
	return claims.Subject
}
