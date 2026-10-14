package policy

// AuthorizeWithOthers lets a municipality employee submit a laadpaal request. It needs two credentials of the same person, from a trusted issuer (the
// issuer, with its default seed; add other municipalities' DIDs to trustedIssuers): an Employee credential for the Burgerzaken department with the
// role laadpalen-aanvrager, and the diploma Laadpalen Management (its own credential type). Each credential's own expiry is checked before this runs, so an
// employee whose diploma has expired never gets here. The request must ask for the Employee's department and roles and for the diploma.
// claims holds the Employee's disclosed claims by name (roles as JSON text, a list of texts); others and otherIssuers hold the diploma's, by type.
func AuthorizeWithOthers(resource string, credType string, issuer string, claims map[string]string, userRoles []string, others map[string]map[string]string, otherIssuers map[string]string) (string, string, []map[string]string, []map[string]string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "request_laadpaal" {
		return "Deny", "Onbekende bron: " + resource, nil, nil
	}
	if credType != "Employee" {
		return "Deny", "Onverwacht bewijs: " + credType, nil, nil
	}
	if !trustedIssuers[issuer] {
		return "Deny", "Uitgever niet vertrouwd: " + issuer, nil, nil
	}
	if claims["department"] != "burgerzaken" {
		return "Deny", "Niet geautoriseerd vanwege afdeling", nil, nil
	}
	if !contains(claims["roles"], `"laadpalen-aanvrager"`) {
		return "Deny", "Niet geautoriseerd vanwege rol", nil, nil
	}
	if _, ok := others["Diploma Laadpalen Management"]; !ok {
		return "Deny", "Diploma Laadpalen Management ontbreekt", nil, nil
	}
	if !trustedIssuers[otherIssuers["Diploma Laadpalen Management"]] {
		return "Deny", "Uitgever niet vertrouwd: " + otherIssuers["Diploma Laadpalen Management"], nil, nil
	}
	return "Permit", "Geautoriseerd", nil, nil
}

// Authorize is what a request for one credential reaches; this policy needs two.
func Authorize(resource string, credType string, issuer string, claims map[string]string, userRoles []string) (string, string, []map[string]string, []map[string]string) {
	return "Deny", "Een Employee en het Diploma Laadpalen Management zijn nodig", nil, nil
}

// contains reports whether part is in text; a policy has no strings package.
func contains(text string, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
