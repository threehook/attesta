// Package config reads the issuer's settings from ATTESTA_ISSUER_* variables.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DevSeed fixes the issuer's DID so the example policies can name it; set ATTESTA_ISSUER_SEED for anything but local development.
const DevSeed = "dev-only-insecure-issuer-seed"

type Config struct {
	Port int
	// PublicURL is where wallets reach the issuer; it appears in the QR codes and in the issuer's metadata.
	PublicURL string
	// Seed derives the issuer's signing key, and so its DID.
	Seed string
	// Name is the logical issuer, e.g. "Gemeente Utrecht", written into every credential and the metadata for display only.
	Name       string
	QRLifetime time.Duration
	// RenewalCooldown is the least time between two new QR codes that an expired one asks for.
	RenewalCooldown time.Duration
	Database        Database
	SMTP            SMTP
	Auth            Auth
	// SPIFFE switches on the endpoints for workloads when it names a trust domain.
	SPIFFE SPIFFE
}

// SPIFFE is how the issuer recognizes a workload by its JWT-SVID.
type SPIFFE struct {
	// TrustDomain is the only trust domain whose workloads are accepted; empty leaves the workload endpoints out.
	TrustDomain string
	// Socket is the SPIFFE Workload API address the trust bundle is read from; empty means the SPIFFE_ENDPOINT_SOCKET variable.
	Socket string
	// Audience is what a JWT-SVID must name; it is the public URL unless set.
	Audience string
	// CredentialLifetime is how long a workload's credential lasts.
	CredentialLifetime time.Duration
	// Approvals are the settings of the requests a workload parks.
	Approvals Approvals
}

// Approvals are the requests of a workload for an action that no mandate covers.
type Approvals struct {
	// Required is how many different people must approve a request.
	Required int
	// RequestLifetime is how long a request waits for approval.
	RequestLifetime time.Duration
	// CredentialLifetime is how long the Approval credential lasts.
	CredentialLifetime time.Duration
	// Notify are the addresses that get a mail for a new request.
	Notify []string
}

// Auth is the OpenID Connect server the staff sign in at. Disabled leaves the forms open and is for tests and local development only.
type Auth struct {
	Issuer       string
	DiscoveryURL string
	ClientID     string
	Role         string
	// ApproverRole is the role of the people who approve mandates.
	ApproverRole string
	Disabled     bool
}

type Database struct {
	// Host is `host` or `host:port`.
	Host     string
	User     string
	Password string
	// Name is the database for entitlements and sessions; it is created when missing.
	Name string
}

type SMTP struct {
	Host     string
	Port     int
	User     string
	Password string
	// TLS is "none", "starttls" or "tls"; empty lets the connection use STARTTLS when the server offers it.
	TLS  string
	From string
}

// Load reads the settings through getenv, so tests need no process environment.
func Load(getenv func(string) string) (Config, error) {
	str := func(name, fallback string) string {
		if v := getenv("ATTESTA_ISSUER_" + name); v != "" {
			return v
		}

		return fallback
	}
	number := func(name string, fallback int) (int, error) {
		v := getenv("ATTESTA_ISSUER_" + name)
		if v == "" {
			return fallback, nil
		}

		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("ATTESTA_ISSUER_%s must be a number, got %q", name, v)
		}

		return n, nil
	}

	port, err := number("PORT", 4001)
	if err != nil {
		return Config{}, err
	}

	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_PORT must be a port number, got %d", port)
	}

	smtpPort, err := number("SMTP_PORT", 1025)
	if err != nil {
		return Config{}, err
	}

	qrSeconds, err := number("QR_LIFETIME_SECONDS", 24*3600)
	if err != nil {
		return Config{}, err
	}

	if qrSeconds < 1 {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_QR_LIFETIME_SECONDS must be positive, got %d", qrSeconds)
	}

	tls := str("SMTP_TLS", "")
	if tls != "" && tls != "none" && tls != "starttls" && tls != "tls" {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_SMTP_TLS must be none, starttls or tls, got %q", tls)
	}

	cooldown, err := time.ParseDuration(str("RENEWAL_COOLDOWN", "10m"))
	if err != nil || cooldown <= 0 {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_RENEWAL_COOLDOWN must be a duration such as 10m or 500ms, got %q", str("RENEWAL_COOLDOWN", ""))
	}

	workloadLifetime, err := time.ParseDuration(str("WORKLOAD_LIFETIME", "1h"))
	if err != nil || workloadLifetime <= 0 {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_WORKLOAD_LIFETIME must be a duration such as 1h or 30m, got %q", str("WORKLOAD_LIFETIME", ""))
	}

	requestLifetime, err := time.ParseDuration(str("APPROVAL_REQUEST_LIFETIME", "24h"))
	if err != nil || requestLifetime <= 0 {
		return Config{}, fmt.Errorf(
			"ATTESTA_ISSUER_APPROVAL_REQUEST_LIFETIME must be a duration such as 24h or 30m, got %q",
			str("APPROVAL_REQUEST_LIFETIME", ""),
		)
	}

	approvalLifetime, err := time.ParseDuration(str("APPROVAL_LIFETIME", "15m"))
	if err != nil || approvalLifetime <= 0 {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_APPROVAL_LIFETIME must be a duration such as 15m or 1h, got %q", str("APPROVAL_LIFETIME", ""))
	}

	approvalsRequired, err := number("APPROVALS_REQUIRED", 2)
	if err != nil || approvalsRequired < 1 {
		return Config{}, errors.New("ATTESTA_ISSUER_APPROVALS_REQUIRED must be a number of people, 1 or more")
	}

	dbHost := str("DB_HOST", "")
	if dbHost == "" {
		return Config{}, errors.New("ATTESTA_ISSUER_DB_HOST is required")
	}

	auth := Auth{
		Issuer: str("OIDC_ISSUER", ""), DiscoveryURL: str("OIDC_DISCOVERY_URL", ""), ClientID: str("OIDC_CLIENT_ID", "issuer-gui"),
		Role: str(
			"OIDC_ROLE",
			"issuer-staff",
		), ApproverRole: str("OIDC_APPROVER_ROLE", "release-manager"), Disabled: str("AUTH_DISABLED", "") == "true",
	}
	if auth.Issuer == "" && !auth.Disabled {
		return Config{}, errors.New("ATTESTA_ISSUER_OIDC_ISSUER is required (ATTESTA_ISSUER_AUTH_DISABLED=true leaves the forms open)")
	}

	return Config{
		Port:       port,
		PublicURL:  str("PUBLIC_URL", fmt.Sprintf("http://localhost:%d", port)),
		Seed:       str("SEED", DevSeed),
		Name:       str("NAME", "Gemeente Vlierdam"),
		QRLifetime: time.Duration(qrSeconds) * time.Second, RenewalCooldown: cooldown,
		Database: Database{
			Host:     dbHost,
			User:     str("DB_USER", "issuer"),
			Password: str("DB_PASSWORD", ""),
			Name:     str("DB_NAME", "issuer_entitlements"),
		},
		SMTP: SMTP{
			Host: str("SMTP_HOST", "localhost"), Port: smtpPort, User: str("SMTP_USER", ""), Password: str("SMTP_PASSWORD", ""),
			TLS: tls, From: str("MAIL_FROM", "credentials@attesta.local"),
		},
		Auth: auth,
		SPIFFE: SPIFFE{
			TrustDomain: str("SPIFFE_TRUST_DOMAIN", ""), Socket: str("SPIFFE_SOCKET", ""),
			Audience: str("SPIFFE_AUDIENCE", str("PUBLIC_URL", fmt.Sprintf("http://localhost:%d", port))), CredentialLifetime: workloadLifetime,
			Approvals: Approvals{
				Required: approvalsRequired, RequestLifetime: requestLifetime, CredentialLifetime: approvalLifetime,
				Notify: addresses(str("APPROVAL_NOTIFY", "")),
			},
		},
	}, nil
}

// addresses reads a comma-separated list without blanks.
func addresses(text string) []string {
	var out []string

	for a := range strings.SplitSeq(text, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}

	return out
}
