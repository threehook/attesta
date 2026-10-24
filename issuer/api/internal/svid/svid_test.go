package svid

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

const (
	trustDomain = "corp.test"
	audience    = "https://issuer.test"
	workload    = "spiffe://corp.test/ns/cicd/sa/agent"
)

type authority struct {
	key *ecdsa.PrivateKey
	kid string
}

func newAuthority(t *testing.T) authority {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return authority{key: key, kid: "key-1"}
}

func (a authority) bundle(t *testing.T) *jwtbundle.Bundle {
	t.Helper()

	td, _ := spiffeid.TrustDomainFromString(trustDomain)

	return jwtbundle.FromJWTAuthorities(td, map[string]crypto.PublicKey{a.kid: a.key.Public()})
}

func (a authority) token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = a.kid

	signed, err := tok.SignedString(a.key)
	if err != nil {
		t.Fatal(err)
	}

	return signed
}

func claimsFor(sub string, aud any, exp time.Time) jwt.MapClaims {
	return jwt.MapClaims{"sub": sub, "aud": aud, "exp": exp.Unix(), "iat": time.Now().Unix()}
}

func TestVerify(t *testing.T) {
	a := newAuthority(t)
	other := newAuthority(t)

	v, err := New(a.bundle(t), trustDomain, audience)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		token string
		want  string
		fails bool
	}{
		"a valid token": {token: a.token(t, claimsFor(workload, []string{audience}, time.Now().Add(time.Minute))), want: workload},
		"a token for another party": {
			token: a.token(t, claimsFor(workload, []string{"https://other.test"}, time.Now().Add(time.Minute))),
			fails: true,
		},
		"an expired token": {token: a.token(t, claimsFor(workload, []string{audience}, time.Now().Add(-time.Minute))), fails: true},
		"a key that is not in the bundle": {
			token: other.token(t, claimsFor(workload, []string{audience}, time.Now().Add(time.Minute))),
			fails: true,
		},
		"another trust domain": {
			token: a.token(t, claimsFor("spiffe://elsewhere.test/ns/x/sa/y", []string{audience}, time.Now().Add(time.Minute))),
			fails: true,
		},
		"no token":    {token: "", fails: true},
		"not a token": {token: "abc.def", fails: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := v.Verify(context.Background(), tc.token)
			if tc.fails {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("error = %v, want ErrInvalid", err)
				}

				return
			}

			if err != nil || got != tc.want {
				t.Fatalf("Verify = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestNewRefusesWhatCannotWork(t *testing.T) {
	a := newAuthority(t)
	if _, err := New(a.bundle(t), "not a domain!", audience); err == nil {
		t.Error("a bad trust domain was accepted")
	}

	if _, err := New(a.bundle(t), trustDomain, ""); err == nil {
		t.Error("an empty audience was accepted")
	}
}
