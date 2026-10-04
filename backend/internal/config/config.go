// Package config loads attesta's runtime configuration from the environment.
package config

import "os"

// Config holds attesta's runtime configuration; see FromEnv.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// PoliciesDir is loaded into the policy store at startup when set; empty starts with no policies, to be hot-deployed via POST /admin/policies.
	PoliciesDir string
	// AdminToken gates POST /admin/policies. A static shared secret, fine for local development; revisit before this is exposed beyond it.
	AdminToken string
	// PublicURL is this backend's externally reachable base URL; wallets are told to post their presentations to it.
	PublicURL string
	// CORSOrigins is the comma-separated browser-origin allowlist passed to httpapi.Server; see withCORS.
	CORSOrigins string
}

func FromEnv() Config {
	return Config{
		Addr:        getenv("ATTESTA_ADDR", ":8080"),
		PoliciesDir: getenv("ATTESTA_POLICIES_DIR", ""),
		AdminToken:  getenv("ATTESTA_ADMIN_TOKEN", "dev-only-insecure-admin-token"),
		PublicURL:   getenv("ATTESTA_PUBLIC_URL", "http://localhost:8080"),
		CORSOrigins: getenv("ATTESTA_CORS_ORIGINS", "http://localhost:5173"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
