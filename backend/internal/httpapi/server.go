// Package httpapi wires the HTTP surface: the authorize flow (request a presentation, receive the wallet's answer, evaluate the policy,
// report the decision), and the admin hot-deploy endpoint — on top of internal/presentation, internal/authz, internal/scripts.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"attesta/backend/internal/authz"
	"attesta/backend/internal/scripts"
)

// Server holds the HTTP handlers' dependencies; call Routes to get an http.Handler.
type Server struct {
	Authz      authz.Evaluator
	Policies   *scripts.Store
	AdminToken string
	Logger     *slog.Logger
	// Presenter serves the OpenID4VP flow (POST /v1/authorize/requests); nil disables it.
	Presenter Presenter
	// CORSOrigins is the comma-separated list of origins allowed to call this API from a browser; see withCORS.
	CORSOrigins string
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /v1/authorize/requests", s.handlePresentationRequest)
	mux.HandleFunc("POST /v1/authorize/requests/{id}/response", s.handlePresentationResponse)
	mux.HandleFunc("GET /v1/authorize/requests/{id}", s.handlePresentationOutcome)
	mux.HandleFunc("POST /admin/policies", s.requireAdmin(s.handlePutPolicy))
	return withCORS(mux, s.CORSOrigins)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// withCORS allows only the origins in the comma-separated allowlist (echoing back the request's Origin when it matches, since
// Access-Control-Allow-Origin can't itself carry a list) — e.g. a page's dev server. A request from any other origin gets no
// Access-Control-Allow-Origin header, so the browser blocks it.
func withCORS(next http.Handler, allowlist string) http.Handler {
	allowed := make(map[string]bool)
	for _, origin := range strings.Split(allowlist, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
