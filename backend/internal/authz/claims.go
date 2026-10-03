package authz

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
)

// DecodePublicSignals interprets diploma_membership.circom's public signals ([root, reqType, reqIssuer], in that declaration order — see
// client-lib/circuits/diploma_membership/diploma_membership.circom's `component main {public [...]}` line) into an Input for Resource.
//
// It first checks the disclosed root against expectedRoot, the real demo registry's Merkle root (see
// client-lib/circuits/diploma_membership/registry.mjs). Without that check, a prover could build their own self-consistent registry (any set of
// leaves hashes to *some* root) and produce an equally "valid" membership proof against it — the proof alone only shows internal consistency, not
// that the root is the real registry's. A root mismatch is reported as (Input{}, false, nil), the same shape as a cryptographically invalid proof:
// it's attacker-reachable input, not a server error. Malformed input (wrong signal count, non-numeric field) is an error.
func DecodePublicSignals(publicSignals []string, resource, expectedRoot string) (Input, bool, error) {
	if len(publicSignals) != 3 {
		return Input{}, false, fmt.Errorf("expected 3 public signals (root, reqType, reqIssuer), got %d", len(publicSignals))
	}
	root, reqTypeField, reqIssuerField := publicSignals[0], publicSignals[1], publicSignals[2]

	if root != expectedRoot {
		return Input{}, false, nil
	}

	credType, err := decodeFieldString(reqTypeField)
	if err != nil {
		return Input{}, false, fmt.Errorf("decode disclosed type: %w", err)
	}
	issuer, err := decodeFieldString(reqIssuerField)
	if err != nil {
		return Input{}, false, fmt.Errorf("decode disclosed issuer: %w", err)
	}

	return Input{Resource: resource, Type: credType, Issuer: issuer}, true, nil
}

// decodeFieldString inverts registry.mjs's encodeString: a field element packed from a UTF-8 string's bytes, big-endian (equivalent to
// big.Int.SetBytes, so big.Int.Bytes() is its exact inverse).
func decodeFieldString(s string) (string, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return "", fmt.Errorf("invalid field element %q", s)
	}
	return string(n.Bytes()), nil
}

// LoadRegistryRoot reads the "root" field out of a registry.json produced by
// client-lib/circuits/diploma_membership/registry.mjs, for use as DecodePublicSignals's expectedRoot.
func LoadRegistryRoot(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read registry: %w", err)
	}
	var parsed struct {
		Root string `json:"root"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("parse registry: %w", err)
	}
	if parsed.Root == "" {
		return "", fmt.Errorf("registry %q has no root", path)
	}
	return parsed.Root, nil
}
