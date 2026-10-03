// Package proof verifies the cryptographic validity of a submitted ZK proof against its disclosed public signals. It says nothing about authorization
// — that's internal/authz's job, once a proof has been established as genuine.
package proof

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/consensys/gnark-crypto/ecc/bn254"
)

// Verifier checks a Groth16 proof, in snarkjs's JSON format, against its public signals.
type Verifier interface {
	Verify(proofJSON []byte, publicSignals []string) (bool, error)
}

// VerifyingKey holds a Groth16 verifying key parsed from a snarkjs verification_key.json file (as produced by `snarkjs zkey export verificationkey`,
// see client-lib/circuits/cubic/build.sh). IC is the public-input commitment basis: IC[0] is the constant term, IC[i+1] corresponds to the i-th
// public signal.
type VerifyingKey struct {
	Alpha bn254.G1Affine
	Beta  bn254.G2Affine
	Gamma bn254.G2Affine
	Delta bn254.G2Affine
	IC    []bn254.G1Affine
}

type verifyingKeyJSON struct {
	Protocol string     `json:"protocol"`
	VkAlpha1 []string   `json:"vk_alpha_1"`
	VkBeta2  [][]string `json:"vk_beta_2"`
	VkGamma2 [][]string `json:"vk_gamma_2"`
	VkDelta2 [][]string `json:"vk_delta_2"`
	IC       [][]string `json:"IC"`
}

// LoadVerifyingKey reads a snarkjs-exported verification key from path.
func LoadVerifyingKey(path string) (*VerifyingKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read verifying key: %w", err)
	}

	var raw verifyingKeyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse verifying key: %w", err)
	}
	if raw.Protocol != "groth16" {
		return nil, fmt.Errorf("unsupported protocol %q, only groth16 is supported", raw.Protocol)
	}

	alpha, err := parseG1(raw.VkAlpha1)
	if err != nil {
		return nil, fmt.Errorf("parse vk_alpha_1: %w", err)
	}
	beta, err := parseG2(raw.VkBeta2)
	if err != nil {
		return nil, fmt.Errorf("parse vk_beta_2: %w", err)
	}
	gamma, err := parseG2(raw.VkGamma2)
	if err != nil {
		return nil, fmt.Errorf("parse vk_gamma_2: %w", err)
	}
	delta, err := parseG2(raw.VkDelta2)
	if err != nil {
		return nil, fmt.Errorf("parse vk_delta_2: %w", err)
	}
	if len(raw.IC) == 0 {
		return nil, fmt.Errorf("verifying key has no IC entries")
	}
	ic := make([]bn254.G1Affine, len(raw.IC))
	for i, coords := range raw.IC {
		ic[i], err = parseG1(coords)
		if err != nil {
			return nil, fmt.Errorf("parse IC[%d]: %w", i, err)
		}
	}

	return &VerifyingKey{Alpha: alpha, Beta: beta, Gamma: gamma, Delta: delta, IC: ic}, nil
}

// SnarkjsVerifier verifies Groth16 proofs produced by snarkjs against a VerifyingKey, using only gnark-crypto's curve/pairing primitives — no gnark
// circuit compiler or trusted-setup machinery is involved on this side at all; the circuit, its setup, and proving all live entirely in
// client-lib/circuits (circom + snarkjs).
type SnarkjsVerifier struct {
	vk *VerifyingKey
}

func NewSnarkjsVerifier(vk *VerifyingKey) *SnarkjsVerifier {
	return &SnarkjsVerifier{vk: vk}
}

type proofJSON struct {
	Protocol string     `json:"protocol"`
	PiA      []string   `json:"pi_a"`
	PiB      [][]string `json:"pi_b"`
	PiC      []string   `json:"pi_c"`
}

// Verify reports whether proofData (snarkjs's proof.json format) is a valid proof for publicSignals (snarkjs's public.json format: one decimal string
// per public signal, in declaration order). A cryptographically invalid proof — including one built from well-formed-but-wrong curve points — is
// reported as (false, nil), not an error; only malformed input (bad JSON, wrong signal count, non-numeric signal) is an error.
func (v *SnarkjsVerifier) Verify(proofData []byte, publicSignals []string) (bool, error) {
	var raw proofJSON
	if err := json.Unmarshal(proofData, &raw); err != nil {
		return false, fmt.Errorf("parse proof: %w", err)
	}
	if raw.Protocol != "" && raw.Protocol != "groth16" {
		return false, fmt.Errorf("unsupported protocol %q, only groth16 is supported", raw.Protocol)
	}
	if len(publicSignals) != len(v.vk.IC)-1 {
		return false, fmt.Errorf("expected %d public signals, got %d", len(v.vk.IC)-1, len(publicSignals))
	}

	a, err := parseG1(raw.PiA)
	if err != nil {
		return false, fmt.Errorf("parse pi_a: %w", err)
	}
	b, err := parseG2(raw.PiB)
	if err != nil {
		return false, fmt.Errorf("parse pi_b: %w", err)
	}
	c, err := parseG1(raw.PiC)
	if err != nil {
		return false, fmt.Errorf("parse pi_c: %w", err)
	}
	if !a.IsInSubGroup() || !b.IsInSubGroup() || !c.IsInSubGroup() {
		return false, nil
	}

	// vk_x = IC[0] + sum(publicSignals[i] * IC[i+1]) — the public-input term of the verification equation.
	vkX := v.vk.IC[0]
	for i, s := range publicSignals {
		scalar, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return false, fmt.Errorf("invalid public signal %q: not a decimal integer", s)
		}
		var term bn254.G1Affine
		term.ScalarMultiplication(&v.vk.IC[i+1], scalar)
		vkX.Add(&vkX, &term)
	}

	// Groth16: e(A,B) == e(alpha,beta) * e(vk_x,gamma) * e(C,delta), checked as a single product-equals-one multi-pairing by negating one side of
	// every term but the first.
	var negAlpha, negVkX, negC bn254.G1Affine
	negAlpha.Neg(&v.vk.Alpha)
	negVkX.Neg(&vkX)
	negC.Neg(&c)

	ok, err := bn254.PairingCheck(
		[]bn254.G1Affine{a, negAlpha, negVkX, negC},
		[]bn254.G2Affine{b, v.vk.Beta, v.vk.Gamma, v.vk.Delta},
	)
	if err != nil {
		return false, fmt.Errorf("pairing check: %w", err)
	}
	return ok, nil
}

func parseG1(coords []string) (bn254.G1Affine, error) {
	var p bn254.G1Affine
	if len(coords) < 2 {
		return p, fmt.Errorf("G1 point needs 2 coordinates, got %d", len(coords))
	}
	if _, err := p.X.SetString(coords[0]); err != nil {
		return p, fmt.Errorf("x: %w", err)
	}
	if _, err := p.Y.SetString(coords[1]); err != nil {
		return p, fmt.Errorf("y: %w", err)
	}
	return p, nil
}

// parseG2 parses a G2 point from snarkjs's [[x0,x1],[y0,y1],...] encoding.
func parseG2(coords [][]string) (bn254.G2Affine, error) {
	var p bn254.G2Affine
	if len(coords) < 2 || len(coords[0]) < 2 || len(coords[1]) < 2 {
		return p, fmt.Errorf("G2 point needs 2x2 coordinates")
	}
	if _, err := p.X.A0.SetString(coords[0][0]); err != nil {
		return p, fmt.Errorf("x.a0: %w", err)
	}
	if _, err := p.X.A1.SetString(coords[0][1]); err != nil {
		return p, fmt.Errorf("x.a1: %w", err)
	}
	if _, err := p.Y.A0.SetString(coords[1][0]); err != nil {
		return p, fmt.Errorf("y.a0: %w", err)
	}
	if _, err := p.Y.A1.SetString(coords[1][1]); err != nil {
		return p, fmt.Errorf("y.a1: %w", err)
	}
	return p, nil
}
