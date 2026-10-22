// Package svid checks the JWT-SVID a workload shows to prove which SPIFFE identity it has.
package svid

import (
	"context"
	"errors"
	"fmt"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
)

// Verifier accepts a JWT-SVID that is signed with a key of the trust domain's bundle, is meant for this audience and has not expired.
type Verifier struct {
	bundles     jwtbundle.Source
	trustDomain spiffeid.TrustDomain
	audience    string
}

// New makes a Verifier. The bundles come from the SPIFFE Workload API in production.
func New(bundles jwtbundle.Source, trustDomain, audience string) (*Verifier, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("trust domain: %w", err)
	}
	if audience == "" {
		return nil, errors.New("an audience is required")
	}
	return &Verifier{bundles: bundles, trustDomain: td, audience: audience}, nil
}

// ErrInvalid is every reason a token is not accepted; the reason is in the message.
var ErrInvalid = errors.New("invalid JWT-SVID")

// Verify gives the SPIFFE ID the token proves.
func (v *Verifier) Verify(_ context.Context, token string) (string, error) {
	svid, err := jwtsvid.ParseAndValidate(token, v.bundles, []string{v.audience})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !svid.ID.MemberOf(v.trustDomain) {
		return "", fmt.Errorf("%w: %s is not in the trust domain %s", ErrInvalid, svid.ID, v.trustDomain)
	}
	return svid.ID.String(), nil
}
