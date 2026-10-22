package policy

# Lets the CI/CD agent run tests and deploy. It must show a Workload credential from the trusted issuer, for the agent's own SPIFFE ID.
# Staging needs the environment `staging` and nothing else. Production, a resource `deploy:prod:<version>`, also needs a Mandate from the same issuer:
# its `resource` pattern (a glob, ":" separates the parts) must cover the resource and name production, and two different people must have approved it.
# input.credentials holds each presented credential by type, with its issuer and its disclosed claims by name (the approvers as JSON text, a list of texts).

trusted_issuers := {"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"}

agent := "spiffe://attesta.local/ns/attesta/sa/cicd-agent"

approvals_needed := 2

workload := input.credentials.Workload

mandate := input.credentials.Mandate

production if glob.match("deploy:prod:*", [":"], input.resource)

staging if glob.match("deploy:staging:*", [":"], input.resource)

approvers := {a | some a in json.unmarshal(mandate.claims.approvers)}

deny(reason) := {"decision": "Deny", "reason": reason}

decision := deny("Workload ontbreekt") if {
	not workload
} else := deny(concat("", ["Uitgever niet vertrouwd: ", workload.issuer])) if {
	not workload.issuer in trusted_issuers
} else := deny(concat("", ["Systeem niet toegestaan: ", workload.claims.workload])) if {
	workload.claims.workload != agent
} else := deny("Productie vraagt een mandaat") if {
	production
	not mandate
} else := deny(concat("", ["Uitgever van het mandaat niet vertrouwd: ", mandate.issuer])) if {
	production
	not mandate.issuer in trusted_issuers
} else := deny("Het mandaat geldt niet voor productie") if {
	production
	not startswith(mandate.claims.resource, "deploy:prod:")
} else := deny(concat("", ["Het mandaat geldt niet voor ", input.resource])) if {
	production
	not glob.match(mandate.claims.resource, [":"], input.resource)
} else := deny("Productie vraagt twee verschillende goedkeurders") if {
	production
	count(approvers) < approvals_needed
} else := {"decision": "Permit", "reason": concat("", ["Toegestaan voor productie, goedgekeurd door ", concat(", ", sort(approvers))])} if {
	production
} else := deny("Alleen staging is toegestaan") if {
	workload.claims.environment != "staging"
} else := {"decision": "Permit", "reason": "Toegestaan voor staging"} if {
	input.resource == "run_tests"
} else := {"decision": "Permit", "reason": "Toegestaan voor staging"} if {
	staging
} else := deny(concat("", ["Onbekende bron: ", input.resource]))
