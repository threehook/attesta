// Package credential is what each credential type holds: its claims, which of them the holder may disclose selectively, and when it ends.
package credential

import (
	"fmt"
	"regexp"
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

// Diploma is a diploma the issuer gives out. Each is a credential type of its own with a form of its own, so a verifier asks for exactly the diploma it
// needs; adding a diploma is adding an entry to Diplomas.
type Diploma struct {
	// Type is the credential type verifiers ask for.
	Type string
	// Path names the diploma in addresses: the form is at /diploma/<Path> and an entitlement is recorded at /entitlements/diploma/<Path>.
	Path string
	// Title is what the wallet and the form call the diploma.
	Title       string
	Description string
}

var Diplomas = []Diploma{
	{Type: "Diploma Laadpalen Management", Path: "laadpalen-management", Title: "Diploma Laadpalen Management", Description: "Bewijs dat de opleiding Laadpalen Management is afgerond"},
}

// DiplomaByPath finds the diploma whose form and entitlements live at the path.
func DiplomaByPath(path string) (Diploma, bool) {
	for _, d := range Diplomas {
		if d.Path == path {
			return d, true
		}
	}
	return Diploma{}, false
}

func diplomaOfType(credType string) (Diploma, bool) {
	for _, d := range Diplomas {
		if d.Type == credType {
			return d, true
		}
	}
	return Diploma{}, false
}

// Title is what the wallet calls a credential of the type. It is fixed by the type, so the holder always reads the same.
func Title(credType string) string {
	if d, ok := diplomaOfType(credType); ok {
		return d.Title
	}
	return employeeTitle
}

// Description tells the holder what the credential is for. It is fixed by the type.
func Description(credType string) string {
	if d, ok := diplomaOfType(credType); ok {
		return d.Description
	}
	return employeeDescription
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

// disclosableClaims lists the claims of a type the holder may reveal one by one. Every one of them is also required. ok is false for an unknown type.
func disclosableClaims(credType string) (names []string, ok bool) {
	if credType == Employee {
		return []string{"name", "email", "department", "organisation"}, true
	}
	if _, isDiploma := diplomaOfType(credType); isDiploma {
		return []string{"name", "email"}, true
	}
	return nil, false
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
// A credential ends at the end of the day in its `validUntil` claim; a diploma has one, an Employee does not end.
func Content(credType string, claims map[string]string, issuerName string) (sdjwt.Credential, error) {
	names, ok := disclosableClaims(credType)
	if !ok {
		return sdjwt.Credential{}, fmt.Errorf("unknown credential type %q", credType)
	}
	c := sdjwt.Credential{Type: credType, Claims: map[string]any{}, Disclosable: map[string]any{}}
	if issuerName != "" {
		c.Claims["issuer_name"] = issuerName
	}
	// The title only names the credential, so it is always visible; the description is for the holder to read.
	c.Claims["title"] = Title(credType)
	c.Disclosable["description"] = Description(credType)
	for _, name := range names {
		value, ok := claims[name]
		if !ok {
			return sdjwt.Credential{}, fmt.Errorf("%s credential has no %s", credType, name)
		}
		c.Disclosable[name] = value
	}
	// An Employee always has the claim, an empty list when staff gave no roles, so a verifier that asks for the roles gets an answer.
	if credType == Employee {
		c.Disclosable["roles"] = SplitRoles(claims["roles"])
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
