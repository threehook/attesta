package httpapi

import (
	"encoding/json"
	"net/http"
)

type devProveRequest struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

type devProveResponse struct {
	Proof         string   `json:"proof"`
	PublicSignals []string `json:"publicSignals"`
}

// handleDevProve generates a toy-circuit proof server-side so /v1/authorize can be exercised with curl without a real ZK client. It is a development
// convenience only — no production client would ever call this, and a real deployment should not expose it.
func (s *Server) handleDevProve(w http.ResponseWriter, r *http.Request) {
	var req devProveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	proofHex, signals, err := s.DevProver.Prove(req.X, req.Y)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, devProveResponse{Proof: proofHex, PublicSignals: signals})
}
