package policy

# Signs in a municipality employee: an Employee credential from a trusted issuer (the issuer, with its default seed;
# add other municipalities' DIDs to trusted_issuers). Nothing but the identity is read; what the employee may do is decided per request.

trusted_issuers := {"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"}

trusted(issuer) if issuer in trusted_issuers

deny(reason) := {"decision": "Deny", "reason": reason}

decision := deny(concat("", ["Onbekende bron: ", input.resource])) if {
	input.resource != "sign_in"
} else := deny(concat("", ["Onverwacht bewijs: ", input.type])) if {
	input.type != "Employee"
} else := deny(concat("", ["Uitgever niet vertrouwd: ", input.issuer])) if {
	not trusted(input.issuer)
} else := {"decision": "Permit", "reason": "Aangemeld"}
