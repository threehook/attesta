package policy

// Authorize lets a municipality employee submit a laadpaal request: an Employee credential from a trusted issuer (the issuer,
// with its default seed; add other municipalities' DIDs to trustedIssuers), for the Burgerzaken department and a laadpalen-management diploma,
// and the application must have given the employee the laadpalen-aanvrager role.
// claims holds the disclosed claims by name; the request must ask for department and diploma. The credential's own expiry is checked before this runs,
// so an employee whose diploma has expired never gets here. userRoles come from the application, not from the credential.
func Authorize(resource string, credType string, issuer string, claims map[string]string, userRoles []string) (string, string, []map[string]string, []map[string]string) {
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
	if claims["diploma"] != "laadpalen-management" {
		return "Deny", "Niet geautoriseerd vanwege opleiding", nil, nil
	}
	hasRole := false
	for _, role := range userRoles {
		if role == "laadpalen-aanvrager" {
			hasRole = true
		}
	}
	if !hasRole {
		return "Deny", "Niet geautoriseerd vanwege rol", nil, nil
	}
	return "Permit", "Geautoriseerd", nil, nil
}
