package policy

// Authorize evaluates whether a disclosed credential (represented here by the
// public signals a ZK proof already verified) satisfies the requirements for
// accessing resource. It returns an allow/deny decision and a human-readable
// reason.
func Authorize(resource string, issuer string) (bool, string) {
	trustedIssuers := map[string]bool{
		"trusted-university": true,
	}

	if resource != "diploma-vault" {
		return false, "unknown resource: " + resource
	}
	if !trustedIssuers[issuer] {
		return false, "issuer not trusted: " + issuer
	}
	return true, "issuer is trusted for resource " + resource
}
