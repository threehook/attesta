package authz

import (
	"io"
	"os"
	"testing"
)

const demoIssuer = "did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"

// The example policies are run as shipped, so a change to one that breaks its decisions is caught here.
func TestExamplePolicies(t *testing.T) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		t.Fatalf("NewGnoVM: %v", err)
	}

	laadpaal := func(mutate func(*Input)) Input {
		in := Input{
			Resource: "request_laadpaal", Type: "GemeenteEmployee", Issuer: demoIssuer,
			Claims: map[string]string{"department": "burgerzaken", "diploma": "laadpalen-management", "email": "jerry@example.com"},
		}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}
	diploma := func(mutate func(*Input)) Input {
		in := Input{Resource: "diploma-vault", Type: "Diploma", Issuer: demoIssuer, Claims: map[string]string{"email": "ada@example.com"}}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}

	for _, tc := range []struct {
		name, file string
		in         Input
		allow      bool
		reason     string
	}{
		{"burgerzaken employee", "laadpalen/policies/request_laadpaal.gno", laadpaal(nil), true, "Geautoriseerd"},
		{"secretariaat employee", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["department"] = "secretariaat" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"no department disclosed", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { delete(in.Claims, "department") }), false, "Niet geautoriseerd vanwege afdeling"},
		{"department in capitals", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["department"] = "Burgerzaken" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"another diploma", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["diploma"] = "boekhouden" }), false, "Niet geautoriseerd vanwege opleiding"},
		{"no diploma disclosed", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { delete(in.Claims, "diploma") }), false, "Niet geautoriseerd vanwege opleiding"},
		{"untrusted issuer", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Issuer = "did:key:z6Mkother" }), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"another credential type", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Type = "Diploma" }), false, "Onverwacht bewijs: Diploma"},
		{"another resource", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Resource = "diploma-vault" }), false, "Onbekende bron: diploma-vault"},
		{"department claim cannot stand in for the issuer", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Issuer = "burgerzaken" }), false, "Uitgever niet vertrouwd: burgerzaken"},

		{"diploma vault", "simple-gui/policies/diploma_check.gno", diploma(nil), true, "credential accepted for resource diploma-vault"},
		{"diploma vault, untrusted issuer", "simple-gui/policies/diploma_check.gno", diploma(func(in *Input) { in.Issuer = "did:key:z6Mkother" }), false, "issuer not trusted: did:key:z6Mkother"},
		{"diploma vault, wrong type", "simple-gui/policies/diploma_check.gno", diploma(func(in *Input) { in.Type = "GemeenteEmployee" }), false, "unexpected credential type: GemeenteEmployee"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile("../../../examples/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if err := vm.Validate(string(source)); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			got, err := vm.Evaluate(string(source), tc.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got.Allow != tc.allow || got.Reason != tc.reason {
				t.Errorf("got allow=%v reason=%q, want allow=%v reason=%q", got.Allow, got.Reason, tc.allow, tc.reason)
			}
		})
	}
}
