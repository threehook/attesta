// Package statuslist publishes the signed list in which verifiers look up whether a credential was revoked.
package statuslist

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	sdjwt "github.com/threehook/sdjwt-go"

	"attesta/issuer/internal/identity"
)

// Path is where the list is published below the issuer's public URL.
const Path = "/status/1"

const (
	// TTL is how long a verifier may keep the list before it asks again; a revocation reaches verifiers within it.
	TTL = time.Minute
	// Lifetime is how long a list token is valid; a verifier refuses a later one.
	Lifetime = time.Hour
	// minSize keeps the list from showing how many credentials there are.
	minSize = 16384
)

// Revocations tells which credentials were revoked.
type Revocations interface {
	StatusIndexes(ctx context.Context) (revoked []int64, size int64, err error)
}

type Server struct {
	identity identity.Identity
	store    Revocations
	uri      string
	now      func() time.Time
}

// New publishes the list at publicURL+Path. A nil now means time.Now.
func New(id identity.Identity, store Revocations, publicURL string, now func() time.Time) *Server {
	if now == nil {
		now = time.Now
	}
	return &Server{identity: id, store: store, uri: publicURL + Path, now: now}
}

// URI is what a credential's status reference names.
func (s *Server) URI() string { return s.uri }

// Handler serves the list at Path.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+Path, s.serve)
	return mux
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	revoked, size, err := s.store.StatusIndexes(r.Context())
	if err != nil {
		slog.Error("could not read the revocations", "error", err)
		http.Error(w, "the status list is not available", http.StatusInternalServerError)
		return
	}
	list := sdjwt.NewStatusList(int(max(size, minSize)))
	for _, i := range revoked {
		if err := list.SetRevoked(int(i), true); err != nil {
			slog.Error("could not mark a revocation", "error", err)
			http.Error(w, "the status list is not available", http.StatusInternalServerError)
			return
		}
	}
	now := s.now()
	token, err := sdjwt.SignStatusList(s.identity.Key, s.uri, list, TTL, now.Add(Lifetime), s.now)
	if err != nil {
		slog.Error("could not sign the status list", "error", err)
		http.Error(w, "the status list is not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/statuslist+jwt")
	w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(int(TTL/time.Second)))
	w.Write([]byte(token))
}
