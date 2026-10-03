// Package httpapi wires the HTTP surface: mock login, the authorize endpoint (verify proof, then evaluate policy), and the admin hot-deploy endpoint
// — on top of internal/proof, internal/authz, internal/scripts and internal/auth.
package httpapi

import (
	"log/slog"
	"net/http"

	"zk-puoi/backend/internal/auth"
	"zk-puoi/backend/internal/authz"
	"zk-puoi/backend/internal/proof"
	"zk-puoi/backend/internal/scripts"
)

type Server struct {
	Proof      proof.Verifier
	Authz      authz.Evaluator
	Policies   *scripts.Store
	Auth       *auth.Issuer
	AdminToken string
	Logger     *slog.Logger
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /v1/login", s.handleLogin)
	mux.HandleFunc("POST /v1/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /admin/policies", s.requireAdmin(s.handlePutPolicy))
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Token") != s.AdminToken {
			writeError(w, http.StatusUnauthorized, "invalid admin token")
			return
		}
		next(w, r)
	}
}
