// Package presentation is the verifier side of OpenID4VP for SD-JWT VC credentials. The backend hands an application an authorization request (an
// `openid4vp://` link) to pass to the holder's wallet; the wallet posts its answer back, and Respond checks it with internal/sdjwt.
//
// Requests are unsigned and identify the verifier by its response URI (the `redirect_uri` client identifier prefix), so there is no verifier key to
// manage. Whether the issuer is trusted is not decided here: Respond reports who issued the credential and the caller applies the policy.
package presentation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"attesta/backend/internal/sdjwt"
)

// queryID names the one credential query in every request.
const queryID = "credential"

// identityClaim is the credential claim that identifies the person. Every request asks for it and Respond requires it.
const identityClaim = "email"

// Request is what an application asks the holder for.
type Request struct {
	Resource       string
	PolicyID       string
	CredentialType string
	// Claims are the top-level claim names the holder is asked to disclose, besides the email that identifies them. The type and issuer are always
	// established. Respond requires every one of them to be disclosed.
	Claims []string
	// UserRoles are the roles the calling application asserted. They are not part of the presentation and attesta cannot verify them.
	UserRoles []string
	// TraceParent is the W3C traceparent the application sent with the request; the decision log keeps its trace.
	TraceParent string
}

// asked lists every claim name a request asks for: its Claims and the identifying email, each once.
func (r Request) asked() []string {
	asked := []string{}
	for _, name := range append(append([]string{}, r.Claims...), identityClaim) {
		if !slices.Contains(asked, name) {
			asked = append(asked, name)
		}
	}
	return asked
}

// Subject identifies the person who presented a credential: the email address the issuer vouches for.
type Subject struct {
	Issuer string
	Email  string
}

// Presented is a presentation that verified.
type Presented struct {
	Request Request
	// Issuer identifies who signed the credential; the policy decides whether to trust it.
	Issuer string
	// Subject is only as trustworthy as the issuer, so it is meaningful once the policy has accepted the issuer.
	Subject Subject
	// Type is the credential's own type, which matches Request.CredentialType.
	Type string
	// Claims are the disclosed claims that were asked for, the email included; anything else the wallet disclosed is dropped.
	Claims map[string]any
}

type Options struct {
	// PublicURL is where wallets reach this backend; response URIs are built from it.
	PublicURL string
	Keys      sdjwt.KeyResolver
	// TTL is how long a request stays answerable, and how old a presentation's key binding may be. Zero means five minutes.
	TTL time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
}

type Verifier struct {
	publicURL string
	keys      sdjwt.KeyResolver
	ttl       time.Duration
	now       func() time.Time
	sessions  *sessions
}

func New(o Options) *Verifier {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	ttl := o.TTL
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	return &Verifier{publicURL: strings.TrimSuffix(o.PublicURL, "/"), keys: o.Keys, ttl: ttl, now: now, sessions: newSessions(ttl, now)}
}

// NewRequest registers a request and returns its id and the `openid4vp://` link to give the wallet.
func (v *Verifier) NewRequest(r Request) (id, authorizationRequest string, err error) {
	id, err = randomID()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomID()
	if err != nil {
		return "", "", err
	}
	state, err := randomID()
	if err != nil {
		return "", "", err
	}
	responseURI := v.responseURI(id)

	credential := map[string]any{
		"id":     queryID,
		"format": "dc+sd-jwt",
		"meta":   map[string]any{"vct_values": []string{r.CredentialType}},
	}
	asked := r.asked()
	claims := make([]map[string]any, len(asked))
	for i, name := range asked {
		claims[i] = map[string]any{"path": []string{name}}
	}
	credential["claims"] = claims
	dcql, err := json.Marshal(map[string]any{"credentials": []any{credential}})
	if err != nil {
		return "", "", err
	}
	clientMetadata, err := json.Marshal(map[string]any{"vp_formats_supported": map[string]any{
		"dc+sd-jwt": map[string]any{"sd-jwt_alg_values": []string{"EdDSA", "ES256"}, "kb-jwt_alg_values": []string{"EdDSA", "ES256"}},
	}})
	if err != nil {
		return "", "", err
	}

	q := url.Values{}
	q.Set("response_type", "vp_token")
	q.Set("client_id", clientID(responseURI))
	q.Set("response_uri", responseURI)
	q.Set("response_mode", "direct_post")
	q.Set("nonce", nonce)
	q.Set("state", state)
	q.Set("dcql_query", string(dcql))
	q.Set("client_metadata", string(clientMetadata))

	v.sessions.put(id, session{request: r, nonce: nonce, state: state, responseURI: responseURI})
	return id, "openid4vp://?" + q.Encode(), nil
}

// ErrInvalidPresentation wraps every reason a wallet's answer is not acceptable.
var ErrInvalidPresentation = errors.New("invalid presentation")

// Rejected is the error Respond returns for an answer that did not verify; it carries the request the answer belonged to.
type Rejected struct {
	Request Request
	Err     error
}

func (r *Rejected) Error() string { return r.Err.Error() }
func (r *Rejected) Unwrap() error { return r.Err }

// Respond verifies the wallet's direct_post answer (`vp_token` and `state` form fields) to request id. The request is consumed whether or not the
// answer verifies.
func (v *Verifier) Respond(_ context.Context, id string, form url.Values) (*Presented, error) {
	sess, err := v.sessions.claim(id)
	if err != nil {
		return nil, err
	}
	invalid := func(format string, args ...any) error {
		return &Rejected{Request: sess.request, Err: fmt.Errorf("%w: %s", ErrInvalidPresentation, fmt.Sprintf(format, args...))}
	}

	if form.Get("state") != sess.state {
		return nil, invalid("state does not match the request")
	}
	var vpToken map[string][]string
	if err := json.Unmarshal([]byte(form.Get("vp_token")), &vpToken); err != nil {
		return nil, invalid("vp_token is not a JSON object of presentations")
	}
	presentations := vpToken[queryID]
	if len(vpToken) != 1 || len(presentations) != 1 {
		return nil, invalid("expected exactly one presentation for %q", queryID)
	}

	verified, err := sdjwt.Verify(presentations[0], sdjwt.Options{
		Keys:             v.keys,
		Audience:         clientID(sess.responseURI),
		Nonce:            sess.nonce,
		Now:              v.now,
		KeyBindingMaxAge: v.ttl,
	})
	if err != nil {
		return nil, invalid("%v", err)
	}
	if verified.Type != sess.request.CredentialType {
		return nil, invalid("credential type %q is not the requested %q", verified.Type, sess.request.CredentialType)
	}
	claims := map[string]any{}
	for _, name := range sess.request.asked() {
		value, ok := verified.Claims[name]
		if !ok {
			return nil, invalid("the %q claim was not disclosed", name)
		}
		claims[name] = value
	}
	email, _ := claims[identityClaim].(string)
	if email == "" {
		return nil, invalid("the %q claim that identifies the holder is not a text", identityClaim)
	}
	return &Presented{
		Request: sess.request, Issuer: verified.Issuer, Subject: Subject{Issuer: verified.Issuer, Email: email}, Type: verified.Type, Claims: claims,
	}, nil
}

// Complete records the decision on request id for Outcome to return.
func (v *Verifier) Complete(id string, o Outcome) { v.sessions.complete(id, o) }

// Outcome returns the decision on request id, or nil while the wallet has not answered yet.
func (v *Verifier) Outcome(id string) (*Outcome, error) { return v.sessions.outcome(id) }

func (v *Verifier) responseURI(id string) string {
	return v.publicURL + "/v1/authorize/requests/" + id + "/response"
}

// clientID is how the request identifies this verifier; the key-binding JWT must name it as audience.
func clientID(responseURI string) string { return "redirect_uri:" + responseURI }

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
