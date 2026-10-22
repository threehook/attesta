// Package presentation is the verifier side of OpenID4VP for SD-JWT VC credentials. The backend hands an application an authorization request (an
// `openid4vp://` link) to pass to the holder's wallet; the wallet posts its answer back, and Respond checks it with sdjwt-go.
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

	"github.com/threehook/sdjwt-go"
)

// queryID names the first credential query of a request, the one that identifies the person; further credentials are queryID_2, queryID_3, ...
const queryID = "credential"

// queryIDOf names the n-th credential query (from 0) of a request.
func queryIDOf(n int) string {
	if n == 0 {
		return queryID
	}
	return fmt.Sprintf("%s_%d", queryID, n+1)
}

// defaultIdentityClaim is the credential claim that identifies the holder when a request names none.
const defaultIdentityClaim = "email"

// Request is what an application asks the holder for.
type Request struct {
	Resource string
	PolicyID string
	// Credentials are what the holder must present, each of a type of its own. The first identifies the holder; Respond requires every other one to be
	// of the same holder (the same identity claim).
	Credentials []CredentialQuery
	// IdentityClaim is the claim that identifies the holder in every credential. Every request asks for it and Respond requires it. Empty means email.
	IdentityClaim string
	// Action describes what the application is about to do, as texts by name. The application asserts it; Attesta cannot verify it.
	Action map[string]string
	// UserRoles are the roles the calling application asserted. They are not part of the presentation and Attesta cannot verify them.
	UserRoles []string
	// TraceParent is the W3C traceparent the application sent with the request; the decision log keeps its trace.
	TraceParent string
}

func (r Request) identity() string {
	if r.IdentityClaim == "" {
		return defaultIdentityClaim
	}
	return r.IdentityClaim
}

// CredentialQuery is one credential a request asks for.
type CredentialQuery struct {
	CredentialType string
	// Claims are the top-level claim names the holder is asked to disclose, besides the identity claim. The type and issuer are always
	// established. Respond requires every one of them to be disclosed.
	Claims []string
}

// asked lists every claim name a credential query asks for: its Claims and the identity claim, each once.
func (q CredentialQuery) asked(identity string) []string {
	asked := []string{}
	for _, name := range append(append([]string{}, q.Claims...), identity) {
		if !slices.Contains(asked, name) {
			asked = append(asked, name)
		}
	}
	return asked
}

// Subject identifies the holder who presented a credential: the value of the identity claim, which the issuer vouches for.
type Subject struct {
	Issuer string
	// ID is the value of the identity claim.
	ID string
	// Claim names the identity claim; it is email unless the request named another.
	Claim string
}

// Presented is a presentation that verified.
type Presented struct {
	Request Request
	// Subject is only as trustworthy as the issuer, so it is meaningful once the policy has accepted the issuer.
	Subject Subject
	// Credentials are the presented credentials in the order the request asked for them; the first is the one that identifies the person.
	Credentials []PresentedCredential
}

// PresentedCredential is one credential of a presentation that verified.
type PresentedCredential struct {
	// Issuer identifies who signed the credential; the policy decides whether to trust it.
	Issuer string
	// Type is the credential's own type, which matches the requested one.
	Type string
	// Claims are the disclosed claims that were asked for, the identity claim included; anything else the wallet disclosed is dropped.
	Claims map[string]any
}

// StatusChecker finds out whether a credential was revoked. It gives nil only for a credential that is in good standing, so a credential without a
// status reference or one whose status cannot be established must give an error.
type StatusChecker interface {
	Check(ctx context.Context, issuer string, ref *sdjwt.StatusReference) error
}

type Options struct {
	// PublicURL is where wallets reach this backend; response URIs are built from it.
	PublicURL string
	Keys      sdjwt.KeyResolver
	// Status is asked about every credential after its signature has verified. Without it every presentation is refused.
	Status StatusChecker
	// TTL is how long a request stays answerable, and how old a presentation's key binding may be. Zero means five minutes.
	TTL time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
}

type Verifier struct {
	publicURL string
	keys      sdjwt.KeyResolver
	status    StatusChecker
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
	return &Verifier{publicURL: strings.TrimSuffix(o.PublicURL, "/"), keys: o.Keys, status: o.Status, ttl: ttl, now: now, sessions: newSessions(ttl, now)}
}

// NewRequest registers a request and returns its id and the `openid4vp://` link to give the wallet.
func (v *Verifier) NewRequest(r Request) (id, authorizationRequest string, err error) {
	if len(r.Credentials) == 0 {
		return "", "", errors.New("a request asks for at least one credential")
	}
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

	queries := make([]any, 0, len(r.Credentials))
	for i, q := range r.Credentials {
		claims := make([]map[string]any, 0)
		for _, name := range q.asked(r.identity()) {
			claims = append(claims, map[string]any{"path": []string{name}})
		}
		queries = append(queries, map[string]any{
			"id": queryIDOf(i), "format": "dc+sd-jwt", "meta": map[string]any{"vct_values": []string{q.CredentialType}}, "claims": claims,
		})
	}
	dcql, err := json.Marshal(map[string]any{"credentials": queries})
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
func (v *Verifier) Respond(ctx context.Context, id string, form url.Values) (*Presented, error) {
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
	queries := sess.request.Credentials
	if len(vpToken) != len(queries) {
		return nil, invalid("expected one presentation for each of %d credential queries", len(queries))
	}

	identity := sess.request.identity()
	var presented []PresentedCredential
	var ids []string
	for i, q := range queries {
		id := queryIDOf(i)
		if len(vpToken[id]) != 1 {
			return nil, invalid("expected exactly one presentation for %q", id)
		}
		verified, err := sdjwt.Verify(vpToken[id][0], sdjwt.Options{
			Keys:             v.keys,
			Audience:         clientID(sess.responseURI),
			Nonce:            sess.nonce,
			Now:              v.now,
			KeyBindingMaxAge: v.ttl,
		})
		if err != nil {
			return nil, invalid("%v", err)
		}
		if verified.Type != q.CredentialType {
			return nil, invalid("credential type %q is not the requested %q", verified.Type, q.CredentialType)
		}
		if v.status == nil {
			return nil, invalid("no way to check whether the %s credential was revoked", q.CredentialType)
		}
		if err := v.status.Check(ctx, verified.Issuer, verified.Status); err != nil {
			return nil, &Rejected{Request: sess.request, Err: fmt.Errorf("%w: the %s credential: %w", ErrInvalidPresentation, q.CredentialType, err)}
		}
		claims := map[string]any{}
		for _, name := range q.asked(identity) {
			value, ok := verified.Claims[name]
			if !ok {
				return nil, invalid("the %q claim of the %s credential was not disclosed", name, q.CredentialType)
			}
			claims[name] = value
		}
		holder, _ := claims[identity].(string)
		if holder == "" {
			return nil, invalid("the %q claim that identifies the holder is not a text", identity)
		}
		ids = append(ids, holder)
		presented = append(presented, PresentedCredential{Issuer: verified.Issuer, Type: verified.Type, Claims: claims})
	}
	for _, other := range ids[1:] {
		// An email address has no case; any other identifier, such as a SPIFFE ID, does.
		if other != ids[0] && (identity != defaultIdentityClaim || !strings.EqualFold(other, ids[0])) {
			return nil, invalid("the credentials are not of the same holder")
		}
	}
	return &Presented{Request: sess.request, Subject: Subject{Issuer: presented[0].Issuer, ID: ids[0], Claim: identity}, Credentials: presented}, nil
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
