// Package proof verifies the cryptographic validity of a submitted ZK proof against its disclosed public signals. It says nothing about authorization
// — that's internal/authz's job, once a proof has been established as genuine.
package proof

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
)

// Verifier checks a hex-encoded Groth16 proof against its public signals.
type Verifier interface {
	Verify(proofHex string, publicSignals []string) (bool, error)
}

// CubicCircuit is gnark's canonical "hello world" circuit: prove knowledge of x such that x^3 + x + 5 == y. It is a placeholder standing in for a
// real credential-disclosure circuit.
type CubicCircuit struct {
	X frontend.Variable `gnark:"x"`
	Y frontend.Variable `gnark:",public"`
}

func (c *CubicCircuit) Define(api frontend.API) error {
	x3 := api.Mul(c.X, c.X, c.X)
	api.AssertIsEqual(c.Y, api.Add(x3, c.X, 5))
	return nil
}

// CubicScheme compiles CubicCircuit and runs its (non-production, in-memory) trusted setup once at construction time, then exposes Prove and Verify
// against that fixed key pair.
//
// The trusted setup here is a throwaway, process-local one — fine for a circuit with no real secrets, but groth16.Setup's own docs are explicit that
// production use requires either an MPC ceremony or a transparent-setup scheme (e.g. PLONK).
type CubicScheme struct {
	ccs constraint.ConstraintSystem
	pk  groth16.ProvingKey
	vk  groth16.VerifyingKey
}

func NewCubicScheme() (*CubicScheme, error) {
	var circuit CubicCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	if err != nil {
		return nil, fmt.Errorf("compile circuit: %w", err)
	}
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	return &CubicScheme{ccs: ccs, pk: pk, vk: vk}, nil
}

// Prove is a development convenience for exercising the API without a real ZK client; a browser-based prover has
// no use for it.
func (s *CubicScheme) Prove(x, y int64) (proofHex string, publicSignals []string, err error) {
	assignment := CubicCircuit{X: x, Y: y}
	w, err := frontend.NewWitness(&assignment, ecc.BN254.ScalarField())
	if err != nil {
		return "", nil, fmt.Errorf("build witness: %w", err)
	}
	proof, err := groth16.Prove(s.ccs, s.pk, w)
	if err != nil {
		return "", nil, fmt.Errorf("prove: %w", err)
	}
	var buf bytes.Buffer
	if _, err := proof.WriteTo(&buf); err != nil {
		return "", nil, fmt.Errorf("serialize proof: %w", err)
	}
	return hex.EncodeToString(buf.Bytes()), []string{fmt.Sprint(y)}, nil
}

// Verify reports whether proofHex is a valid proof for the given public signals. A cryptographically invalid proof is reported as (false, nil), not
// an error — only malformed input (bad hex, wrong signal count) is an error.
func (s *CubicScheme) Verify(proofHex string, publicSignals []string) (bool, error) {
	if len(publicSignals) != 1 {
		return false, fmt.Errorf("expected 1 public signal, got %d", len(publicSignals))
	}
	y, ok := new(big.Int).SetString(publicSignals[0], 10)
	if !ok {
		return false, fmt.Errorf("invalid public signal %q: not a decimal integer", publicSignals[0])
	}

	raw, err := hex.DecodeString(proofHex)
	if err != nil {
		return false, fmt.Errorf("decode proof: %w", err)
	}
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(raw)); err != nil {
		return false, fmt.Errorf("deserialize proof: %w", err)
	}

	assignment := CubicCircuit{Y: y}
	publicWitness, err := frontend.NewWitness(&assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return false, fmt.Errorf("build public witness: %w", err)
	}

	if err := groth16.Verify(proof, s.vk, publicWitness); err != nil {
		return false, nil
	}
	return true, nil
}
