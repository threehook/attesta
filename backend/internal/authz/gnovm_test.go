package authz

import (
	"io"
	"testing"
)

const diplomaPolicyV1 = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	if resource != "diploma-vault" {
		return false, "unknown resource"
	}
	if credType != "Diploma" {
		return false, "unexpected credential type"
	}
	if issuer != "trusted-university" {
		return false, "issuer not trusted"
	}
	return true, "ok"
}
`

const diplomaPolicyV2 = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	return false, "v2 denies everything"
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
	if !result.Allow {
		t.Fatalf("Evaluate allow case: got deny, reason %q", result.Reason)
	}

	result, err = vm.Evaluate(diplomaPolicyV1, Input{Resource: "diploma-vault", Type: "Diploma", Issuer: "diploma-mill"})
	if err != nil {
		t.Fatalf("Evaluate deny case: %v", err)
	}
	if result.Allow {
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
	if err != nil || !result.Allow {
		t.Fatalf("Evaluate v1: result=%+v err=%v, want allow", result, err)
	}

	result, err = vm.Evaluate(diplomaPolicyV2, in)
	if err != nil {
		t.Fatalf("Evaluate v2: %v", err)
	}
	if result.Allow {
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

	for name, source := range map[string]string{
		"malformed":       "package policy\n\nfunc Authorize( {",
		"no Authorize":    "package policy",
		"wrong arg count": "package policy\n\nfunc Authorize(resource string) (bool, string) { return true, \"\" }",
		"no claims":       "package policy\n\nfunc Authorize(a string, b string, c string) (bool, string) { return true, \"\" }",
		"wrong arg type":  "package policy\n\nfunc Authorize(a int, b int, c int, d int) (bool, string) { return true, \"\" }",
		"wrong results":   "package policy\n\nfunc Authorize(a, b, c string, d map[string]string) bool { return true }",
		"result types":    "package policy\n\nfunc Authorize(a, b, c string, d map[string]string) (string, bool) { return \"\", true }",
		"panics":          "package policy\n\nfunc Authorize(a, b, c string, d map[string]string) (bool, string) { panic(\"boom\") }",
	} {
		if err := vm.Validate(source); err == nil {
			t.Errorf("Validate %s policy: want error, got nil", name)
		}
	}
}

const departmentPolicy = `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	if claims["department"] != "burgerzaken" {
		return false, "department " + claims["department"] + " may not request"
	}
	return true, "ok"
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
			got, err := vm.Evaluate(departmentPolicy, Input{Resource: "laadpaal", Type: "GemeenteEmployee", Issuer: "did:key:x", Claims: tc.claims})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got.Allow != tc.allow || got.Reason != tc.reason {
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
