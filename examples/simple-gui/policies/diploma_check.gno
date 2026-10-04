package policy

// Authorize allows access to the diploma vault for a Diploma credential issued by the demo issuer (examples/issuer, with its default seed).
// issuer is the issuer's DID; add the DIDs of other issuers you trust to trustedIssuers.
// claims holds the disclosed claims by name; this policy does not use them.
func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	trustedIssuers := map[string]bool{
		"did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA": true,
	}

	if resource != "diploma-vault" {
		return false, "unknown resource: " + resource
	}
	if credType != "Diploma" {
		return false, "unexpected credential type: " + credType
	}
	if !trustedIssuers[issuer] {
		return false, "issuer not trusted: " + issuer
	}
	return true, "credential accepted for resource " + resource
}
