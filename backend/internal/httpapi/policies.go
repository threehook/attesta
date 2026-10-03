package httpapi

import (
	"encoding/json"
	"net/http"
)

type putPolicyRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// handlePutPolicy installs or replaces a policy without restarting the server — the manual deploy path described in the README, gated by
// requireAdmin. The source is validated (it must compile as Gno) before it's accepted; an invalid policy never reaches the store.
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
