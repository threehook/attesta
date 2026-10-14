// Package oid4vci is the issuing side of OpenID4VCI (final): the pre-authorized code flow without a transaction code, for SD-JWT VC credentials
// bound to the holder's jwk. A wallet gets from a QR code, through an offer, a one-time code and an access token, to its credential.
package oid4vci

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sdjwt "github.com/threehook/sdjwt-go"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/entitlement"
	"attesta/issuer/internal/identity"
)

const (
	preAuthorizedGrant = "urn:ietf:params:oauth:grant-type:pre-authorized_code"
	proofType          = "openid4vci-proof+jwt"
	tokenLifetime      = 5 * time.Minute
	proofLeeway        = 5 * time.Minute
	// Path is where the issuer's endpoints live below its public URL.
	Path = "/oid4vci"
)

// Entitlements is what the credential is made from.
type Entitlements interface {
	Find(ctx context.Context, id string) (entitlement.Entitlement, error)
	FindOpenByEmail(ctx context.Context, email string) ([]entitlement.Entitlement, error)
	Claim(ctx context.Context, id string) (bool, error)
	MarkIssued(ctx context.Context, id string) (bool, error)
}

// Renew sends the person of an entitlement a new QR code.
type Renew func(ctx context.Context, e entitlement.Entitlement) error

type Config struct {
	// PublicURL is where wallets reach the issuer, without Path.
	PublicURL  string
	IssuerName string
	Identity   identity.Identity
	// Secret is what the one-time codes and nonces are derived from.
	Secret []byte
	// OfferLifetime is how long a QR code works.
	OfferLifetime time.Duration
	// RenewalCooldown is the least time between two new QR codes that an expired one asks for.
	RenewalCooldown time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
}

type Server struct {
	cfg          Config
	base         string
	nonceKey     []byte
	sessions     *Sessions
	entitlements Entitlements
	renew        Renew
}

func New(cfg Config, sessions *Sessions, entitlements Entitlements, renew Renew) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Server{
		cfg: cfg, base: strings.TrimSuffix(cfg.PublicURL, "/") + Path, nonceKey: derive(cfg.Secret, "nonce"),
		sessions: sessions, entitlements: entitlements, renew: renew,
	}
}

// Handler serves everything below Path, and the well-known documents for it.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, wellKnown := range []struct{ name, path string }{{"openid-credential-issuer", "issuer"}, {"oauth-authorization-server", "authorization"}} {
		handle := s.issuerMetadata
		if wellKnown.path == "authorization" {
			handle = s.authorizationMetadata
		}
		mux.HandleFunc("GET /.well-known/"+wellKnown.name+Path, handle)
		mux.HandleFunc("GET "+Path+"/.well-known/"+wellKnown.name, handle)
	}
	mux.HandleFunc("GET "+Path+"/offers/{id}", s.offer)
	mux.HandleFunc("POST "+Path+"/token", s.token)
	mux.HandleFunc("POST "+Path+"/nonce", s.nonce)
	mux.HandleFunc("POST "+Path+"/credential", s.credential)
	return mux
}

// Offer makes a QR code's session and gives the link the code holds (the standard calls it a credential offer).
func (s *Server) Offer(ctx context.Context, e entitlement.Entitlement) (string, error) {
	session, err := s.sessions.Create(ctx, NewSession{EntitlementID: e.ID, ConfigurationID: e.Type, Lifetime: s.cfg.OfferLifetime}, s.cfg.Now())
	if err != nil {
		return "", err
	}
	return "openid-credential-offer://?credential_offer_uri=" + url.QueryEscape(s.base+"/offers/"+session.OfferID), nil
}

// Reconcile marks the entitlements entitled by a completed session as issued, for a credential made just before the issuer stopped.
func (s *Server) Reconcile(ctx context.Context) error {
	ids, err := s.sessions.CompletedEntitlements(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.entitlements.MarkIssued(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (s *Server) issuerMetadata(w http.ResponseWriter, _ *http.Request) {
	configurations := map[string]any{}
	types := []string{credential.Employee}
	for _, d := range credential.Diplomas {
		types = append(types, d.Type)
	}
	for _, t := range types {
		configurations[t] = map[string]any{
			"format": "dc+sd-jwt", "scope": t, "vct": t,
			"cryptographic_binding_methods_supported": []string{"jwk"},
			"credential_signing_alg_values_supported": []string{"EdDSA"},
			"proof_types_supported":                   map[string]any{"jwt": map[string]any{"proof_signing_alg_values_supported": []string{"EdDSA", "ES256"}}},
		}
	}
	metadata := map[string]any{
		"credential_issuer": s.base, "credential_endpoint": s.base + "/credential", "nonce_endpoint": s.base + "/nonce",
		"credential_configurations_supported": configurations,
	}
	if s.cfg.IssuerName != "" {
		metadata["display"] = []map[string]string{{"name": s.cfg.IssuerName}}
	}
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) authorizationMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": s.base, "token_endpoint": s.base + "/token",
		"grant_types_supported":                           []string{preAuthorizedGrant},
		"pre-authorized_grant_anonymous_access_supported": true,
	})
}

func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	session, err := s.sessions.FindByOffer(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "unknown QR code")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	now := s.cfg.Now()
	if session.State == stateOffer && now.Before(session.ExpiresAt) {
		writeJSON(w, http.StatusOK, map[string]any{
			"credential_issuer": s.base, "credential_configuration_ids": []string{session.ConfigurationID},
			"grants": map[string]any{preAuthorizedGrant: map[string]string{"pre-authorized_code": s.sessions.code(session.OfferID)}},
		})
		return
	}
	if session.State != stateOffer || !s.stillOpen(r.Context(), session.EntitlementID) {
		writeError(w, http.StatusGone, "qr_gone", "Dit credential is al opgehaald of ingetrokken.")
		return
	}
	// The QR code ran out while its entitlement is still there: the person gets a new one, unless one went out lately.
	if last, ok, err := s.sessions.LastCreated(r.Context(), session.EntitlementID); err != nil {
		serverError(w, err)
		return
	} else if ok && now.Sub(last) < s.cfg.RenewalCooldown {
		writeError(w, http.StatusGone, "qr_expired", "De QR-code is verlopen. Er is al een nieuwe QR-code naar uw e-mail gestuurd.")
		return
	}
	e, err := s.entitlements.Find(r.Context(), session.EntitlementID)
	if err == nil {
		err = s.renew(r.Context(), e)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeError(w, http.StatusGone, "qr_expired", "De QR-code is verlopen. Er is een nieuwe QR-code naar uw e-mail gestuurd.")
}

// stillOpen reports whether the entitlement can still be claimed.
func (s *Server) stillOpen(ctx context.Context, id string) bool {
	e, err := s.entitlements.Find(ctx, id)
	if err != nil {
		return false
	}
	open, err := s.entitlements.FindOpenByEmail(ctx, e.Email)
	if err != nil {
		return false
	}
	for _, o := range open {
		if o.ID == e.ID {
			return true
		}
	}
	return false
}

func serverError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "server_error", "something went wrong at the issuer")
	logError(err)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the request is not a form")
		return
	}
	if r.PostForm.Get("grant_type") != preAuthorizedGrant {
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "only the pre-authorized code is supported")
		return
	}
	_, token, err := s.sessions.ExchangeCode(r.Context(), r.PostForm.Get("pre-authorized_code"), s.cfg.Now(), tokenLifetime)
	if errors.Is(err, ErrInvalidGrant) {
		writeError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, used or expired")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(tokenLifetime.Seconds())})
}

func (s *Server) nonce(w http.ResponseWriter, _ *http.Request) {
	nonce, err := mintNonce(s.nonceKey, s.cfg.Now())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"c_nonce": nonce, "c_nonce_expires_in": int(nonceLifetime.Seconds())})
}

type credentialRequest struct {
	ConfigurationID string `json:"credential_configuration_id"`
	Proofs          struct {
		JWT []string `json:"jwt"`
	} `json:"proofs"`
	Proof *struct {
		Type string `json:"proof_type"`
		JWT  string `json:"jwt"`
	} `json:"proof"`
}

func (s *Server) credential(w http.ResponseWriter, r *http.Request) {
	now := s.cfg.Now()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "invalid_token", "an access token is required")
		return
	}
	session, err := s.sessions.FindByAccessToken(r.Context(), token, now)
	if errors.Is(err, ErrNotFound) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeError(w, http.StatusUnauthorized, "invalid_token", "the access token is unknown, used or expired")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	var req credentialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_credential_request", "the request is not JSON")
		return
	}
	if req.ConfigurationID != session.ConfigurationID {
		writeError(w, http.StatusBadRequest, "invalid_credential_request", "credential_configuration_id is not the one this QR code is for")
		return
	}
	proofs := req.Proofs.JWT
	if req.Proof != nil && req.Proof.Type == "jwt" {
		proofs = append(proofs, req.Proof.JWT)
	}
	if len(proofs) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_proof", "exactly one jwt proof is required")
		return
	}
	holder, err := s.verifyProof(proofs[0], now)
	if errors.Is(err, errInvalidNonce) {
		writeError(w, http.StatusBadRequest, "invalid_nonce", "the nonce is not one of ours or has expired; ask for a new one")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_proof", err.Error())
		return
	}

	e, err := s.claim(r.Context(), session.EntitlementID, now)
	if errors.Is(err, errNoEntitlement) {
		writeError(w, http.StatusBadRequest, "credential_request_denied", "there is no open entitlement for you")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	content, err := credential.Content(e.Type, e.Claims, s.cfg.IssuerName)
	if err != nil {
		serverError(w, err)
		return
	}
	content.HolderKey = holder
	issued, err := sdjwt.Issue(s.cfg.Identity.Key, content)
	if err != nil {
		serverError(w, err)
		return
	}
	if done, err := s.sessions.Complete(r.Context(), session.ID, now); err != nil || !done {
		if err == nil {
			err = errors.New("the session was completed by another request")
		}
		serverError(w, err)
		return
	}
	if _, err := s.entitlements.MarkIssued(r.Context(), e.ID); err != nil {
		logError(err) // the credential is made; the next start marks the entitlement from the completed session
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": []map[string]string{{"credential": issued}}})
}

var errNoEntitlement = errors.New("no open entitlement")

// claim takes the entitlement, so that no one else can while the credential is made.
func (s *Server) claim(ctx context.Context, id string, now time.Time) (entitlement.Entitlement, error) {
	e, err := s.entitlements.Find(ctx, id)
	if errors.Is(err, entitlement.ErrNotFound) || (err == nil && e.ValidUntil != nil && !e.ValidUntil.After(now)) {
		return entitlement.Entitlement{}, errNoEntitlement
	}
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	claimed, err := s.entitlements.Claim(ctx, e.ID)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	if !claimed {
		return entitlement.Entitlement{}, errNoEntitlement
	}
	return e, nil
}

var (
	errInvalidNonce = errors.New("invalid nonce")
	proofAlgs       = []string{"EdDSA", "ES256"}
)

// verifyProof checks the holder's proof of possession and gives back the key it was signed with.
func (s *Server) verifyProof(token string, now time.Time) (map[string]any, error) {
	claims := jwt.MapClaims{}
	var holder map[string]any
	_, err := jwt.NewParser(
		jwt.WithValidMethods(proofAlgs), jwt.WithTimeFunc(func() time.Time { return now }), jwt.WithLeeway(proofLeeway),
		jwt.WithAudience(s.base),
	).ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Header["typ"] != proofType {
			return nil, errors.New("the proof must have typ " + proofType)
		}
		jwk, ok := t.Header["jwk"].(map[string]any)
		if !ok {
			return nil, errors.New("the proof must carry its key as jwk")
		}
		key, normalized, err := publicKeyFromJWK(jwk)
		holder = normalized
		return key, err
	})
	if err != nil {
		return nil, err
	}
	iat, _ := claims.GetIssuedAt()
	if iat == nil {
		return nil, errors.New("the proof has no iat")
	}
	if d := now.Sub(iat.Time); d > proofLeeway || d < -proofLeeway {
		return nil, errors.New("the proof was not made just now")
	}
	if nonce, _ := claims["nonce"].(string); !checkNonce(s.nonceKey, nonce, now) {
		return nil, errInvalidNonce
	}
	return holder, nil
}
