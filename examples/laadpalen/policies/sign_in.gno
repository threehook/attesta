package policy

// Authorize signs in a municipality employee: an Employee credential from a trusted issuer (the issuer, with its default seed;
// add other municipalities' DIDs to trustedIssuers). Nothing but the identity is read; what the employee may do is decided per request.
func Authorize(resource string, credType string, issuer string, claims map[string]string, roles []string) (string, string, []map[string]string, []map[string]string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "sign_in" {
		return "Deny", "Onbekende bron: " + resource, nil, nil
	}
	if credType != "Employee" {
		return "Deny", "Onverwacht bewijs: " + credType, nil, nil
	}
	if !trustedIssuers[issuer] {
		return "Deny", "Uitgever niet vertrouwd: " + issuer, nil, nil
	}
	return "Permit", "Aangemeld", nil, nil
}
