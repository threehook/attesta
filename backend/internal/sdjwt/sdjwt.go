// Package sdjwt verifies SD-JWT VC presentations (draft-ietf-oauth-selective-disclosure-jwt): the issuer's signature, the disclosed claims, and the
// holder's key-binding JWT. It never decides whether an issuer is trusted; it reports who the issuer is and the caller decides.
package sdjwt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// KeyResolver finds the key that must have signed a credential issued by issuer.
type KeyResolver interface {
	Key(issuer string, header map[string]any) (crypto.PublicKey, error)
}

type Options struct {
	Keys KeyResolver
	// Audience and Nonce must match the key-binding JWT: they tie a presentation to one request from one verifier.
	Audience string
	Nonce    string
	// Now defaults to time.Now.
	Now func() time.Time
	// KeyBindingMaxAge is how old the key-binding JWT's iat may be. Zero means five minutes.
	KeyBindingMaxAge time.Duration
}

// Verified is a presentation that checked out.
type Verified struct {
	Issuer string
	// Type is the credential type (the `vct` claim).
	Type string
	// Claims are the issuer-signed claims with the disclosed ones filled in. Undisclosed claims are absent.
	Claims map[string]any
}

const (
	leeway          = 30 * time.Second
	defaultMaxAge   = 5 * time.Minute
	sdAlgSHA256     = "sha-256"
	arrayDigestKey  = "..."
	typeCredential  = "dc+sd-jwt"
	typeCredentialV = "vc+sd-jwt" // the name before draft 06 renamed it
	typeKeyBinding  = "kb+jwt"
)

var (
	issuerAlgs = []string{"EdDSA", "ES256"}
	holderAlgs = []string{"EdDSA", "ES256"}
)

// Verify checks a presentation of the form `<issuer-signed JWT>~<disclosure>~...~<key-binding JWT>`.
func Verify(presentation string, o Options) (*Verified, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	maxAge := o.KeyBindingMaxAge
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}

	parts := strings.Split(presentation, "~")
	if len(parts) < 2 {
		return nil, errors.New("not an SD-JWT presentation")
	}
	issuerJWT, disclosureStrings, keyBindingJWT := parts[0], parts[1:len(parts)-1], parts[len(parts)-1]
	if keyBindingJWT == "" {
		return nil, errors.New("presentation has no key-binding JWT")
	}

	claims := jwt.MapClaims{}
	var issuer string
	_, err := jwt.NewParser(jwt.WithValidMethods(issuerAlgs), jwt.WithLeeway(leeway), jwt.WithTimeFunc(now)).ParseWithClaims(issuerJWT, claims,
		func(t *jwt.Token) (any, error) {
			if typ, _ := t.Header["typ"].(string); typ != typeCredential && typ != typeCredentialV {
				return nil, fmt.Errorf("unexpected credential typ %q", typ)
			}
			issuer, _ = claims["iss"].(string)
			if issuer == "" {
				return nil, errors.New("credential has no issuer")
			}
			return o.Keys.Key(issuer, t.Header)
		})
	if err != nil {
		return nil, fmt.Errorf("issuer signature: %w", err)
	}

	if alg, ok := claims["_sd_alg"]; ok && alg != sdAlgSHA256 {
		return nil, fmt.Errorf("unsupported _sd_alg %v", alg)
	}
	disclosures, err := parseDisclosures(disclosureStrings)
	if err != nil {
		return nil, err
	}
	expanded, err := expandObject(map[string]any(claims), disclosures, map[string]bool{})
	if err != nil {
		return nil, err
	}
	for digest, d := range disclosures {
		if !d.used {
			return nil, fmt.Errorf("disclosure %s is not referenced by the credential", digest)
		}
	}

	holderKey, err := holderKeyOf(claims)
	if err != nil {
		return nil, err
	}
	signed := strings.Join(parts[:len(parts)-1], "~") + "~"
	if err := verifyKeyBinding(keyBindingJWT, holderKey, signed, o, now(), maxAge); err != nil {
		return nil, fmt.Errorf("key binding: %w", err)
	}

	vct, _ := expanded["vct"].(string)
	if vct == "" {
		return nil, errors.New("credential has no vct")
	}
	delete(expanded, "_sd_alg")
	return &Verified{Issuer: issuer, Type: vct, Claims: expanded}, nil
}

func verifyKeyBinding(token string, key crypto.PublicKey, signed string, o Options, now time.Time, maxAge time.Duration) error {
	claims := jwt.MapClaims{}
	_, err := jwt.NewParser(jwt.WithValidMethods(holderAlgs), jwt.WithLeeway(leeway), jwt.WithTimeFunc(func() time.Time { return now })).
		ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
			if typ, _ := t.Header["typ"].(string); typ != typeKeyBinding {
				return nil, fmt.Errorf("unexpected typ %q", typ)
			}
			return key, nil
		})
	if err != nil {
		return err
	}
	if !audienceContains(claims["aud"], o.Audience) {
		return errors.New("audience does not match this verifier")
	}
	if nonce, _ := claims["nonce"].(string); nonce != o.Nonce {
		return errors.New("nonce does not match the request")
	}
	if want := digest(signed); claims["sd_hash"] != want {
		return errors.New("sd_hash does not cover this presentation")
	}
	iat, err := claims.GetIssuedAt()
	if err != nil || iat == nil {
		return errors.New("missing iat")
	}
	if age := now.Sub(iat.Time); age > maxAge || age < -leeway {
		return errors.New("iat is outside the accepted window")
	}
	return nil
}

func audienceContains(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, a := range v {
			if a == want {
				return true
			}
		}
	}
	return false
}

type disclosure struct {
	name  string // empty for an array element
	value any
	array bool
	used  bool
}

func parseDisclosures(encoded []string) (map[string]*disclosure, error) {
	out := make(map[string]*disclosure, len(encoded))
	for _, e := range encoded {
		raw, err := base64.RawURLEncoding.DecodeString(e)
		if err != nil {
			return nil, errors.New("disclosure is not base64url")
		}
		var parts []any
		if err := json.Unmarshal(raw, &parts); err != nil {
			return nil, errors.New("disclosure is not a JSON array")
		}
		d := &disclosure{}
		switch len(parts) {
		case 3:
			name, ok := parts[1].(string)
			if !ok || name == "" || name == "_sd" || name == arrayDigestKey {
				return nil, errors.New("disclosure has an invalid claim name")
			}
			d.name, d.value = name, parts[2]
		case 2:
			d.array, d.value = true, parts[1]
		default:
			return nil, errors.New("disclosure must have two or three elements")
		}
		key := digest(e)
		if _, dup := out[key]; dup {
			return nil, errors.New("duplicate disclosure")
		}
		out[key] = d
	}
	return out, nil
}

// expandObject returns o with `_sd` digests replaced by the disclosed claims, recursively. seen guards against a digest appearing twice.
func expandObject(o map[string]any, disclosures map[string]*disclosure, seen map[string]bool) (map[string]any, error) {
	out := make(map[string]any, len(o))
	for k, v := range o {
		if k == "_sd" {
			continue
		}
		expanded, err := expandValue(v, disclosures, seen)
		if err != nil {
			return nil, err
		}
		out[k] = expanded
	}
	digests, _ := o["_sd"].([]any)
	for _, raw := range digests {
		key, _ := raw.(string)
		if seen[key] {
			return nil, errors.New("a digest appears more than once")
		}
		seen[key] = true
		d, ok := disclosures[key]
		if !ok {
			continue // a decoy digest
		}
		if d.array {
			return nil, errors.New("an array-element disclosure is referenced from an object")
		}
		if _, clash := out[d.name]; clash {
			return nil, fmt.Errorf("disclosed claim %q collides with an existing claim", d.name)
		}
		d.used = true
		expanded, err := expandValue(d.value, disclosures, seen)
		if err != nil {
			return nil, err
		}
		out[d.name] = expanded
	}
	return out, nil
}

func expandValue(v any, disclosures map[string]*disclosure, seen map[string]bool) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		return expandObject(v, disclosures, seen)
	case jwt.MapClaims:
		return expandObject(map[string]any(v), disclosures, seen)
	case []any:
		out := make([]any, 0, len(v))
		for _, el := range v {
			if m, ok := el.(map[string]any); ok && len(m) == 1 {
				if raw, ok := m[arrayDigestKey]; ok {
					key, _ := raw.(string)
					if seen[key] {
						return nil, errors.New("a digest appears more than once")
					}
					seen[key] = true
					d, ok := disclosures[key]
					if !ok {
						continue // a decoy digest
					}
					if !d.array {
						return nil, errors.New("an object-property disclosure is referenced from an array")
					}
					d.used = true
					expanded, err := expandValue(d.value, disclosures, seen)
					if err != nil {
						return nil, err
					}
					out = append(out, expanded)
					continue
				}
			}
			expanded, err := expandValue(el, disclosures, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded)
		}
		return out, nil
	default:
		return v, nil
	}
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// holderKeyOf reads the key the credential is bound to from its `cnf.jwk` claim.
func holderKeyOf(claims jwt.MapClaims) (crypto.PublicKey, error) {
	cnf, _ := claims["cnf"].(map[string]any)
	jwk, _ := cnf["jwk"].(map[string]any)
	if jwk == nil {
		return nil, errors.New("credential is not bound to a holder key (no cnf.jwk)")
	}
	str := func(k string) string { s, _ := jwk[k].(string); return s }
	b64 := func(k string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(str(k)) }

	switch {
	case str("kty") == "OKP" && str("crv") == "Ed25519":
		x, err := b64("x")
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 holder key")
		}
		return ed25519.PublicKey(x), nil
	case str("kty") == "EC" && str("crv") == "P-256":
		x, errX := b64("x")
		y, errY := b64("y")
		if errX != nil || errY != nil {
			return nil, errors.New("invalid P-256 holder key")
		}
		key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !key.Curve.IsOnCurve(key.X, key.Y) {
			return nil, errors.New("holder key is not on the P-256 curve")
		}
		return key, nil
	}
	return nil, fmt.Errorf("unsupported holder key %s/%s", str("kty"), str("crv"))
}
