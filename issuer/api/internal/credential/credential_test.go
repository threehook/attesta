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

var diploma = credential.Type{
	Type: "Diploma Laadpalen Management", Path: "laadpalen-management", Title: "Diploma Laadpalen Management", Description: "Bewijs",
	Fields: []credential.Field{
		{Key: "name", Label: "Naam", Kind: "text", Required: true},
		{Key: "email", Label: "E-mailadres", Kind: "email", Required: true},
		{Key: "validUntil", Label: "Geldig tot", Kind: "date", Required: true},
	},
}

var employee = map[string]string{
	"name": "Jerry Smith", "email": "jerry@example.com", "department": "burgerzaken", "organisation": "Gemeente Vlierdam",
	"roles": "laadpalen-aanvrager",
}

func TestEmployeeHoldsDepartmentAndRolesAndNothingAboutCourses(t *testing.T) {
	c, err := credential.Content(credential.EmployeeType, employee, "Gemeente Vlierdam")
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "Employee" || c.Claims["issuer_name"] != "Gemeente Vlierdam" || !c.ExpiresAt.IsZero() {
		t.Fatalf("content %+v", c)
	}
	for _, name := range []string{"name", "email", "department", "organisation", "roles", "description"} {
		if _, ok := c.Disclosable[name]; !ok {
			t.Errorf("%s is not disclosable", name)
		}
	}
	for _, name := range []string{"diploma", "diplomaValidUntil", "course"} {
		if _, ok := c.Disclosable[name]; ok {
			t.Errorf("%s must not be in an Employee", name)
		}
	}
}

func TestDiplomaEndsAtTheEndOfItsLastDayAndHoldsNoCourse(t *testing.T) {
	c, err := credential.Content(diploma, map[string]string{
		"name": "Jerry", "email": "j@example.com", "validUntil": "2099-06-30",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "Diploma Laadpalen Management" || !c.ExpiresAt.Equal(time.Date(2099, 6, 30, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("content %+v", c)
	}
	if _, ok := c.Disclosable["course"]; ok {
		t.Fatalf("a diploma is its own type, it has no course claim: %v", c.Disclosable)
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
	if _, err := credential.Content(credential.EmployeeType, without("department"), ""); err == nil {
		t.Error("missing department accepted")
	}
	if _, err := credential.Content(credential.EmployeeType, without("roles"), ""); err != nil {
		t.Errorf("an Employee without the roles claim was refused: %v", err)
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
	c, err := credential.Content(credential.EmployeeType, employee, "Gemeente Vlierdam")
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

func TestTitleAndDescriptionAreFixedByTheTypeWhateverTheEntitlementSays(t *testing.T) {
	claims := map[string]string{"name": "n", "email": "e", "department": "burgerzaken", "organisation": "o", "title": "Eigen titel", "description": "Eigen tekst"}
	c, err := credential.Content(credential.EmployeeType, claims, "Gemeente")
	if err != nil || c.Claims["title"] != "Medewerker afdeling & rollen" || c.Disclosable["title"] != nil {
		t.Fatalf("%v %v", c, err)
	}
	if want := "Deze credential is verplicht om werkzaamheden te mogen verrichten voor onze gemeente. Het geeft aan voor welke afdeling je werkt en welke rollen je hebt"; c.Disclosable["description"] != want {
		t.Fatalf("description %v", c.Disclosable["description"])
	}
	d, err := credential.Content(diploma, map[string]string{"name": "n", "email": "e", "validUntil": "2099-01-01"}, "")
	if err != nil || d.Claims["title"] != "Diploma Laadpalen Management" || d.Disclosable["description"] == "" {
		t.Fatalf("%v %v", d, err)
	}
}

func TestGivesAnEmployeeItsRolesAsAListEvenWhenThereAreNone(t *testing.T) {
	employee := map[string]string{"description": "d", "name": "n", "email": "e", "department": "burgerzaken", "organisation": "o", "roles": "a, b,a"}
	c, err := credential.Content(credential.EmployeeType, employee, "Gemeente")
	roles, _ := c.Disclosable["roles"].([]string)
	if err != nil || len(roles) != 2 || roles[0] != "a" || roles[1] != "b" {
		t.Fatalf("%v %v", c.Disclosable, err)
	}
	delete(employee, "roles")
	none, _ := credential.Content(credential.EmployeeType, employee, "Gemeente")
	if list, ok := none.Disclosable["roles"].([]string); !ok || len(list) != 0 {
		t.Fatalf("roles %v, want an empty list", none.Disclosable["roles"])
	}
}

func TestAnOptionalFieldWithoutAnAnswerIsLeftOutAndARequiredOneIsNot(t *testing.T) {
	def := credential.Type{Type: "Badge", Title: "Badge", Fields: []credential.Field{
		{Key: "email", Label: "E-mail", Kind: "email", Required: true},
		{Key: "floor", Label: "Verdieping", Kind: "text"},
		{Key: "colour", Label: "Kleur", Kind: "choice", Required: true, Options: []string{"red", "blue"}},
	}}
	c, err := credential.Content(def, map[string]string{"email": "e@example.com", "colour": "red"}, "")
	if err != nil || c.Disclosable["colour"] != "red" || c.Disclosable["email"] != "e@example.com" || !c.ExpiresAt.IsZero() {
		t.Fatalf("%+v %v", c, err)
	}
	if _, ok := c.Disclosable["floor"]; ok {
		t.Fatal("an unanswered optional field was written")
	}
	if _, err := credential.Content(def, map[string]string{"email": "e@example.com"}, ""); err == nil {
		t.Fatal("a missing required field was accepted")
	}
}

func fields(edit func(*[]credential.Field)) credential.Type {
	f := []credential.Field{{Key: "email", Label: "E-mail", Kind: "email", Required: true}, {Key: "name", Label: "Naam", Kind: "text"}}
	if edit != nil {
		edit(&f)
	}
	return credential.Type{Title: "Badge", Fields: f}
}

func TestChecksTheFormOfACredentialType(t *testing.T) {
	if err := fields(nil).Check(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*[]credential.Field){
		"no email":       func(f *[]credential.Field) { (*f)[0].Key = "mail" },
		"optional email": func(f *[]credential.Field) { (*f)[0].Required = false },
		"a second email": func(f *[]credential.Field) {
			*f = append(*f, credential.Field{Key: "other", Label: "x", Kind: "email"})
		},
		"duplicate key":     func(f *[]credential.Field) { (*f)[1].Key = "email" },
		"reserved key":      func(f *[]credential.Field) { (*f)[1].Key = "title" },
		"bad key":           func(f *[]credential.Field) { (*f)[1].Key = "Naam" },
		"no label":          func(f *[]credential.Field) { (*f)[1].Label = " " },
		"unknown kind":      func(f *[]credential.Field) { (*f)[1].Kind = "number" },
		"choice no options": func(f *[]credential.Field) { (*f)[1].Kind = "choice" },
		"options on text":   func(f *[]credential.Field) { (*f)[1].Options = []string{"a"} },
		"duplicate options": func(f *[]credential.Field) { (*f)[1].Kind, (*f)[1].Options = "choice", []string{"a", "a"} },
		"validUntil text": func(f *[]credential.Field) {
			*f = append(*f, credential.Field{Key: "validUntil", Label: "x", Kind: "text"})
		},
		"no fields": func(f *[]credential.Field) { *f = nil },
	}
	for name, edit := range bad {
		if err := fields(edit).Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (credential.Type{Title: " ", Fields: fields(nil).Fields}).Check(); err == nil {
		t.Error("a blank title was accepted")
	}
}

func TestSlugKeepsLettersAndDigitsAndSeparatesWithHyphens(t *testing.T) {
	for in, want := range map[string]string{"Diploma Laadpalen Management": "diploma-laadpalen-management", "  Bedrijfs-pas #2! ": "bedrijfs-pas-2", "???": ""} {
		if got := credential.Slug(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
