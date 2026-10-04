package sdjwt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var now = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// fixture mints SD-JWT presentations the way an issuer and a holder would, so every check in Verify can be exercised on its own.
type fixture struct {
	issuerKey, holderKey ed25519.PrivateKey
	issuer               string
	claims               map[string]any      // plain claims, signed as they are
	disclosable          map[string]any      // claims the issuer makes selectively disclosable
	keyBindingKey        ed25519.PrivateKey  // signs the key binding instead of holderKey
	present              []string            // names of the disclosable claims the holder reveals; nil reveals all
	credential           func(jwt.MapClaims) // edits the issuer-signed payload before signing
	keyBinding           func(jwt.MapClaims) // edits the key-binding payload before signing
	audience, nonce      string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	issuerPub, issuerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, holderKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		issuerKey: issuerKey, holderKey: holderKey, issuer: DIDKeyFromEd25519(issuerPub),
		claims:      map[string]any{"vct": "Diploma"},
		disclosable: map[string]any{"degree": "Mathematics", "university": "Trusted University"},
		audience:    "redirect_uri:https://verifier.test/response", nonce: "nonce-1",
	}
}

func (f *fixture) options() Options {
	return Options{Keys: DIDKey{}, Audience: f.audience, Nonce: f.nonce, Now: func() time.Time { return now }}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (f *fixture) mint(t *testing.T) string {
	t.Helper()
	payload := jwt.MapClaims{
		"iss": f.issuer, "iat": now.Add(-time.Hour).Unix(), "_sd_alg": "sha-256",
		"cnf": map[string]any{"jwk": map[string]any{"kty": "OKP", "crv": "Ed25519", "x": b64(f.holderKey.Public().(ed25519.PublicKey))}},
	}
	for k, v := range f.claims {
		payload[k] = v
	}
	disclosures := map[string]string{}
	var digests []any
	for name, value := range f.disclosable {
		raw, _ := json.Marshal([]any{"salt-" + name, name, value})
		d := b64(raw)
		disclosures[name] = d
		digests = append(digests, digest(d))
	}
	payload["_sd"] = digests
	if f.credential != nil {
		f.credential(payload)
	}

	issuerJWT := sign(t, f.issuerKey, jwt.MapClaims(payload), "dc+sd-jwt")
	presentation := issuerJWT + "~"
	for name, d := range disclosures {
		if f.present == nil || contains(f.present, name) {
			presentation += d + "~"
		}
	}
	kb := jwt.MapClaims{"aud": f.audience, "nonce": f.nonce, "iat": now.Unix(), "sd_hash": digest(presentation)}
	if f.keyBinding != nil {
		f.keyBinding(kb)
	}
	kbKey := f.holderKey
	if f.keyBindingKey != nil {
		kbKey = f.keyBindingKey
	}
	return presentation + sign(t, kbKey, kb, "kb+jwt")
}

func sign(t *testing.T, key ed25519.PrivateKey, claims jwt.MapClaims, typ string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["typ"] = typ
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestVerify(t *testing.T) {
	f := newFixture(t)
	got, err := Verify(f.mint(t), f.options())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Issuer != f.issuer || got.Type != "Diploma" {
		t.Errorf("issuer/type = %q/%q", got.Issuer, got.Type)
	}
	if got.Claims["degree"] != "Mathematics" || got.Claims["university"] != "Trusted University" {
		t.Errorf("claims = %v, want both disclosed claims", got.Claims)
	}
	if _, ok := got.Claims["_sd"]; ok {
		t.Error("the _sd digests must not appear in the claims")
	}
}

func TestVerifyDisclosesOnlyWhatTheHolderReveals(t *testing.T) {
	f := newFixture(t)
	f.present = []string{"degree"}

	got, err := Verify(f.mint(t), f.options())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Claims["degree"] != "Mathematics" {
		t.Errorf("degree = %v, want the disclosed value", got.Claims["degree"])
	}
	if _, ok := got.Claims["university"]; ok {
		t.Error("university was not disclosed and must be absent")
	}
}

func TestVerifyNestedAndArrayDisclosures(t *testing.T) {
	f := newFixture(t)
	f.disclosable = map[string]any{}
	nested, _ := json.Marshal([]any{"s1", "city", "Innsbruck"})
	element, _ := json.Marshal([]any{"s2", "math"})
	f.credential = func(p jwt.MapClaims) {
		p["address"] = map[string]any{"_sd": []any{digest(b64(nested))}}
		p["subjects"] = []any{map[string]any{"...": digest(b64(element))}, "physics"}
	}
	presentation := f.mint(t)
	// The fixture discloses nothing by itself, so splice the two disclosures in and re-sign the key binding over the new text.
	head, _, _ := strings.Cut(presentation, "~")
	withDisclosures := head + "~" + b64(nested) + "~" + b64(element) + "~"
	kb := sign(t, f.holderKey, jwt.MapClaims{"aud": f.audience, "nonce": f.nonce, "iat": now.Unix(), "sd_hash": digest(withDisclosures)}, "kb+jwt")

	got, err := Verify(withDisclosures+kb, f.options())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Claims["address"].(map[string]any)["city"] != "Innsbruck" {
		t.Errorf("address = %v, want the nested disclosure", got.Claims["address"])
	}
	subjects := got.Claims["subjects"].([]any)
	if len(subjects) != 2 || subjects[0] != "math" || subjects[1] != "physics" {
		t.Errorf("subjects = %v, want [math physics]", subjects)
	}
}

func TestVerifyRejects(t *testing.T) {
	cases := map[string]struct {
		prepare func(f *fixture)
		after   func(f *fixture, presentation string, o *Options) string // edits the finished presentation or the options
		want    string                                                   // part of the expected error
	}{
		"no holder key in the credential": {prepare: func(f *fixture) { f.credential = func(p jwt.MapClaims) { delete(p, "cnf") } }, want: "cnf.jwk"},
		"credential without a type":       {prepare: func(f *fixture) { delete(f.claims, "vct") }, want: "no vct"},
		"expired credential":              {prepare: func(f *fixture) { f.credential = func(p jwt.MapClaims) { p["exp"] = now.Add(-time.Hour).Unix() } }, want: "expired"},
		"credential not valid yet":        {prepare: func(f *fixture) { f.credential = func(p jwt.MapClaims) { p["nbf"] = now.Add(time.Hour).Unix() } }, want: "not valid yet"},
		"issuer that is not a did:key":    {prepare: func(f *fixture) { f.credential = func(p jwt.MapClaims) { p["iss"] = "https://issuer.test" } }, want: "not a did:key"},
		"signature by another key": {
			prepare: func(f *fixture) {
				_, other, _ := ed25519.GenerateKey(rand.Reader)
				f.issuerKey = other
			},
			want: "signature",
		},
		"unsupported digest algorithm": {prepare: func(f *fixture) { f.credential = func(p jwt.MapClaims) { p["_sd_alg"] = "sha-512" } }, want: "_sd_alg"},
		"wrong audience":               {prepare: func(f *fixture) { f.keyBinding = func(p jwt.MapClaims) { p["aud"] = "redirect_uri:https://evil.test" } }, want: "audience"},
		"wrong nonce":                  {prepare: func(f *fixture) { f.keyBinding = func(p jwt.MapClaims) { p["nonce"] = "other" } }, want: "nonce"},
		"sd_hash of another text":      {prepare: func(f *fixture) { f.keyBinding = func(p jwt.MapClaims) { p["sd_hash"] = digest("something else") } }, want: "sd_hash"},
		"old key binding":              {prepare: func(f *fixture) { f.keyBinding = func(p jwt.MapClaims) { p["iat"] = now.Add(-time.Hour).Unix() } }, want: "iat"},
		"key binding without iat":      {prepare: func(f *fixture) { f.keyBinding = func(p jwt.MapClaims) { delete(p, "iat") } }, want: "iat"},
		"key binding by another key": {
			prepare: func(f *fixture) { _, f.keyBindingKey, _ = ed25519.GenerateKey(rand.Reader) },
			want:    "key binding",
		},
		"no key binding": {
			after: func(_ *fixture, presentation string, _ *Options) string {
				return presentation[:strings.LastIndex(presentation, "~")+1]
			},
			want: "no key-binding",
		},
		"disclosure that was changed": {
			after: func(f *fixture, presentation string, _ *Options) string {
				raw, _ := json.Marshal([]any{"salt-degree", "degree", "Forged"})
				parts := strings.Split(presentation, "~")
				parts[1] = b64(raw)
				return strings.Join(parts, "~")
			},
			want: "not referenced",
		},
		"disclosure that is not in the credential": {
			after: func(f *fixture, presentation string, _ *Options) string {
				raw, _ := json.Marshal([]any{"salt", "extra", "claim"})
				i := strings.LastIndex(presentation, "~")
				kbStart := presentation[i+1:]
				return presentation[:i] + "~" + b64(raw) + "~" + kbStart
			},
			want: "not referenced",
		},
		"disclosure that repeats": {
			after: func(f *fixture, presentation string, _ *Options) string {
				parts := strings.Split(presentation, "~")
				return strings.Join(append(parts[:2], append([]string{parts[1]}, parts[2:]...)...), "~")
			},
			want: "duplicate",
		},
		"not an SD-JWT": {after: func(*fixture, string, *Options) string { return "a.b.c" }, want: "not an SD-JWT"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if tc.prepare != nil {
				tc.prepare(f)
			}
			o := f.options()
			presentation := f.mint(t)
			if tc.after != nil {
				presentation = tc.after(f, presentation, &o)
			}
			_, err := Verify(presentation, o)
			if err == nil {
				t.Fatal("Verify: want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestDIDKeyRoundTrip(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	got, err := DIDKey{}.Key(DIDKeyFromEd25519(pub), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.(ed25519.PublicKey).Equal(pub) {
		t.Error("the key resolved from the did:key differs from the key it was made from")
	}
	for _, bad := range []string{"did:web:example.com", "did:key:zQ3shokFTS3brHcDQrn82RUDfCZESWL1ZdCEJwekUDPQiYBme", "did:key:z0OIl"} {
		if _, err := (DIDKey{}).Key(bad, nil); err == nil {
			t.Errorf("Key(%q): want error, got nil", bad)
		}
	}
}

// testdata/credo-presentation.json was recorded from a Credo wallet answering an OpenID4VP request (examples/issuer/scripts/sdjwt-fixture.ts); it
// checks that this verifier accepts what a real wallet produces.
func TestVerifyCredoPresentation(t *testing.T) {
	raw, err := os.ReadFile("testdata/credo-presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Issuer, Audience string
		IssuedAt         int64
		Full             struct{ Nonce, VPToken string }
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	var vpToken map[string][]string
	if err := json.Unmarshal([]byte(recorded.Full.VPToken), &vpToken); err != nil || len(vpToken["credential"]) != 1 {
		t.Fatalf("vp_token = %s, want one presentation under \"credential\"", recorded.Full.VPToken)
	}

	o := Options{Keys: DIDKey{}, Audience: recorded.Audience, Nonce: recorded.Full.Nonce, Now: func() time.Time { return time.Unix(recorded.IssuedAt, 0) }}
	got, err := Verify(vpToken["credential"][0], o)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Issuer != recorded.Issuer || got.Type != "Diploma" {
		t.Errorf("issuer/type = %q/%q, want %q/Diploma", got.Issuer, got.Type, recorded.Issuer)
	}
	if got.Claims["degree"] != "Mathematics" || got.Claims["email"] != "ada@example.com" {
		t.Errorf("claims = %v, want the requested degree and email disclosed", got.Claims)
	}
	for _, hidden := range []string{"name", "university"} {
		if _, ok := got.Claims[hidden]; ok {
			t.Errorf("%s was not requested and must stay undisclosed", hidden)
		}
	}

	o.Nonce = "another"
	if _, err := Verify(vpToken["credential"][0], o); err == nil {
		t.Error("Verify with another nonce: want error, got nil")
	}
}
