package sdjwt

import (
	"crypto"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// ed25519Multicodec is the multicodec prefix (0xed, as a varint) that marks an Ed25519 public key in a did:key.
var ed25519Multicodec = []byte{0xed, 0x01}

// DIDKey resolves an issuer that identifies itself with an Ed25519 did:key. The key is encoded in the DID, so resolving needs no network and no
// registry; which issuers to trust is the caller's decision.
type DIDKey struct{}

func (DIDKey) Key(issuer string, _ map[string]any) (crypto.PublicKey, error) {
	encoded, ok := strings.CutPrefix(issuer, "did:key:z")
	if !ok {
		return nil, fmt.Errorf("issuer %q is not a did:key", issuer)
	}
	raw, err := base58Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("issuer %q: %w", issuer, err)
	}
	if len(raw) != len(ed25519Multicodec)+ed25519.PublicKeySize || string(raw[:len(ed25519Multicodec)]) != string(ed25519Multicodec) {
		return nil, fmt.Errorf("issuer %q is not an Ed25519 did:key", issuer)
	}
	return ed25519.PublicKey(raw[len(ed25519Multicodec):]), nil
}

func base58Decode(s string) ([]byte, error) {
	n := new(big.Int)
	base := big.NewInt(58)
	for _, c := range s {
		i := strings.IndexRune(base58Alphabet, c)
		if i < 0 {
			return nil, errors.New("invalid base58 character")
		}
		n.Mul(n, base).Add(n, big.NewInt(int64(i)))
	}
	out := n.Bytes()
	leadingZeros := len(s) - len(strings.TrimLeft(s, "1"))
	return append(make([]byte, leadingZeros), out...), nil
}

func base58Encode(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// DIDKeyFromEd25519 returns the did:key that identifies an Ed25519 public key.
func DIDKeyFromEd25519(key ed25519.PublicKey) string {
	return "did:key:z" + base58Encode(append(append([]byte{}, ed25519Multicodec...), key...))
}
