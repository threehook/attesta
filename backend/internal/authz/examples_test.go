package authz

import (
	"os"
	"testing"
)

const demoIssuer = "did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"

// The example policies are run as shipped, so a change to one that breaks its decisions is caught here.
// A denial that people can undo says so with the advice request_approval; one that they cannot does not.
func TestCICDPolicyAdvisesOnlyWhereAnApprovalCouldHelp(t *testing.T) {
	source, err := os.ReadFile("../../../examples/cicd-agent/policies/cicd.rego")
	if err != nil {
		t.Fatal(err)
	}

	agent := "spiffe://attesta.local/ns/attesta/sa/cicd-agent"
	workload := Credential{Issuer: demoIssuer, Claims: map[string]string{"workload": agent, "owner": "platform", "environment": "staging"}}
	mandate := func(resource, approvers string) Credential {
		return Credential{Issuer: demoIssuer, Claims: map[string]string{"workload": agent, "resource": resource, "approvers": approvers}}
	}

	both := `["anna@corp.test","bob@corp.test"]`
	for name, tc := range map[string]struct {
		in     Input
		advice bool
	}{
		"no mandate": {Input{Resource: "deploy:prod:v2.0.0", Credentials: map[string]Credential{"Workload": workload}}, true},
		"a mandate for another version": {Input{Resource: "deploy:prod:v2.0.0", Credentials: map[string]Credential{
			"Workload": workload,
			"Mandate":  mandate("deploy:prod:v1.*", both),
		}}, true},
		"a mandate with one approver": {Input{Resource: "deploy:prod:v1.4.2", Credentials: map[string]Credential{
			"Workload": workload,
			"Mandate":  mandate("deploy:prod:v1.*", `["anna@corp.test"]`),
		}}, false},
		"another system": {Input{Resource: "deploy:prod:v2.0.0", Credentials: map[string]Credential{"Workload": {
			Issuer: demoIssuer,
			Claims: map[string]string{"workload": "spiffe://attesta.local/ns/x/sa/y"},
		}}}, false},
		"an unknown resource": {Input{Resource: "format-disk", Credentials: map[string]Credential{"Workload": workload}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := NewOPA().Evaluate(string(source), tc.in)
			if err != nil || got.Decision != Deny {
				t.Fatalf("got %+v, %v, want Deny", got, err)
			}

			if advised := len(got.Advice) == 1 && got.Advice[0].ID == "request_approval"; advised != tc.advice {
				t.Errorf("advice = %+v, want request_approval: %v", got.Advice, tc.advice)
			}
		})
	}
}

func TestExamplePolicies(t *testing.T) {
	vm := NewOPA()

	laadpaal := func(mutate func(*Input)) Input {
		in := Input{
			Resource: "request_laadpaal",
			Credentials: map[string]Credential{
				"Employee": {
					Issuer: demoIssuer,
					Claims: map[string]string{"department": "burgerzaken", "roles": `["laadpalen-aanvrager"]`, "email": "jerry@example.com"},
				},
				"Diploma Laadpalen Management": {Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}},
			},
		}
		if mutate != nil {
			mutate(&in)
		}

		return in
	}
	signIn := func(mutate func(*Input)) Input {
		in := Input{
			Resource:    "sign_in",
			Credentials: map[string]Credential{"Employee": {Issuer: demoIssuer, Claims: map[string]string{"email": "jerry@example.com"}}},
		}
		if mutate != nil {
			mutate(&in)
		}

		return in
	}

	const agent = "spiffe://attesta.local/ns/attesta/sa/cicd-agent"

	cicd := func(resource string, mutate func(*Input)) Input {
		in := Input{
			Resource: resource,
			Credentials: map[string]Credential{
				"Workload": {Issuer: demoIssuer, Claims: map[string]string{"workload": agent, "owner": "platform", "environment": "staging"}},
			},
		}
		if mutate != nil {
			mutate(&in)
		}

		return in
	}
	mandated := func(resource, approvers string) func(*Input) {
		return func(in *Input) {
			in.Credentials["Mandate"] = Credential{
				Issuer: demoIssuer,
				Claims: map[string]string{"workload": agent, "resource": resource, "approvers": approvers},
			}
		}
	}
	approved := func(resource, approvers string) func(*Input) {
		return func(in *Input) {
			in.Credentials["Approval"] = Credential{
				Issuer: demoIssuer,
				Claims: map[string]string{"workload": agent, "resource": resource, "approvers": approvers},
			}
		}
	}

	const prod = "cicd-agent/policies/cicd.rego"

	employee := func(in *Input) Credential { return in.Credentials["Employee"] }

	for _, tc := range []struct {
		name, file string
		in         Input
		allow      bool
		reason     string
	}{
		{"burgerzaken employee with the diploma", "laadpalen/policies/request_laadpaal.rego", laadpaal(nil), true, "Geautoriseerd"},
		{
			"secretariaat employee", "laadpalen/policies/request_laadpaal.rego",
			laadpaal(func(in *Input) { employee(in).Claims["department"] = "secretariaat" }), false, "Niet geautoriseerd vanwege afdeling",
		},
		{
			"no department disclosed", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
				delete(employee(in).Claims,
					"department")
			}),
			false, "Niet geautoriseerd vanwege afdeling",
		},
		{
			"department in capitals", "laadpalen/policies/request_laadpaal.rego",
			laadpaal(func(in *Input) { employee(in).Claims["department"] = "Burgerzaken" }), false, "Niet geautoriseerd vanwege afdeling",
		},
		{"untrusted issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Employee"] = Credential{Issuer: "did:key:z6Mkother", Claims: employee(in).Claims}
		}), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{
			"no Employee", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(in.Credentials, "Employee") }), false,
			"Employee ontbreekt",
		},
		{
			"another resource", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { in.Resource = "diploma-vault" }), false,
			"Onbekende bron: diploma-vault",
		},
		{"department claim cannot stand in for the issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Employee"] = Credential{Issuer: "burgerzaken", Claims: employee(in).Claims}
		}), false, "Uitgever niet vertrouwd: burgerzaken"},

		{
			"no roles", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `[]` }), false,
			"Niet geautoriseerd vanwege rol",
		},
		{
			"roles not disclosed", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(employee(in).Claims, "roles") }),
			false,
			"Niet geautoriseerd vanwege rol",
		},
		{
			"another role", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["reader"]` }),
			false,
			"Niet geautoriseerd vanwege rol",
		},
		{
			"a role that only contains the name", "laadpalen/policies/request_laadpaal.rego",
			laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["niet-laadpalen-aanvrager"]` }), false, "Niet geautoriseerd vanwege rol",
		},
		{
			"role among others", "laadpalen/policies/request_laadpaal.rego",
			laadpaal(func(in *Input) { employee(in).Claims["roles"] = `["reader","laadpalen-aanvrager"]` }), true, "Geautoriseerd",
		},
		{
			"the asserted roles of the application do not count", "laadpalen/policies/request_laadpaal.rego",
			laadpaal(func(in *Input) { employee(in).Claims["roles"] = `[]`; in.UserRoles = []string{"laadpalen-aanvrager"} }),
			false, "Niet geautoriseerd vanwege rol",
		},

		{"diploma from an untrusted issuer", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["Diploma Laadpalen Management"] = Credential{Issuer: "did:key:z6Mkother"}
		}), false, "Uitgever niet vertrouwd: did:key:z6Mkother"},
		{"another diploma", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			in.Credentials["DiplomaKoeienMelken"] = Credential{Issuer: demoIssuer}
			delete(in.Credentials, "Diploma Laadpalen Management")
		}), false, "Diploma Laadpalen Management ontbreekt"},
		{"the Employee alone", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) {
			delete(in.Credentials,
				"Diploma Laadpalen Management")
		}), false, "Diploma Laadpalen Management ontbreekt"},
		{
			"the diploma alone", "laadpalen/policies/request_laadpaal.rego", laadpaal(func(in *Input) { delete(in.Credentials, "Employee") }), false,
			"Employee ontbreekt",
		},

		{"cicd, tests", prod, cicd("run_tests", nil), true, "Toegestaan voor staging"},
		{"cicd, staging deploy", prod, cicd("deploy:staging:v1.4.2", nil), true, "Toegestaan voor staging"},
		{
			"cicd, another system", prod,
			cicd("run_tests", func(in *Input) { in.Credentials["Workload"].Claims["workload"] = "spiffe://attesta.local/ns/x/sa/y" }),
			false, "Systeem niet toegestaan: spiffe://attesta.local/ns/x/sa/y",
		},
		{"cicd, production without a mandate", prod, cicd("deploy:prod:v1.4.2", nil), false, "Productie vraagt een mandaat"},
		{
			"cicd, production with two approvers", prod, cicd("deploy:prod:v1.4.2", mandated("deploy:prod:v1.4.2",
				`["anna@corp.test","bob@corp.test"]`)),
			true, "Toegestaan voor productie, goedgekeurd door anna@corp.test, bob@corp.test",
		},
		{
			"cicd, production by a pattern", prod, cicd("deploy:prod:v1.4.2", mandated("deploy:prod:v1.4.*", `["anna@corp.test","bob@corp.test"]`)),
			true,
			"Toegestaan voor productie, goedgekeurd door anna@corp.test, bob@corp.test",
		},
		{
			"cicd, production with one approver", prod, cicd("deploy:prod:v1.4.2", mandated("deploy:prod:v1.4.2", `["anna@corp.test"]`)), false,
			"Productie vraagt twee verschillende goedkeurders",
		},
		{
			"cicd, one person twice", prod, cicd("deploy:prod:v1.4.2", mandated("deploy:prod:v1.4.2", `["anna@corp.test","anna@corp.test"]`)), false,
			"Productie vraagt twee verschillende goedkeurders",
		},
		{
			"cicd, another version", prod, cicd("deploy:prod:v2.0.0", mandated("deploy:prod:v1.4.2", `["anna@corp.test","bob@corp.test"]`)), false,
			"Het mandaat geldt niet voor deploy:prod:v2.0.0",
		},
		{
			"cicd, a mandate for staging", prod, cicd("deploy:prod:v1.4.2", mandated("deploy:staging:*", `["anna@corp.test","bob@corp.test"]`)),
			false,
			"Het mandaat geldt niet voor productie",
		},
		{
			"cicd, a resource with an extra part is unknown", prod, cicd("deploy:prod:v1:x", mandated("deploy:prod:*",
				`["anna@corp.test","bob@corp.test"]`)),
			false, "Onbekende bron: deploy:prod:v1:x",
		},
		{"cicd, a mandate from an untrusted issuer", prod, cicd("deploy:prod:v1.4.2", func(in *Input) {
			mandated("deploy:prod:v1.4.2", `["anna@corp.test","bob@corp.test"]`)(in)
			in.Credentials["Mandate"] = Credential{Issuer: "did:key:z6Mkother", Claims: in.Credentials["Mandate"].Claims}
		}), false, "Uitgever van het mandaat niet vertrouwd: did:key:z6Mkother"},
		{"cicd, production with an approval of two", prod, cicd("deploy:prod:v2.0.0", approved("deploy:prod:v2.0.0",
			`["anna@corp.test","bob@corp.test"]`)), true, "Toegestaan voor productie met eenmalige goedkeuring van anna@corp.test, bob@corp.test"},
		{
			"cicd, an approval of one", prod, cicd("deploy:prod:v2.0.0", approved("deploy:prod:v2.0.0", `["anna@corp.test"]`)), false,
			"Productie vraagt een mandaat",
		},
		{
			"cicd, an approval for another version", prod, cicd("deploy:prod:v2.0.0", approved("deploy:prod:v2.0.1",
				`["anna@corp.test","bob@corp.test"]`)),
			false, "Productie vraagt een mandaat",
		},
		{"cicd, an approval does not stretch to a pattern", prod, cicd("deploy:prod:v2.0.1", approved("deploy:prod:v2.0.*",
			`["anna@corp.test","bob@corp.test"]`)), false, "Productie vraagt een mandaat"},
		{"cicd, an approval from an untrusted issuer", prod, cicd("deploy:prod:v2.0.0", func(in *Input) {
			approved("deploy:prod:v2.0.0", `["anna@corp.test","bob@corp.test"]`)(in)
			in.Credentials["Approval"] = Credential{Issuer: "did:key:z6Mkother", Claims: in.Credentials["Approval"].Claims}
		}), false, "Productie vraagt een mandaat"},
		{
			"cicd, a mandate does not widen staging", prod, cicd("deploy:other:v1", mandated("deploy:prod:*", `["anna@corp.test","bob@corp.test"]`)),
			false,
			"Onbekende bron: deploy:other:v1",
		},

		{"sign in", "laadpalen/policies/sign_in.rego", signIn(nil), true, "Aangemeld"},
		{
			"sign in, untrusted issuer", "laadpalen/policies/sign_in.rego",
			signIn(func(in *Input) { in.Credentials["Employee"] = Credential{Issuer: "did:key:z6Mkother"} }),
			false, "Uitgever niet vertrouwd: did:key:z6Mkother",
		},
		{"sign in, another credential type", "laadpalen/policies/sign_in.rego", signIn(func(in *Input) {
			in.Credentials = map[string]Credential{"Diploma Laadpalen Management": {Issuer: demoIssuer}}
		}), false, "Employee ontbreekt"},
		{
			"sign in, another resource", "laadpalen/policies/sign_in.rego", signIn(func(in *Input) { in.Resource = "request_laadpaal" }), false,
			"Onbekende bron: request_laadpaal",
		},
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
