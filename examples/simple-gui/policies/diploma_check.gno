package policy

// Authorize evaluates whether a disclosed credential (credType, issuer — both disclosed by a ZK proof already verified against the real credential
// registry, see internal/authz.DecodePublicSignals) satisfies the requirements for accessing resource. It returns an allow/deny decision and a
// human-readable reason.
func Authorize(resource string, credType string, issuer string) (bool, string) {
	trustedIssuers := map[string]bool{
		"trusted-university": true,
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
