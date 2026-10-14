package credential_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	sdjwt "github.com/threehook/sdjwt-go"

	"attesta/issuer/internal/credential"
	"attesta/issuer/internal/identity"
)

var employee = map[string]string{
	"name": "Jerry Smith", "email": "jerry@example.com", "department": "burgerzaken", "organisation": "Gemeente Vlierdam",
	"diploma": "Laadpalen", "diplomaValidUntil": "31-12-2099", "description": "Autorisatie",
}

func TestEmployeeEndsAtTheEndOfTheDiplomasLastDay(t *testing.T) {
	c, err := credential.Content(credential.Employee, employee, "Gemeente Vlierdam")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC); !c.ExpiresAt.Equal(want) {
		t.Fatalf("ends %s, want %s", c.ExpiresAt, want)
	}
	if c.Type != "Employee" || c.Claims["issuer_name"] != "Gemeente Vlierdam" {
		t.Fatalf("content %+v", c)
	}
	for _, name := range []string{"name", "email", "department", "organisation", "diploma", "description"} {
		if _, ok := c.Disclosable[name]; !ok {
			t.Errorf("%s is not disclosable", name)
		}
	}
	if _, ok := c.Disclosable["diplomaValidUntil"]; ok {
		t.Error("diplomaValidUntil must not be in the credential")
	}
}

func TestDiplomaHasNoEnd(t *testing.T) {
	c, err := credential.Content(credential.Diploma, map[string]string{
		"name": "Jerry", "email": "j@example.com", "degree": "Wiskunde", "university": "Universiteit Utrecht", "description": "Diploma",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !c.ExpiresAt.IsZero() {
		t.Fatalf("ends %s", c.ExpiresAt)
	}
	if _, ok := c.Claims["issuer_name"]; ok {
		t.Fatal("an issuer name was written without one being set")
	}
}

func TestRejectsWhatItCannotBuild(t *testing.T) {
	without := func(name string) map[string]string {
		m := map[string]string{}
		for k, v := range employee {
			if k != name {
				m[k] = v
			}
		}
		return m
	}
	if _, err := credential.Content("Passport", employee, ""); err == nil {
		t.Error("unknown type accepted")
	}
	if _, err := credential.Content(credential.Employee, without("department"), ""); err == nil {
		t.Error("missing department accepted")
	}
	if _, err := credential.Content(credential.Employee, without("diplomaValidUntil"), ""); err == nil {
		t.Error("missing end date accepted")
	}
}

func TestParsesBothDateFormats(t *testing.T) {
	for _, s := range []string{"31-12-2099", "2099-12-31", " 31-12-2099 "} {
		d, err := credential.ParseDate(s)
		if err != nil || d.Year() != 2099 || d.Month() != 12 || d.Day() != 31 {
			t.Errorf("%q: %v %v", s, d, err)
		}
	}
	for _, s := range []string{"", "tomorrow", "31/12/2099", "2099-13-01", "32-01-2099"} {
		if _, err := credential.ParseDate(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

// What the verifier of the backend makes of the issued Employee credential: the holder shows only the email.
func TestAnIssuedEmployeeCredentialVerifies(t *testing.T) {
	issuer := identity.FromSeed("test")
	_, holder, _ := ed25519.GenerateKey(rand.Reader)
	c, err := credential.Content(credential.Employee, employee, "Gemeente Vlierdam")
	if err != nil {
		t.Fatal(err)
	}
	c.HolderKey = map[string]any{"kty": "OKP", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(holder.Public().(ed25519.PublicKey))}
	issued, err := sdjwt.Issue(issuer.Key, c)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(strings.TrimSuffix(issued, "~"), "~")
	shown := parts[0] + "~"
	for _, d := range parts[1:] {
		raw, _ := base64.RawURLEncoding.DecodeString(d)
		if strings.Contains(string(raw), `"email"`) {
			shown += d + "~"
		}
	}
	sum := sdHash(shown)
	kb := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{"aud": "a", "nonce": "n", "iat": time.Now().Unix(), "sd_hash": sum})
	kb.Header["typ"] = "kb+jwt"
	signed, err := kb.SignedString(holder)
	if err != nil {
		t.Fatal(err)
	}

	v, err := sdjwt.Verify(shown+signed, sdjwt.Options{Keys: sdjwt.DIDKey{}, Audience: "a", Nonce: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Issuer != issuer.DID || v.Type != "Employee" || v.Claims["email"] != "jerry@example.com" || v.Claims["issuer_name"] != "Gemeente Vlierdam" {
		t.Fatalf("verified %+v", v)
	}
	if _, ok := v.Claims["department"]; ok {
		t.Fatal("department was not shown")
	}
}

func sdHash(presentation string) string {
	sum := sha256.Sum256([]byte(presentation))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
