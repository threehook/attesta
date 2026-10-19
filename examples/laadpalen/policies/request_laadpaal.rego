package policy

# Lets a municipality employee submit a laadpaal request. It needs two credentials of the same person, from a trusted issuer (the issuer, with its
# default seed; add other municipalities' DIDs to trusted_issuers): an Employee credential for the Burgerzaken department with the role
# laadpalen-aanvrager, and the diploma Laadpalen Management (its own credential type). Each credential's own expiry is checked before this runs, so
# an employee whose diploma has expired never gets here. The request must ask for the Employee's department and roles and for the diploma.
# input.credentials holds each presented credential by type, with its issuer and its disclosed claims by name (the Employee's roles as JSON text, a list of texts).

trusted_issuers := {"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"}

employee := "Employee"

diploma := "Diploma Laadpalen Management"

# Helper rules, so that a missing claim makes a check fail instead of making the whole condition undefined.
has_role if "laadpalen-aanvrager" in json.unmarshal(input.credentials[employee].claims.roles)

trusted(issuer) if issuer in trusted_issuers

deny(reason) := {"decision": "Deny", "reason": reason}

decision := deny(concat("", ["Onbekende bron: ", input.resource])) if {
	input.resource != "request_laadpaal"
} else := deny("Employee ontbreekt") if {
	not input.credentials[employee]
} else := deny(concat("", ["Uitgever niet vertrouwd: ", input.credentials[employee].issuer])) if {
	not trusted(input.credentials[employee].issuer)
} else := deny("Niet geautoriseerd vanwege afdeling") if {
	not input.credentials[employee].claims.department == "burgerzaken"
} else := deny("Niet geautoriseerd vanwege rol") if {
	not has_role
} else := deny("Diploma Laadpalen Management ontbreekt") if {
	not input.credentials[diploma]
} else := deny(concat("", ["Uitgever niet vertrouwd: ", input.credentials[diploma].issuer])) if {
	not trusted(input.credentials[diploma].issuer)
} else := {"decision": "Permit", "reason": "Geautoriseerd"}
