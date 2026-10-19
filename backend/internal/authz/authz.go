// Package authz evaluates authorization policies written in Rego, executed in-process by OPA — no OPA server is involved.
package authz

// Input is what gets passed into a policy's Authorize function. Type and Issuer are the credential type and issuer that a verified presentation
// established — the policy decides whether that combination is acceptable for Resource.
type Input struct {
	Resource string
	Type     string
	Issuer   string
	// Claims are the disclosed claims the policy may base its decision on, by name. The values are strings; the caller encodes anything else.
	Claims map[string]string
	// UserRoles are asserted by the calling application, not verified by Attesta; empty when it sent none.
	UserRoles []string
	// Others are the further credentials of the same person that the request asked for, by credential type. A request without any is evaluated with
	// Authorize, one with some with AuthorizeWithOthers.
	Others map[string]Other
}

// Other is a further credential a presentation held: who issued it and its disclosed claims, as strings.
type Other struct {
	Issuer string
	Claims map[string]string
}

// Decision is one of the decisions standardized by XACML.
type Decision string

const (
	Permit        Decision = "Permit"
	Deny          Decision = "Deny"
	NotApplicable Decision = "NotApplicable"
	Indeterminate Decision = "Indeterminate"
)

func (d Decision) valid() bool {
	return d == Permit || d == Deny || d == NotApplicable || d == Indeterminate
}

// Attribute is a named string value that an obligation or advice hands to the application.
type Attribute struct {
	ID    string
	Value string
}

// Statement is one obligation or one advice. Attributes are sorted by ID.
type Statement struct {
	ID         string
	Attributes []Attribute
}

// Result is a policy's decision, the human-readable reason for it, and the obligations and advice that go with it.
type Result struct {
	Decision    Decision
	Reason      string
	Obligations []Statement
	Advice      []Statement
}

// Evaluator runs Rego policy source against an Input and returns a Result.
type Evaluator interface {
	// Validate reports whether source is a policy Evaluate can run: it compiles, is in package policy and defines the rule decision. Used to reject
	// bad policy source before it's stored.
	Validate(source string) error
	// Evaluate runs the rule data.policy.decision of source with the Input as input: resource, type, issuer, claims, userRoles and others (by
	// credential type: issuer and claims). The rule's value is an object with decision (Permit, Deny, NotApplicable or Indeterminate), reason, and
	// the lists obligations and advice, whose items are objects with an id and attributes, an object of string values. A rule that is undefined
	// gives Indeterminate; a policy that cannot be run or returns a value of another shape gives an error.
	Evaluate(source string, in Input) (Result, error)
	// Diagnose lists the problems that make Validate refuse source, each with the place in the source; none means source is accepted.
	Diagnose(source string) []Diagnostic
}
