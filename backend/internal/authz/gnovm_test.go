package authz

import (
	"io"
	"reflect"
	"testing"
)

const diplomaPolicyV1 = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	if resource != "diploma-vault" {
		return "Deny", "unknown resource", nil, nil
	}
	if credType != "Diploma" {
		return "Deny", "unexpected credential type", nil, nil
	}
	if issuer != "trusted-university" {
		return "Deny", "issuer not trusted", nil, nil
	}
	return "Permit", "ok", nil, nil
}
`

const diplomaPolicyV2 = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	return "Deny", "v2 denies everything", nil, nil
}
`

func TestGnoVMEvaluate(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	result, err := vm.Evaluate(diplomaPolicyV1, Input{Resource: "diploma-vault", Type: "Diploma", Issuer: "trusted-university"})
	if err != nil {
		t.Fatalf("Evaluate allow case: %v", err)
	}
	if !(result.Decision == Permit) {
		t.Fatalf("Evaluate allow case: got deny, reason %q", result.Reason)
	}

	result, err = vm.Evaluate(diplomaPolicyV1, Input{Resource: "diploma-vault", Type: "Diploma", Issuer: "diploma-mill"})
	if err != nil {
		t.Fatalf("Evaluate deny case: %v", err)
	}
	if result.Decision == Permit {
		t.Fatal("Evaluate deny case: got allow, want deny")
	}
}

// Hot-deploy relies on being able to re-evaluate a different policy source against the same shared store without interference from a previously
// loaded package at the same path.
func TestGnoVMEvaluateAfterHotReload(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	in := Input{Resource: "diploma-vault", Type: "Diploma", Issuer: "trusted-university"}

	result, err := vm.Evaluate(diplomaPolicyV1, in)
	if err != nil || !(result.Decision == Permit) {
		t.Fatalf("Evaluate v1: result=%+v err=%v, want allow", result, err)
	}

	result, err = vm.Evaluate(diplomaPolicyV2, in)
	if err != nil {
		t.Fatalf("Evaluate v2: %v", err)
	}
	if result.Decision == Permit {
		t.Fatalf("Evaluate v2: got allow, want deny (reason %q)", result.Reason)
	}
}

func TestGnoVMValidate(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	if err := vm.Validate(diplomaPolicyV1); err != nil {
		t.Fatalf("Validate well-formed policy: %v", err)
	}

	const (
		sig    = "func Authorize(a, b, c string, d map[string]string, e []string) "
		res    = "(string, string, []map[string]string, []map[string]string)"
		permit = `{ return "Permit", "", nil, nil }`
	)
	for name, source := range map[string]string{
		"malformed":        "package policy\n\nfunc Authorize( {",
		"no Authorize":     "package policy",
		"wrong arg count":  "package policy\n\nfunc Authorize(resource string) " + res + permit,
		"no roles":         "package policy\n\nfunc Authorize(a, b, c string, d map[string]string) " + res + permit,
		"wrong arg type":   "package policy\n\nfunc Authorize(a int, b int, c int, d int, e int) " + res + permit,
		"bool and string":  "package policy\n\n" + sig + `(bool, string) { return true, "" }`,
		"too few results":  "package policy\n\n" + sig + `(string, string) { return "Permit", "" }`,
		"result types":     "package policy\n\n" + sig + `(string, bool, []map[string]string, []map[string]string) { return "Permit", true, nil, nil }`,
		"unknown decision": "package policy\n\n" + sig + res + `{ return "Allow", "", nil, nil }`,
		"no statement id":  "package policy\n\n" + sig + res + `{ return "Permit", "", []map[string]string{{"k": "v"}}, nil }`,
		"nil statement":    "package policy\n\n" + sig + res + `{ return "Permit", "", []map[string]string{nil}, nil }`,
		"panics":           "package policy\n\n" + sig + res + `{ panic("boom") }`,
	} {
		if err := vm.Validate(source); err == nil {
			t.Errorf("Validate %s policy: want error, got nil", name)
		}
	}
}

const departmentPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	if claims["department"] != "burgerzaken" {
		return "Deny", "department " + claims["department"] + " may not request", nil, nil
	}
	return "Permit", "ok", nil, nil
}
`

func TestGnoVMEvaluateSeesClaims(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	for name, tc := range map[string]struct {
		claims map[string]string
		allow  bool
		reason string
	}{
		"matching claim": {claims: map[string]string{"department": "burgerzaken", "email": "a@b.nl"}, allow: true, reason: "ok"},
		"other value":    {claims: map[string]string{"department": "secretariaat"}, reason: "department secretariaat may not request"},
		"claim absent":   {claims: map[string]string{"email": "a@b.nl"}, reason: "department  may not request"},
		"no claims":      {claims: nil, reason: "department  may not request"},
		"awkward value":  {claims: map[string]string{"department": "bur\"gers\n\u00e9"}, reason: "department bur\"gers\n\u00e9 may not request"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := vm.Evaluate(departmentPolicy, Input{Resource: "laadpaal", Type: "Employee", Issuer: "did:key:x", Claims: tc.claims})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if (got.Decision == Permit) != tc.allow || got.Reason != tc.reason {
				t.Errorf("got %+v, want allow=%v reason=%q", got, tc.allow, tc.reason)
			}
		})
	}
}

func TestClaimsLiteralIsStable(t *testing.T) {
	got := claimsLiteral(map[string]string{"b": "2", "a": "1"})
	if want := `map[string]string{"a": "1", "b": "2"}`; got != want {
		t.Errorf("claimsLiteral = %s, want %s", got, want)
	}
	if got, want := claimsLiteral(nil), "map[string]string{}"; got != want {
		t.Errorf("claimsLiteral(nil) = %s, want %s", got, want)
	}
}

const userRolesPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, userRoles []string) (string, string, []map[string]string, []map[string]string) {
	for _, r := range userRoles {
		if r == "admin" {
			return "Permit", "admin", nil, nil
		}
	}
	return "Deny", "no admin role", nil, nil
}
`

func TestGnoVMUserRoles(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}
	if err := vm.Validate(userRolesPolicy); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, tt := range []struct {
		roles []string
		want  bool
	}{{[]string{"user", "admin"}, true}, {[]string{"user"}, false}, {nil, false}} {
		result, err := vm.Evaluate(userRolesPolicy, Input{Resource: "r", Type: "t", Issuer: "i", UserRoles: tt.roles})
		if err != nil {
			t.Fatalf("Evaluate %v: %v", tt.roles, err)
		}
		if (result.Decision == Permit) != tt.want {
			t.Errorf("roles %v: allow = %v (%s), want %v", tt.roles, (result.Decision == Permit), result.Reason, tt.want)
		}
	}
}

const claimsAndUserRolesPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, userRoles []string) (string, string, []map[string]string, []map[string]string) {
	if claims["department"] != "burgerzaken" {
		return "Deny", "wrong department", nil, nil
	}
	for _, r := range userRoles {
		if r == "planner" {
			return "Permit", "ok", nil, nil
		}
	}
	return "Deny", "no planner role", nil, nil
}
`

func TestGnoVMPolicyUsesClaimsAndUserRoles(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}
	tests := []struct {
		name       string
		department string
		userRoles  []string
		want       bool
		reason     string
	}{
		{"claim and role", "burgerzaken", []string{"planner"}, true, "ok"},
		{"claim, other role", "burgerzaken", []string{"reader"}, false, "no planner role"},
		{"claim, no roles", "burgerzaken", nil, false, "no planner role"},
		{"role, other claim", "financien", []string{"planner"}, false, "wrong department"},
		{"neither", "financien", nil, false, "wrong department"},
	}
	for _, tt := range tests {
		result, err := vm.Evaluate(claimsAndUserRolesPolicy, Input{
			Resource: "r", Type: "Employee", Issuer: "i", Claims: map[string]string{"department": tt.department}, UserRoles: tt.userRoles,
		})
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if (result.Decision == Permit) != tt.want || result.Reason != tt.reason {
			t.Errorf("%s: got allow=%v reason=%q, want allow=%v reason=%q", tt.name, (result.Decision == Permit), result.Reason, tt.want, tt.reason)
		}
	}
}

const obligationsPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	obligations := []map[string]string{
		{"id": "log-access", "retention-days": "30", "channel": "audit"},
		{"id": "notify-owner"},
	}
	advice := []map[string]string{{"id": "show-banner", "text": "welcome"}}
	return "Permit", "ok", obligations, advice
}
`

func TestGnoVMObligationsAndAdvice(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}
	got, err := vm.Evaluate(obligationsPolicy, Input{Resource: "r", Type: "t", Issuer: "i"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	want := Result{
		Decision: Permit, Reason: "ok",
		Obligations: []Statement{
			{ID: "log-access", Attributes: []Attribute{{ID: "channel", Value: "audit"}, {ID: "retention-days", Value: "30"}}},
			{ID: "notify-owner"},
		},
		Advice: []Statement{{ID: "show-banner", Attributes: []Attribute{{ID: "text", Value: "welcome"}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGnoVMDecisionsOtherThanPermitAndDeny(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}
	for _, d := range []Decision{NotApplicable, Indeterminate} {
		source := "package policy\n\nfunc Authorize(a, b, c string, d map[string]string, e []string) (string, string, []map[string]string, []map[string]string) { return \"" + string(d) + "\", \"r\", nil, nil }"
		got, err := vm.Evaluate(source, Input{})
		if err != nil || got.Decision != d || got.Obligations != nil || got.Advice != nil {
			t.Errorf("%s: got %+v, err %v", d, got, err)
		}
	}
}

const withOthersPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	return "Deny", "one credential is not enough", nil, nil
}

func AuthorizeWithOthers(resource string, credType string, issuer string, claims map[string]string, roles []string, others map[string]map[string]string, otherIssuers map[string]string) (string, string, []map[string]string, []map[string]string) {
	if others["Certificate"]["course"] == "c1" && otherIssuers["Certificate"] == "i2" && issuer == "i1" {
		return "Permit", "both", nil, nil
	}
	return "Deny", "not both", nil, nil
}
`

func TestGnoVMDecidesOnSeveralCredentialsWhenThePolicySaysItCan(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Validate(withOthersPolicy); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	other := map[string]Other{"Certificate": {Issuer: "i2", Claims: map[string]string{"course": "c1"}}}

	got, err := vm.Evaluate(withOthersPolicy, Input{Resource: "r", Type: "Employee", Issuer: "i1", Others: other})
	if err != nil || got.Decision != Permit {
		t.Fatalf("with others: %+v %v", got, err)
	}
	if got, _ = vm.Evaluate(withOthersPolicy, Input{Resource: "r", Type: "Employee", Issuer: "i1"}); got.Decision != Deny || got.Reason != "one credential is not enough" {
		t.Fatalf("without others: %+v", got)
	}
	if _, err := vm.Evaluate(diplomaPolicyV1, Input{Resource: "r", Type: "Employee", Issuer: "i1", Others: other}); err == nil {
		t.Fatal("a policy without AuthorizeWithOthers was evaluated on several credentials")
	}
}
