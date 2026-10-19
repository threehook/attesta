package authz

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func diplomaFrom(issuer string) Input {
	return Input{Resource: "diploma-vault", Credentials: map[string]Credential{"Diploma": {Issuer: issuer}}}
}

const diplomaPolicyV1 = `package policy

decision := {"decision": "Permit", "reason": "ok"} if {
	input.resource == "diploma-vault"
	input.credentials.Diploma.issuer == "trusted-university"
} else := {"decision": "Deny", "reason": "not allowed"}
`

const diplomaPolicyV2 = `package policy

decision := {"decision": "Deny", "reason": "v2 denies everything"}
`

func TestOPAEvaluate(t *testing.T) {
	vm := NewOPA()
	for issuer, want := range map[string]Decision{"trusted-university": Permit, "diploma-mill": Deny} {
		got, err := vm.Evaluate(diplomaPolicyV1, diplomaFrom(issuer))
		if err != nil || got.Decision != want {
			t.Errorf("issuer %s: got %+v, %v, want %s", issuer, got, err, want)
		}
	}
}

// Hot deployment gives the evaluator another source for the same policy; it must be used at once.
func TestOPAEvaluateAfterHotReload(t *testing.T) {
	vm := NewOPA()
	in := diplomaFrom("trusted-university")
	if got, err := vm.Evaluate(diplomaPolicyV1, in); err != nil || got.Decision != Permit {
		t.Fatalf("v1: got %+v, %v, want Permit", got, err)
	}
	if got, err := vm.Evaluate(diplomaPolicyV2, in); err != nil || got.Decision != Deny || got.Reason != "v2 denies everything" {
		t.Fatalf("v2: got %+v, %v, want Deny", got, err)
	}
}

func TestOPAValidate(t *testing.T) {
	vm := NewOPA()
	if err := vm.Validate(diplomaPolicyV1); err != nil {
		t.Fatalf("Validate well-formed policy: %v", err)
	}
	for name, source := range map[string]string{
		"malformed":        "package policy\n\ndecision := {",
		"no decision rule": "package policy\n\nallow := true",
		"other package":    "package other\n\ndecision := {\"decision\": \"Permit\"}",
		"not an object":    "package policy\n\ndecision := true",
		"unknown decision": "package policy\n\ndecision := {\"decision\": \"Allow\"}",
		"no decision":      "package policy\n\ndecision := {\"reason\": \"x\"}",
		"reason not text":  "package policy\n\ndecision := {\"decision\": \"Permit\", \"reason\": 1}",
		"no statement id":  "package policy\n\ndecision := {\"decision\": \"Permit\", \"obligations\": [{\"attributes\": {}}]}",
		"attribute number": "package policy\n\ndecision := {\"decision\": \"Permit\", \"obligations\": [{\"id\": \"a\", \"attributes\": {\"k\": 1}}]}",
		"obligations text": "package policy\n\ndecision := {\"decision\": \"Permit\", \"obligations\": \"a\"}",
		"http.send":        "package policy\n\ndecision := {\"decision\": \"Permit\", \"reason\": http.send({\"method\": \"get\", \"url\": \"http://localhost\"}).raw_body}",
		"time.now_ns":      "package policy\n\ndecision := {\"decision\": \"Permit\", \"reason\": sprintf(\"%d\", [time.now_ns()])}",
		"unknown function": "package policy\n\ndecision := {\"decision\": \"Permit\", \"reason\": nothing(1)}",
	} {
		if err := vm.Validate(source); err == nil {
			t.Errorf("Validate %s policy: want error, got nil", name)
		}
	}
}

// A rule that is undefined for the input is no decision: Indeterminate, not an error and not Permit.
func TestOPAUndefinedRuleIsIndeterminate(t *testing.T) {
	const source = `package policy

decision := {"decision": "Permit", "reason": "ok"} if input.resource == "a"
`
	vm := NewOPA()
	got, err := vm.Evaluate(source, Input{Resource: "b"})
	if err != nil || got.Decision != Indeterminate || got.Reason != reasonNoDecision {
		t.Fatalf("got %+v, %v, want Indeterminate %q", got, err, reasonNoDecision)
	}
}

const claimsPolicy = `package policy

decision := {"decision": "Permit", "reason": sprintf("%s %s %s %v", [input.credentials.Employee.claims.department, input.userRoles[0], input.credentials.Diploma.claims.level, input.credentials.Diploma.issuer])}
`

func TestOPAInput(t *testing.T) {
	got, err := NewOPA().Evaluate(claimsPolicy, Input{
		UserRoles: []string{"reader"},
		Credentials: map[string]Credential{
			"Employee": {Claims: map[string]string{"department": "burgerzaken"}},
			"Diploma":  {Issuer: "did:key:x", Claims: map[string]string{"level": "2"}},
		},
	})
	if err != nil || got.Reason != "burgerzaken reader 2 did:key:x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

const statementsPolicy = `package policy

decision := {
	"decision": "Permit", "reason": "ok",
	"obligations": [{"id": "log", "attributes": {"b": "2", "a": "1"}}],
	"advice": [{"id": "notify"}],
}
`

func TestOPAObligationsAndAdvice(t *testing.T) {
	got, err := NewOPA().Evaluate(statementsPolicy, Input{})
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Decision: Permit, Reason: "ok",
		Obligations: []Statement{{ID: "log", Attributes: []Attribute{{ID: "a", Value: "1"}, {ID: "b", Value: "2"}}}},
		Advice:      []Statement{{ID: "notify"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOPAAllFourDecisions(t *testing.T) {
	vm := NewOPA()
	for _, d := range []Decision{Permit, Deny, NotApplicable, Indeterminate} {
		got, err := vm.Evaluate(`package policy

decision := {"decision": "`+string(d)+`"}
`, Input{})
		if err != nil || got.Decision != d {
			t.Errorf("%s: got %+v, %v", d, got, err)
		}
	}
}

// A policy that cannot finish is cut off and reported as an error, which the server records as Indeterminate.
func TestOPAEvaluationIsCutOff(t *testing.T) {
	const source = `package policy

decision := {"decision": "Permit", "reason": sprintf("%d", [count([x | some x in numbers.range(1, 2000000000)])])}
`
	start := time.Now()
	if _, err := NewOPA().Evaluate(source, Input{}); err == nil {
		t.Fatal("want an error, got nil")
	}
	if time.Since(start) > 5*evalTimeout {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestOPAEvaluateConcurrently(t *testing.T) {
	vm := NewOPA()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got, err := vm.Evaluate(diplomaPolicyV1, diplomaFrom("trusted-university")); err != nil || got.Decision != Permit {
					t.Errorf("got %+v, %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkOPAEvaluate(b *testing.B) {
	vm := NewOPA()
	in := diplomaFrom("trusted-university")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := vm.Evaluate(diplomaPolicyV1, in); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestOPADiagnose(t *testing.T) {
	vm := NewOPA()
	tests := []struct {
		name, source, message string
		row, col              int
	}{
		{"accepted", diplomaPolicyV1, "", 0, 0},
		{"syntax", "package policy\n\ndecision := {\"decision\": \"Deny\" if {\n", "unexpected", 3, 0},
		{"wrong package", "package other\n\ndecision := {\"decision\": \"Deny\"}\n", "package policy", 1, 1},
		{"no decision rule", "package policy\n\nallow := true\n", "no rule decision", 1, 1},
		{"unknown input field", "package policy\n\ndecision := {\"decision\": \"Deny\"} if input.usr == \"a\"\n", "usr", 3, 0},
		{"old input field", "package policy\n\ndecision := {\"decision\": \"Deny\"} if input.type == \"Employee\"\n", "input.type", 3, 0},
		{"forbidden builtin", "package policy\n\ndecision := {\"decision\": \"Deny\"} if http.send({})\n", "http.send", 3, 0},
		{"wrong shape", "package policy\n\ndecision := 1\n", "decision", 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vm.Diagnose(tt.source)
			if tt.message == "" {
				if len(got) != 0 {
					t.Fatalf("got %+v, want none", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatal("got no diagnostics")
			}
			d := got[0]
			if !strings.Contains(d.Message, tt.message) || d.Row != tt.row || (tt.col != 0 && d.Col != tt.col) || d.EndCol <= d.Col {
				t.Fatalf("got %+v, want %q at %d:%d", d, tt.message, tt.row, tt.col)
			}
		})
	}
}
