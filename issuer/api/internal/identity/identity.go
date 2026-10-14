// Package identity is the issuer's signing key and the did:key that verifiers trust it by.
package identity

import (
	"crypto/ed25519"
	"crypto/sha256"

	sdjwt "github.com/threehook/sdjwt-go"
)

type Identity struct {
	Key ed25519.PrivateKey
	// DID is the issuer as it appears in every credential.
	DID string
}

// FromSeed derives the key from the text: its SHA-256 is the Ed25519 seed. The same text always gives the same DID.
func FromSeed(seed string) Identity {
	sum := sha256.Sum256([]byte(seed))
	key := ed25519.NewKeyFromSeed(sum[:])
	return Identity{Key: key, DID: sdjwt.DIDKeyFromEd25519(key.Public().(ed25519.PublicKey))}
}
