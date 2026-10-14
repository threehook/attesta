// Package auth lets only signed-in staff use the issuer's forms: it checks the access token an OpenID Connect server issued.
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Options struct {
	// Issuer is the server's name as it writes it in its tokens (the iss claim).
	Issuer string
	// DiscoveryURL is where the server's metadata is fetched; empty means Issuer. It differs when the issuer is only reachable from outside.
	DiscoveryURL string
	// ClientID is the application the staff signed in with; the token must have been issued to it (azp).
	ClientID string
	// Role is the role a token must carry: realm_access.roles (Keycloak) or roles.
	Role string
}

type Verifier struct {
	opts Options

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

func New(opts Options) *Verifier { return &Verifier{opts: opts} }

// verifierFor finds the server's keys on first use and retries when it was not reachable, so the issuer starts without the server.
func (v *Verifier) verifierFor(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	url := v.opts.DiscoveryURL
	if url == "" {
		url = v.opts.Issuer
	}
	// The metadata names the issuer as tokens do, which may differ from the address it was fetched from.
	provider, err := oidc.NewProvider(oidc.InsecureIssuerURLContext(ctx, v.opts.Issuer), url)
	if err != nil {
		return nil, err
	}
	v.verifier = provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	return v.verifier, nil
}

// Protect answers 401 unless the request carries a valid access token of someone with the staff role.
func (v *Verifier) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			deny(w, http.StatusUnauthorized, "inloggen is nodig")
			return
		}
		verifier, err := v.verifierFor(r.Context())
		if err != nil {
			slog.Error("cannot reach the login server", "error", err)
			deny(w, http.StatusServiceUnavailable, "de loginserver is niet bereikbaar")
			return
		}
		if err := v.check(r.Context(), verifier, token); err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, errForbidden) {
				status = http.StatusForbidden
			}
			deny(w, status, err.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errForbidden = errors.New("geen toegang voor deze gebruiker")

func (v *Verifier) check(ctx context.Context, verifier *oidc.IDTokenVerifier, raw string) error {
	t, err := verifier.Verify(ctx, raw)
	if err != nil {
		return errors.New("de aanmelding is ongeldig of verlopen")
	}
	var claims struct {
		Azp         string   `json:"azp"`
		Roles       []string `json:"roles"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := t.Claims(&claims); err != nil {
		return errors.New("de aanmelding is ongeldig of verlopen")
	}
	if claims.Azp != v.opts.ClientID {
		return errForbidden
	}
	if !slices.Contains(claims.RealmAccess.Roles, v.opts.Role) && !slices.Contains(claims.Roles, v.opts.Role) {
		return errForbidden
	}
	return nil
}

func deny(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + message + `"}` + "\n"))
}
