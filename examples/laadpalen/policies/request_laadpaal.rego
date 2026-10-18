package policy

# Lets a municipality employee submit a laadpaal request. It needs two credentials of the same person, from a trusted issuer (the issuer, with its
# default seed; add other municipalities' DIDs to trusted_issuers): an Employee credential for the Burgerzaken department with the role
# laadpalen-aanvrager, and the diploma Laadpalen Management (its own credential type). Each credential's own expiry is checked before this runs, so
# an employee whose diploma has expired never gets here. The request must ask for the Employee's department and roles and for the diploma.
# input.claims holds the Employee's disclosed claims by name (roles as JSON text, a list of texts); input.others holds the diploma's issuer and claims.

trusted_issuers := {"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"}

diploma := "Diploma Laadpalen Management"

# Helper rules, so that a missing claim makes a check fail instead of making the whole condition undefined.
has_role if "laadpalen-aanvrager" in json.unmarshal(input.claims.roles)

trusted(issuer) if issuer in trusted_issuers

deny(reason) := {"decision": "Deny", "reason": reason}

decision := deny("Een Employee en het Diploma Laadpalen Management zijn nodig") if {
	count(input.others) == 0
} else := deny(concat("", ["Onbekende bron: ", input.resource])) if {
	input.resource != "request_laadpaal"
} else := deny(concat("", ["Onverwacht bewijs: ", input.type])) if {
	input.type != "Employee"
} else := deny(concat("", ["Uitgever niet vertrouwd: ", input.issuer])) if {
	not trusted(input.issuer)
} else := deny("Niet geautoriseerd vanwege afdeling") if {
	not input.claims.department == "burgerzaken"
} else := deny("Niet geautoriseerd vanwege rol") if {
	not has_role
} else := deny("Diploma Laadpalen Management ontbreekt") if {
	not input.others[diploma]
} else := deny(concat("", ["Uitgever niet vertrouwd: ", input.others[diploma].issuer])) if {
	not trusted(input.others[diploma].issuer)
} else := {"decision": "Permit", "reason": "Geautoriseerd"}
