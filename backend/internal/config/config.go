// Package config loads Attesta's runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds Attesta's runtime configuration; see FromEnv.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// PoliciesDir is loaded into the policy store at startup when set, and watched afterwards: a new, changed or removed file takes effect without a
	// restart. Empty starts with no policies, to be hot-deployed via POST /admin/policies.
	PoliciesDir string
	// AdminToken gates POST /admin/policies. A static shared secret, fine for local development; revisit before this is exposed beyond it.
	AdminToken string
	// PublicURL is this backend's externally reachable base URL; wallets are told to post their presentations to it.
	PublicURL string
	// CORSOrigins is the comma-separated browser-origin allowlist passed to httpapi.Server; see withCORS.
	CORSOrigins string
	// StatusPrefixes are the URLs, each ending in "/", that credential status lists may be fetched from (comma-separated). A credential whose list is
	// elsewhere is refused, and with none set every credential is.
	StatusPrefixes []string
	// StatusHosts sends the public address of a status list's host to the address this backend reaches it at, as `https://public=http://internal`
	// pairs separated by commas.
	StatusHosts map[string]string
}

func FromEnv() (Config, error) {
	hosts, err := pairs(os.Getenv("ATTESTA_STATUS_HOSTS"))
	if err != nil {
		return Config{}, fmt.Errorf("ATTESTA_STATUS_HOSTS: %w", err)
	}
	return Config{
		StatusPrefixes: list(os.Getenv("ATTESTA_STATUS_URL_PREFIXES")), StatusHosts: hosts,
		Addr:        getenv("ATTESTA_ADDR", ":8080"),
		PoliciesDir: getenv("ATTESTA_POLICIES_DIR", ""),
		AdminToken:  getenv("ATTESTA_ADMIN_TOKEN", "dev-only-insecure-admin-token"),
		PublicURL:   getenv("ATTESTA_PUBLIC_URL", "http://localhost:8080"),
		CORSOrigins: getenv("ATTESTA_CORS_ORIGINS", "http://localhost:5173"),
	}, nil
}

func list(value string) []string {
	var out []string
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func pairs(value string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range list(value) {
		from, to, ok := strings.Cut(p, "=")
		if !ok || from == "" || to == "" {
			return nil, fmt.Errorf("%q is not public=internal", p)
		}
		out[from] = to
	}
	return out, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
