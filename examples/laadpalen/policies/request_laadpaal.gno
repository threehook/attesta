package policy

// Authorize lets a municipality employee submit a laadpaal request: an Employee credential from a trusted issuer (the demo issuer, examples/issuer,
// with its default seed; add other municipalities' DIDs to trustedIssuers), for the Burgerzaken department and a laadpalen-management diploma,
// and the application must have given the employee the laadpalen-aanvrager role.
// claims holds the disclosed claims by name; the request must ask for department and diploma. The credential's own expiry is checked before this runs,
// so an employee whose diploma has expired never gets here. userRoles come from the application, not from the credential.
func AuthorizeWithUserRoles(resource string, credType string, issuer string, claims map[string]string, userRoles []string) (bool, string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "request_laadpaal" {
		return false, "Onbekende bron: " + resource
	}
	if credType != "Employee" {
		return false, "Onverwacht bewijs: " + credType
	}
	if !trustedIssuers[issuer] {
		return false, "Uitgever niet vertrouwd: " + issuer
	}
	if claims["department"] != "burgerzaken" {
		return false, "Niet geautoriseerd vanwege afdeling"
	}
	if claims["diploma"] != "laadpalen-management" {
		return false, "Niet geautoriseerd vanwege opleiding"
	}
	hasRole := false
	for _, role := range userRoles {
		if role == "laadpalen-aanvrager" {
			hasRole = true
		}
	}
	if !hasRole {
		return false, "Niet geautoriseerd vanwege rol"
	}
	return true, "Geautoriseerd"
}
