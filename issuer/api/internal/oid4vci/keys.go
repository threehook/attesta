package oid4vci

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/big"
	"time"
)

// derive makes a key for one purpose out of the issuer's secret.
func derive(secret []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

const nonceLifetime = 5 * time.Minute

// mintNonce makes a nonce that vouches for itself: an expiry and random bytes, signed. No state, so any replica accepts it.
func mintNonce(key []byte, now time.Time) (string, error) {
	payload := make([]byte, 8+16)
	binary.BigEndian.PutUint64(payload, uint64(now.Add(nonceLifetime).Unix()))
	if _, err := rand.Read(payload[8:]); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(payload)), nil
}

func checkNonce(key []byte, nonce string, now time.Time) bool {
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(raw) != 8+16+sha256.Size {
		return false
	}
	payload, sig := raw[:24], raw[24:]
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hmac.Equal(sig, mac.Sum(nil)) && now.Unix() < int64(binary.BigEndian.Uint64(payload))
}

// publicKeyFromJWK reads an Ed25519 or P-256 public key, and gives it back as the JWK the credential is bound to. A JWK with a private part is refused.
func publicKeyFromJWK(jwk map[string]any) (crypto.PublicKey, map[string]any, error) {
	str := func(k string) string { s, _ := jwk[k].(string); return s }
	b64 := func(k string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(str(k)) }
	if _, private := jwk["d"]; private {
		return nil, nil, errors.New("the key holds a private part")
	}
	switch {
	case str("kty") == "OKP" && str("crv") == "Ed25519":
		x, err := b64("x")
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, nil, errors.New("invalid Ed25519 key")
		}
		return ed25519.PublicKey(x), map[string]any{"kty": "OKP", "crv": "Ed25519", "x": str("x")}, nil
	case str("kty") == "EC" && str("crv") == "P-256":
		x, errX := b64("x")
		y, errY := b64("y")
		if errX != nil || errY != nil {
			return nil, nil, errors.New("invalid P-256 key")
		}
		key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !key.Curve.IsOnCurve(key.X, key.Y) {
			return nil, nil, errors.New("the key is not on the P-256 curve")
		}
		return key, map[string]any{"kty": "EC", "crv": "P-256", "x": str("x"), "y": str("y")}, nil
	}
	return nil, nil, errors.New("unsupported key: only Ed25519 and P-256 keys are accepted")
}
