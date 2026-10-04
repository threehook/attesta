// Package config loads zk-puoi's runtime configuration from the environment.
package config

import "os"

// Config holds zk-puoi's runtime configuration; see FromEnv.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// PoliciesDir is loaded into the policy store at startup when set; empty starts with no policies, to be hot-deployed via POST /admin/policies.
	PoliciesDir string
	// VerificationKeyPath is a snarkjs-exported verification_key.json for the circuit proofs are checked against. The default assumes the working
	// directory is backend/ (as `go run ./cmd/zk-puoi` leaves it) and client-lib is checked out alongside it; the Docker image overrides this to a
	// path baked into the container.
	VerificationKeyPath string
	// RegistryPath is the demo credential registry (client-lib/circuits/diploma_membership/registry.mjs's output) whose root a proof's disclosed
	// root public signal must match. Same default/override pattern as VerificationKeyPath.
	RegistryPath string
	// JWTSecret signs and verifies the mock login's JWTs. Fine as a static shared secret for this MVP; revisit (e.g. per-environment secret,
	// rotation) before this is ever exposed beyond local/dev use.
	JWTSecret string
	// AdminToken gates POST /admin/policies. Same caveat as JWTSecret.
	AdminToken string
	// CORSOrigins is the comma-separated browser-origin allowlist passed to httpapi.Server; see withCORS.
	CORSOrigins string
}

func FromEnv() Config {
	return Config{
		Addr:                getenv("ZKPUOI_ADDR", ":8080"),
		PoliciesDir:         getenv("ZKPUOI_POLICIES_DIR", ""),
		JWTSecret:           getenv("ZKPUOI_JWT_SECRET", "dev-only-insecure-secret"),
		AdminToken:          getenv("ZKPUOI_ADMIN_TOKEN", "dev-only-insecure-admin-token"),
		VerificationKeyPath: getenv("ZKPUOI_VERIFICATION_KEY", "../client-lib/circuits/diploma_membership/build/verification_key.json"),
		RegistryPath:        getenv("ZKPUOI_REGISTRY_PATH", "../client-lib/circuits/diploma_membership/build/registry.json"),
		CORSOrigins:         getenv("ZKPUOI_CORS_ORIGINS", "http://localhost:5173"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
