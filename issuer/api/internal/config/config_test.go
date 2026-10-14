package config

import (
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"ATTESTA_ISSUER_DB_HOST": "db", "ATTESTA_ISSUER_OIDC_ISSUER": "https://login.example/realms/r"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 4001 || c.PublicURL != "http://localhost:4001" || c.Seed != DevSeed || c.QRLifetime != 24*time.Hour || c.RenewalCooldown != 10*time.Minute || c.Database.Name != "issuer_entitlements" {
		t.Fatalf("config %+v", c)
	}
	if c.Auth.Issuer != "https://login.example/realms/r" || c.Auth.ClientID != "issuer-gui" || c.Auth.Role != "issuer-staff" || c.Auth.Disabled {
		t.Fatalf("auth %+v", c.Auth)
	}
	if c.Database.User != "issuer" || c.SMTP.Host != "localhost" || c.SMTP.Port != 1025 || c.SMTP.From != "credentials@attesta.local" {
		t.Fatalf("config %+v", c)
	}
}

func TestReadsTheSettings(t *testing.T) {
	c, err := Load(env(map[string]string{
		"ATTESTA_ISSUER_DB_HOST": "db:5433", "ATTESTA_ISSUER_AUTH_DISABLED": "true", "ATTESTA_ISSUER_PORT": "8000", "ATTESTA_ISSUER_PUBLIC_URL": "https://issuer.example",
		"ATTESTA_ISSUER_SEED": "s", "ATTESTA_ISSUER_QR_LIFETIME_SECONDS": "60", "ATTESTA_ISSUER_SMTP_HOST": "smtp.gmail.com",
		"ATTESTA_ISSUER_SMTP_PORT": "587", "ATTESTA_ISSUER_SMTP_TLS": "starttls", "ATTESTA_ISSUER_MAIL_FROM": "me@example.com",
		"ATTESTA_ISSUER_RENEWAL_COOLDOWN": "500ms", "ATTESTA_ISSUER_DB_NAME": "other",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8000 || c.PublicURL != "https://issuer.example" || c.Seed != "s" || c.QRLifetime != time.Minute || c.Database.Host != "db:5433" || c.RenewalCooldown != 500*time.Millisecond || c.Database.Name != "other" {
		t.Fatalf("config %+v", c)
	}
	if c.SMTP.Host != "smtp.gmail.com" || c.SMTP.Port != 587 || c.SMTP.TLS != "starttls" || c.SMTP.From != "me@example.com" {
		t.Fatalf("smtp %+v", c.SMTP)
	}
}

func TestRejectsBadSettings(t *testing.T) {
	good := map[string]string{"ATTESTA_ISSUER_DB_HOST": "db", "ATTESTA_ISSUER_AUTH_DISABLED": "true"}
	cases := map[string]map[string]string{
		"no database":       {},
		"no sign-in":        {"ATTESTA_ISSUER_AUTH_DISABLED": ""},
		"port text":         {"ATTESTA_ISSUER_PORT": "abc"},
		"port out of range": {"ATTESTA_ISSUER_PORT": "70000"},
		"tls mode":          {"ATTESTA_ISSUER_SMTP_TLS": "ssl"},
		"qr lifetime":       {"ATTESTA_ISSUER_QR_LIFETIME_SECONDS": "0"},
		"cooldown text":     {"ATTESTA_ISSUER_RENEWAL_COOLDOWN": "soon"},
		"cooldown zero":     {"ATTESTA_ISSUER_RENEWAL_COOLDOWN": "0s"},
	}
	for name, extra := range cases {
		vars := map[string]string{}
		if name != "no database" {
			for k, v := range good {
				vars[k] = v
			}
		}
		for k, v := range extra {
			vars[k] = v
		}
		if _, err := Load(env(vars)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
