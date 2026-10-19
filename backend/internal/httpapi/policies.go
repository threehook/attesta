package httpapi

import (
	"attesta/backend/internal/authz"
	"encoding/json"
	"net/http"
)

type putPolicyRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// handlePutPolicy installs or replaces a policy without restarting the server — the manual deploy path described in the README, gated by
// requireAdmin. The source is validated (it must compile as Rego) before it's accepted; an invalid policy never reaches the store.
func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	var req putPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" || req.Source == "" {
		writeError(w, http.StatusBadRequest, "id and source are required")
		return
	}

	if err := s.Policies.Put(req.ID, req.Source); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.Logger.Info("policy deployed", "id", req.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deployed", "id": req.ID})
}

// maxPolicySource bounds a policy sent for diagnosis; real ones are a few kilobytes.
const maxPolicySource = 256 << 10

// handleDiagnosePolicy reports the problems in a policy source, with their places, and stores nothing.
func (s *Server) handleDiagnosePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPolicySource)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	diagnostics := s.Authz.Diagnose(req.Source)
	if diagnostics == nil {
		diagnostics = []authz.Diagnostic{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics})
}
