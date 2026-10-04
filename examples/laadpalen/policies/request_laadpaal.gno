package policy

// Authorize lets a municipality employee submit a laadpaal request: a GemeenteEmployee credential from a trusted issuer (the demo issuer, examples/issuer,
// with its default seed; add other municipalities' DIDs to trustedIssuers), for the Burgerzaken department and a laadpalen-management diploma.
// claims holds the disclosed claims by name; the request must ask for department and diploma. The credential's own expiry is checked before this runs,
// so an employee whose diploma has expired never gets here.
func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "request_laadpaal" {
		return false, "Onbekende bron: " + resource
	}
	if credType != "GemeenteEmployee" {
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
	return true, "Geautoriseerd"
}
