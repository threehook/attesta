package authz

import (
	"os"
	"testing"
)

const demoIssuer = "did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"

// The example policies are run as shipped, so a change to one that breaks its decisions is caught here.
func TestExamplePolicies(t *testing.T) {
	vm := NewOPA()

	laadpaal := func(mutate func(*Input)) Input {
		in := Input{
			Resource: "request_laadpaal",
			Credentials: map[string]Credential{
				"Employee":                     {Issuer: demoIssuer, Claims: map[string]string{"department": "burgerzaken", "roles": `["laadpalen-aanvrager"]`, "email": "jerry@example.com"}},
				"Diploma Laadpalen Management": {Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}},
			},
		}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}
	signIn := func(mutate func(*Input)) Input {
		in := Input{Resource: "sign_in", Credentials: map[string]Credential{"Employee": {Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}}}}
		if mutate != nil {
			mutate(&in)
		}
		return in
	}
	employee := func(in *Input) Credential { return in.Credentials["Employee"] }

	for _, tc := range []struct {
		name, file string
		in         Input
		allow      bool
		reason     string
	}{
		{"burgerzaken employee with the diploma", "laadpalen/policies/request_laadpaal.rego", laadpaal(nil), true, "Geautoriseerd"},
		{"secretariaat employee", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["department"] = "secretariaat" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"no department disclosed", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(employee(in).Claims, "department") }), false, "Niet geautoriseerd vanwege afdeling"},
		{"department in capitals", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["department"] = "Burgerzaken" }), false, "Niet geautoriseerd vanwege afdeling"},
		{"untrusted issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Employee"] = Credential{Issuer: "did:key:z6Mkother", Claims: employee(in).Claims}
		}), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"no Employee", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(in.Credentials, "Employee") }), false, "Employee ontbreekt"},
		{"another resource", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { in.Resource = "diploma-vault" }), false, "Onbekende bron: diploma-vault"},
		{"department claim cannot stand in for the issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Employee"] = Credential{Issuer: "burgerzaken", Claims: employee(in).Claims}
		}), false, "Uitgever niet vertrouwd: burgerzaken"},

		{"no roles", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `[]` }), false, "Niet geautoriseerd vanwege rol"},
		{"roles not disclosed", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(employee(in).Claims, "roles") }), false, "Niet geautoriseerd vanwege rol"},
		{"another role", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["reader"]` }), false, "Niet geautoriseerd vanwege rol"},
		{"a role that only contains the name", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["niet-laadpalen-aanvrager"]` }), false, "Niet geautoriseerd vanwege rol"},
		{"role among others", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["reader","laadpalen-aanvrager"]` }), true, "Geautoriseerd"},
		{"the asserted roles of the application do not count", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `[]`; in.UserRoles = []string{"laadpalen-aanvrager"} }), false, "Niet geautoriseerd vanwege rol"},

		{"diploma from an untrusted issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Diploma Laadpalen Management"] = Credential{Issuer: "did:key:z6Mkother"}
		}), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"another diploma", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["DiplomaKoeienMelken"] = Credential{Issuer: demoIssuer}
			delete(in.Credentials, "Diploma Laadpalen Management")
		}), false, "Diploma Laadpalen Management ontbreekt"},
		{"the Employee alone", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(in.Credentials, "Diploma Laadpalen Management") }), false, "Diploma Laadpalen Management ontbreekt"},
		{"the diploma alone", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(in.Credentials, "Employee") }), false, "Employee ontbreekt"},

		{"sign in", "laadpalen/policies/sign_in.rego", signIn(nil), true, "Aangemeld"},
		{"sign in, untrusted issuer", "laadpalen/policies/sign_in.rego", signIn(func(in *Input) { in.Credentials["Employee"] = Credential{Issuer: "did:key:z6Mkother"} }), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"sign in, another credential type", "laadpalen/policies/sign_in.rego", signIn(func(in *Input) {
			in.Credentials = map[string]Credential{"Diploma Laadpalen Management": {Issuer: demoIssuer}}
		}), false, "Employee ontbreekt"},
		{"sign in, another resource", "laadpalen/policies/sign_in.rego", signIn(func(in *Input) { in.Resource = "request_laadpaal" }), false, "Onbekende bron: request_laadpaal"},
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
