// Package config reads the issuer's settings from ATTESTA_ISSUER_* variables.
package config

import (
	"fmt"
	"strconv"
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
	Database   Database
	SMTP       SMTP
}

type Database struct {
	// Host is `host` or `host:port`.
	Host     string
	User     string
	Password string
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
	dbHost := str("DB_HOST", "")
	if dbHost == "" {
		return Config{}, fmt.Errorf("ATTESTA_ISSUER_DB_HOST is required")
	}

	return Config{
		Port:       port,
		PublicURL:  str("PUBLIC_URL", fmt.Sprintf("http://localhost:%d", port)),
		Seed:       str("SEED", DevSeed),
		Name:       str("NAME", "Gemeente Vlierdam"),
		QRLifetime: time.Duration(qrSeconds) * time.Second,
		Database:   Database{Host: dbHost, User: str("DB_USER", "issuer"), Password: str("DB_PASSWORD", "")},
		SMTP: SMTP{
			Host: str("SMTP_HOST", "localhost"), Port: smtpPort, User: str("SMTP_USER", ""), Password: str("SMTP_PASSWORD", ""),
			TLS: tls, From: str("MAIL_FROM", "credentials@attesta.local"),
		},
	}, nil
}
