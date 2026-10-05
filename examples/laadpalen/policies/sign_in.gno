package policy

// Authorize signs in a municipality employee: an Employee credential from a trusted issuer (the demo issuer, examples/issuer, with its default seed;
// add other municipalities' DIDs to trustedIssuers). Nothing but the identity is read; what the employee may do is decided per request.
func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "sign_in" {
		return false, "Onbekende bron: " + resource
	}
	if credType != "Employee" {
		return false, "Onverwacht bewijs: " + credType
	}
	if !trustedIssuers[issuer] {
		return false, "Uitgever niet vertrouwd: " + issuer
	}
	return true, "Aangemeld"
}
