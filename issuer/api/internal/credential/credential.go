// Package credential is what each credential type holds: its claims, which of them the holder may disclose selectively, and when it ends.
package credential

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	sdjwt "github.com/threehook/sdjwt-go"
)

const (
	Diploma  = "Diploma"
	Employee = "Employee"
)

var Departments = []string{"burgerzaken", "secretariaat"}

// DefaultDescriptions are pre-filled in the forms and used when a request leaves the description out.
var DefaultDescriptions = map[string]string{
	Diploma:  "Diploma uitgereikt door het opleidingsinstituut",
	Employee: "Autorisatie om laadpalen aan te vragen voor burgers van onze gemeente",
}

// disclosable lists, per type, the claims the holder may reveal one by one. Every one of them is also required.
var disclosable = map[string][]string{
	Diploma:  {"description", "name", "email", "degree", "university"},
	Employee: {"description", "name", "email", "department", "organisation", "diploma"},
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
// The Employee credential ends at the end of the day its diploma is valid until (`diplomaValidUntil`).
func Content(credType string, claims map[string]string, issuerName string) (sdjwt.Credential, error) {
	names, ok := disclosable[credType]
	if !ok {
		return sdjwt.Credential{}, fmt.Errorf("unknown credential type %q", credType)
	}
	c := sdjwt.Credential{Type: credType, Claims: map[string]any{}, Disclosable: map[string]any{}}
	if issuerName != "" {
		c.Claims["issuer_name"] = issuerName
	}
	for _, name := range names {
		value, ok := claims[name]
		if !ok {
			return sdjwt.Credential{}, fmt.Errorf("%s credential has no %s", credType, name)
		}
		c.Disclosable[name] = value
	}
	if credType == Employee {
		day, err := ParseDate(claims["diplomaValidUntil"])
		if err != nil {
			return sdjwt.Credential{}, fmt.Errorf("diplomaValidUntil: %w", err)
		}
		c.ExpiresAt = EndOfDay(day)
	}
	return c, nil
}
