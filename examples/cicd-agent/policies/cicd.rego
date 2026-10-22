package policy

# Lets the CI/CD agent run tests and deploy to staging. It must show a Workload credential from the trusted issuer, for the agent's own SPIFFE ID and the
# staging environment. Production needs a mandate, which this policy does not know yet, so it is always denied.
# input.credentials holds each presented credential by type, with its issuer and its disclosed claims by name.

trusted_issuers := {"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"}

agent := "spiffe://attesta.local/ns/attesta/sa/cicd-agent"

workload := input.credentials.Workload

deny(reason) := {"decision": "Deny", "reason": reason}

decision := deny("Workload ontbreekt") if {
	not workload
} else := deny(concat("", ["Uitgever niet vertrouwd: ", workload.issuer])) if {
	not workload.issuer in trusted_issuers
} else := deny(concat("", ["Systeem niet toegestaan: ", workload.claims.workload])) if {
	workload.claims.workload != agent
} else := deny("Productie vraagt een mandaat") if {
	input.resource == "deploy:prod"
} else := deny("Alleen staging is toegestaan") if {
	workload.claims.environment != "staging"
} else := {"decision": "Permit", "reason": "Toegestaan voor staging"} if {
	input.resource in {"run_tests", "deploy:staging"}
} else := deny(concat("", ["Onbekende bron: ", input.resource]))
