// Package credential is what each credential type holds: its claims, which of them the holder may disclose selectively, and when it ends.
package credential

import (
	"errors"
	"fmt"
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
)

// Field is one claim of a credential type and the input of its form.
type Field struct {
	// Key is the claim name.
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
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
		if !slices.Contains([]string{KindText, KindEmail, KindDate, KindChoice}, f.Kind) {
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
