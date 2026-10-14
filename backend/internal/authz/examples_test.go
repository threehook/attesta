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
			Resource: "request_laadpaal", Type: "Employee", Issuer: demoIssuer,
			Claims: map[string]string{"department": "burgerzaken", "roles": `["laadpalen-aanvrager"]`, "email": "jerry@example.com"},
			Others: map[string]Other{"Diploma Laadpalen Management": {Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}}},
		}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}
	signIn := func(mutate func(*Input)) Input {
		in := Input{Resource: "sign_in", Type: "Employee", Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}}
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
		{"burgerzaken employee with the diploma", "laadpalen/policies/request_laadpaal.gno", laadpaal(nil), true, "Geautoriseerd"},
		{"secretariaat employee", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["department"] = "secretariaat" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"no department disclosed", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { delete(in.Claims, "department") }), false, "Niet geautoriseerd vanwege afdeling"},
		{"department in capitals", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["department"] = "Burgerzaken" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"untrusted issuer", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Issuer = "did:key:z6Mkother" }), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"another credential type", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Type = "Diploma Laadpalen Management" }), false, "Onverwacht bewijs: Diploma Laadpalen Management"},
		{"another resource", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Resource = "diploma-vault" }), false, "Onbekende bron: diploma-vault"},
		{"department claim cannot stand in for the issuer", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Issuer = "burgerzaken" }), false, "Uitgever niet vertrouwd: burgerzaken"},

		{"no roles", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["roles"] = `[]` }), false, "Niet geautoriseerd vanwege rol"},
		{"roles not disclosed", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { delete(in.Claims, "roles") }), false, "Niet geautoriseerd vanwege rol"},
		{"another role", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["roles"] = `["reader"]` }), false, "Niet geautoriseerd vanwege rol"},
		{"a role that only contains the name", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["roles"] = `["niet-laadpalen-aanvrager"]` }), false, "Niet geautoriseerd vanwege rol"},
		{"role among others", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["roles"] = `["reader","laadpalen-aanvrager"]` }), true, "Geautoriseerd"},
		{"the asserted roles of the application do not count", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Claims["roles"] = `[]`; in.UserRoles = []string{"laadpalen-aanvrager"} }), false, "Niet geautoriseerd vanwege rol"},

		{"diploma from an untrusted issuer", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Others["Diploma Laadpalen Management"] = Other{Issuer: "did:key:z6Mkother"} }), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"another diploma", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Others = map[string]Other{"DiplomaKoeienMelken": {Issuer: demoIssuer}} }), false, "Diploma Laadpalen Management ontbreekt"},
		{"the Employee alone", "laadpalen/policies/request_laadpaal.gno", laadpaal(func(in *Input) { in.Others = nil }), false, "Een Employee en het Diploma Laadpalen Management zijn nodig"},

		{"sign in", "laadpalen/policies/sign_in.gno", signIn(nil), true, "Aangemeld"},
		{"sign in, untrusted issuer", "laadpalen/policies/sign_in.gno", signIn(func(in *Input) { in.Issuer = "did:key:z6Mkother" }), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"sign in, another credential type", "laadpalen/policies/sign_in.gno", signIn(func(in *Input) { in.Type = "Diploma Laadpalen Management" }), false, "Onverwacht bewijs: Diploma Laadpalen Management"},
		{"sign in, another resource", "laadpalen/policies/sign_in.gno", signIn(func(in *Input) { in.Resource = "request_laadpaal" }), false, "Onbekende bron: request_laadpaal"},
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
			if (got.Decision == Permit) != tc.allow || got.Reason != tc.reason {
				t.Errorf("got allow=%v reason=%q, want allow=%v reason=%q", got.Decision == Permit, got.Reason, tc.allow, tc.reason)
			}
		})
	}
}
