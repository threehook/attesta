// Package credential is what each credential type holds: its claims, which of them the holder may disclose selectively, and when it ends.
package credential

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"

	sdjwt "github.com/threehook/sdjwt-go"
)

const Employee = "Employee"

var Departments = []string{"burgerzaken", "secretariaat"}

// employeeTitle and employeeDescription are fixed: an Employee credential is always for the same purpose.
const (
	employeeTitle       = "Medewerker afdeling & rollen"
	employeeDescription = "Deze credential is verplicht om werkzaamheden te mogen verrichten voor onze gemeente. Het geeft aan voor welke afdeling je werkt en welke rollen je hebt"
)

// Field kinds a credential type's form offers. Every claim is a string.
const (
	KindText   = "text"
	KindEmail  = "email"
	KindDate   = "date"
	KindChoice = "choice"
	KindNumber = "number"
)

// Field is one claim of a credential type and the input of its form.
type Field struct {
	// Key is the claim name.
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	// Min, Max and Decimals bound a number. Min and Max are decimal texts, so they are exact; either may be empty. Decimals is how many places
	// the number has: 0 for a whole number.
	Min      string `json:"min,omitempty"`
	Max      string `json:"max,omitempty"`
	Decimals int    `json:"decimals,omitempty"`
}

// Type is a credential type the issuer gives out, with a form of its own, so a verifier asks for exactly the type it needs.
type Type struct {
	// Type is the credential type verifiers ask for. It does not change once chosen.
	Type string
	// Path names the type in addresses: its form is at /type/<Path> and an entitlement is recorded at /entitlements/type/<Path>.
	Path string
	// Title is what the wallet and the form call the type.
	Title       string
	Description string
	Fields      []Field
}

// EmployeeType is the Employee credential. Its fields are fixed in code, not in Fields.
var EmployeeType = Type{Type: Employee, Path: "employee", Title: employeeTitle, Description: employeeDescription}

// ValidUntilKey is the date field that ends the credential.
const ValidUntilKey = "validUntil"

var (
	keyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,39}$`)
	// reserved claims are written by the issuer itself.
	reserved = []string{"title", "description", "issuer_name", "iss", "sub", "exp", "iat", "nbf", "cnf", "vct", "status", "jti", "_sd", "_sd_alg"}
)

const (
	maxDecimals  = 6
	maxFields    = 30
	maxTitle     = 100
	maxDesc      = 500
	maxLabel     = 80
	MaxValueSize = 500
)

// Check reports why the title, description or fields of a type cannot be used. Type and Path are not looked at.
func (t Type) Check() error {
	if n := len([]rune(strings.TrimSpace(t.Title))); n == 0 || n > maxTitle {
		return fmt.Errorf("de titel is niet leeg en hoogstens %d tekens", maxTitle)
	}
	if len([]rune(t.Description)) > maxDesc {
		return fmt.Errorf("de beschrijving is hoogstens %d tekens", maxDesc)
	}
	if len(t.Fields) == 0 || len(t.Fields) > maxFields {
		return fmt.Errorf("een formulier heeft 1 tot %d velden", maxFields)
	}
	keys := map[string]bool{}
	email := false
	for _, f := range t.Fields {
		if !keyPattern.MatchString(f.Key) || slices.Contains(reserved, f.Key) {
			return fmt.Errorf("de naam %q van een veld begint met een kleine letter, bevat alleen letters en cijfers en is niet gereserveerd", f.Key)
		}
		if keys[f.Key] {
			return fmt.Errorf("de naam %q komt twee keer voor", f.Key)
		}
		keys[f.Key] = true
		if n := len([]rune(strings.TrimSpace(f.Label))); n == 0 || n > maxLabel {
			return fmt.Errorf("het veld %q heeft een label van 1 tot %d tekens", f.Key, maxLabel)
		}
		if !slices.Contains([]string{KindText, KindEmail, KindDate, KindChoice, KindNumber}, f.Kind) {
			return fmt.Errorf("het veld %q heeft een onbekend soort %q", f.Key, f.Kind)
		}
		if (f.Kind == KindChoice) != (len(f.Options) > 0) {
			return fmt.Errorf("het veld %q heeft opties als en alleen als het een keuzelijst is", f.Key)
		}
		seen := map[string]bool{}
		for _, o := range f.Options {
			if o == "" || o != strings.TrimSpace(o) || len([]rune(o)) > maxLabel || seen[o] {
				return fmt.Errorf("een optie van het veld %q is leeg, heeft spaties aan de rand, is te lang of komt twee keer voor", f.Key)
			}
			seen[o] = true
		}
		if err := f.checkNumber(); err != nil {
			return err
		}
		if f.Key == "email" {
			if f.Kind != KindEmail || !f.Required {
				return errors.New("het veld email is een verplicht e-mailadres")
			}
			email = true
		}
		if f.Kind == KindEmail && f.Key != "email" {
			return errors.New("alleen het veld email is een e-mailadres")
		}
		if f.Key == ValidUntilKey && f.Kind != KindDate {
			return fmt.Errorf("het veld %s is een datum", ValidUntilKey)
		}
	}
	if !email {
		return errors.New("het formulier heeft een veld email: het identificeert de persoon")
	}
	return nil
}

var numberPattern = regexp.MustCompile(`^-?\d{1,30}(\.\d{1,30})?$`)

// parseNumber reads a number with a point or a comma as decimal separator, exactly.
func parseNumber(s string) (*big.Rat, int, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	if !numberPattern.MatchString(s) {
		return nil, 0, false
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, 0, false
	}
	places := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		places = len(s) - i - 1
	}
	return r, places, true
}

// checkNumber reports why the bounds of a field cannot be used; only a number has any.
func (f Field) checkNumber() error {
	if f.Kind != KindNumber {
		if f.Min != "" || f.Max != "" || f.Decimals != 0 {
			return fmt.Errorf("alleen een getal heeft een minimum, een maximum en decimalen (veld %q)", f.Key)
		}
		return nil
	}
	if f.Decimals < 0 || f.Decimals > maxDecimals {
		return fmt.Errorf("het veld %q heeft 0 tot %d decimalen", f.Key, maxDecimals)
	}
	var low, high *big.Rat
	for _, b := range []struct {
		text string
		into **big.Rat
	}{{f.Min, &low}, {f.Max, &high}} {
		if b.text == "" {
			continue
		}
		r, _, ok := parseNumber(b.text)
		if !ok {
			return fmt.Errorf("het minimum en maximum van het veld %q zijn getallen", f.Key)
		}
		*b.into = r
	}
	if low != nil && high != nil && low.Cmp(high) > 0 {
		return fmt.Errorf("het minimum van het veld %q is groter dan het maximum", f.Key)
	}
	return nil
}

// NormalizeNumber checks the answer to a number field against its decimals, minimum and maximum, and writes it with a point and exactly the
// decimals of the field, so the same number is always the same text.
func NormalizeNumber(f Field, answer string) (string, error) {
	r, places, ok := parseNumber(answer)
	if !ok {
		return "", fmt.Errorf("%s must be a number", f.Key)
	}
	if places > f.Decimals {
		if f.Decimals == 0 {
			return "", fmt.Errorf("%s must be a whole number", f.Key)
		}
		return "", fmt.Errorf("%s has at most %d decimals", f.Key, f.Decimals)
	}
	for _, b := range []struct {
		text  string
		above bool
	}{{f.Min, false}, {f.Max, true}} {
		if b.text == "" {
			continue
		}
		bound, _, _ := parseNumber(b.text)
		if c := r.Cmp(bound); (b.above && c > 0) || (!b.above && c < 0) {
			if b.above {
				return "", fmt.Errorf("%s is at most %s", f.Key, bound.FloatString(f.Decimals))
			}
			return "", fmt.Errorf("%s is at least %s", f.Key, bound.FloatString(f.Decimals))
		}
	}
	return r.FloatString(f.Decimals), nil
}

// Slug turns a title into a path: lowercase letters and digits separated by hyphens.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	return b.String()
}

// SplitRoles reads roles stored as one text, separated by commas, as a list without blanks and duplicates.
func SplitRoles(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range strings.Split(text, ",") {
		if r = strings.TrimSpace(r); r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

var dateLayouts = regexp.MustCompile(`^(\d{2})-(\d{2})-(\d{4})$`)

// ParseDate reads dd-mm-yyyy (what the form sends) or YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	s = dateLayouts.ReplaceAllString(strings.TrimSpace(s), "$3-$2-$1")
	return time.Parse(time.DateOnly, s)
}

// EndOfDay is the last second of the given day, UTC.
func EndOfDay(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 0, time.UTC)
}

// Content builds the credential of this type from an entitlement's claims. The holder's key is set by the caller.
// A credential ends at the end of the day in its `validUntil` claim; an Employee does not end.
func Content(def Type, claims map[string]string, issuerName string) (sdjwt.Credential, error) {
	c := sdjwt.Credential{Type: def.Type, Claims: map[string]any{}, Disclosable: map[string]any{}}
	if issuerName != "" {
		c.Claims["issuer_name"] = issuerName
	}
	// The title only names the credential, so it is always visible; the description is for the holder to read.
	c.Claims["title"] = def.Title
	c.Disclosable["description"] = def.Description
	if def.Type == Employee {
		for _, name := range []string{"name", "email", "department", "organisation"} {
			value, ok := claims[name]
			if !ok {
				return sdjwt.Credential{}, fmt.Errorf("%s credential has no %s", def.Type, name)
			}
			c.Disclosable[name] = value
		}
		// An Employee always has the claim, an empty list when staff gave no roles, so a verifier that asks for the roles gets an answer.
		c.Disclosable["roles"] = SplitRoles(claims["roles"])
	} else {
		for _, f := range def.Fields {
			value := claims[f.Key]
			if value == "" {
				if f.Required {
					return sdjwt.Credential{}, fmt.Errorf("%s credential has no %s", def.Type, f.Key)
				}
				continue
			}
			if f.Key != ValidUntilKey {
				c.Disclosable[f.Key] = value
			}
		}
	}
	if claims["validUntil"] != "" {
		day, err := ParseDate(claims["validUntil"])
		if err != nil {
			return sdjwt.Credential{}, fmt.Errorf("validUntil: %w", err)
		}
		c.ExpiresAt = EndOfDay(day)
	}
	return c, nil
}
