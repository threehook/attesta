package httpapi

import (
	"encoding/json"
	"net/http"
)

type loginRequest struct {
	Subject string   `json:"subject"`
	Roles   []string `json:"roles"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// handleLogin is the mock login examples/react-gui uses to obtain a JWT: it performs no credential check at all and issues a token for whatever
// subject/roles the caller sends. The token identifies the session for audit/logging in handleAuthorize; it plays no part in the allow/deny decision
// itself.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}

	token, err := s.Auth.MockLogin(req.Subject, req.Roles)
	if err != nil {
		s.Logger.Error("mock login failed", "subject", req.Subject, "error", err)
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}
