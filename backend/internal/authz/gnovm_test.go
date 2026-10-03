package authz

import (
	"io"
	"testing"
)

const diplomaPolicyV1 = `package policy

func Authorize(resource string, issuer string) (bool, string) {
	if resource != "diploma-vault" {
		return false, "unknown resource"
	}
	if issuer != "trusted-university" {
		return false, "issuer not trusted"
	}
	return true, "ok"
}
`

const diplomaPolicyV2 = `package policy

func Authorize(resource string, issuer string) (bool, string) {
	return false, "v2 denies everything"
}
`

func TestGnoVMEvaluate(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	result, err := vm.Evaluate(diplomaPolicyV1, Input{Resource: "diploma-vault", Issuer: "trusted-university"})
	if err != nil {
		t.Fatalf("Evaluate allow case: %v", err)
	}
	if !result.Allow {
		t.Fatalf("Evaluate allow case: got deny, reason %q", result.Reason)
	}

	result, err = vm.Evaluate(diplomaPolicyV1, Input{Resource: "diploma-vault", Issuer: "diploma-mill"})
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

	in := Input{Resource: "diploma-vault", Issuer: "trusted-university"}

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

	if err := vm.Validate("package policy\n\nfunc Authorize( {"); err == nil {
		t.Fatal("Validate malformed policy: want error, got nil")
	}
}
