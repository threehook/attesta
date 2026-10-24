package oid4vci

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func TestANonceWorksUntilItExpires(t *testing.T) {
	key := derive([]byte("secret"), "nonce")

	nonce, err := mintNonce(key, t0)
	if err != nil {
		t.Fatal(err)
	}

	if !checkNonce(key, nonce, t0.Add(nonceLifetime-time.Second)) {
		t.Fatal("a fresh nonce was refused")
	}

	if checkNonce(key, nonce, t0.Add(nonceLifetime)) {
		t.Fatal("an expired nonce was accepted")
	}
}

func TestANonceOfAnotherKeyOrAlteredIsRefused(t *testing.T) {
	key := derive([]byte("secret"), "nonce")
	nonce, _ := mintNonce(key, t0)
	other, _ := mintNonce(derive([]byte("another"), "nonce"), t0)

	flipped := nonce[:len(nonce)-2] + strings.ToUpper(nonce[len(nonce)-2:])
	for name, n := range map[string]string{"other key": other, "altered": flipped, "empty": "", "text": "not a nonce", "short": nonce[:10]} {
		if n == nonce {
			continue
		}

		if checkNonce(key, n, t0) {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOnlyPublicEd25519AndP256KeysAreAccepted(t *testing.T) {
	good := map[string]any{"kty": "OKP", "crv": "Ed25519", "x": "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
	if _, jwk, err := publicKeyFromJWK(good); err != nil || jwk["x"] != good["x"] {
		t.Fatalf("good key: %v %v", jwk, err)
	}

	withPrivate := map[string]any{"kty": "OKP", "crv": "Ed25519", "x": good["x"], "d": "nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A"}

	bad := map[string]map[string]any{
		"private part": withPrivate,
		"rsa":          {"kty": "RSA", "n": "x", "e": "AQAB"},
		"short x":      {"kty": "OKP", "crv": "Ed25519", "x": "AAAA"},
		"off curve":    {"kty": "EC", "crv": "P-256", "x": "AAAA", "y": "AAAA"},
		"empty":        {},
	}
	for name, jwk := range bad {
		if _, _, err := publicKeyFromJWK(jwk); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
