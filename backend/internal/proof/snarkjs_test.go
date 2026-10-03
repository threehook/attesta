package proof

import (
	"os"
	"testing"
)

// testdata/cubic_* were produced by client-lib/circuits/cubic/build.sh and a real `snarkjs groth16 prove` run for
// x=3 (3^3+3+5=35) — this test exercises real cross-toolchain interop, not a fabricated fixture.
func loadTestVerifier(t *testing.T) *SnarkjsVerifier {
	t.Helper()
	vk, err := LoadVerifyingKey("testdata/cubic_verification_key.json")
	if err != nil {
		t.Fatalf("LoadVerifyingKey: %v", err)
	}
	return NewSnarkjsVerifier(vk)
}

func TestSnarkjsVerifierValidProof(t *testing.T) {
	v := loadTestVerifier(t)
	proofData, err := os.ReadFile("testdata/cubic_proof.json")
	if err != nil {
		t.Fatalf("read proof fixture: %v", err)
	}

	ok, err := v.Verify(proofData, []string{"35"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify valid proof: got false, want true")
	}
}

func TestSnarkjsVerifierRejectsWrongPublicSignal(t *testing.T) {
	v := loadTestVerifier(t)
	proofData, err := os.ReadFile("testdata/cubic_proof.json")
	if err != nil {
		t.Fatalf("read proof fixture: %v", err)
	}

	ok, err := v.Verify(proofData, []string{"999"})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatal("Verify with wrong public signal: got true, want false")
	}
}

func TestSnarkjsVerifierRejectsMalformedInput(t *testing.T) {
	v := loadTestVerifier(t)

	if _, err := v.Verify([]byte("not json"), []string{"35"}); err == nil {
		t.Fatal("Verify with malformed proof JSON: want error, got nil")
	}

	proofData, err := os.ReadFile("testdata/cubic_proof.json")
	if err != nil {
		t.Fatalf("read proof fixture: %v", err)
	}
	if _, err := v.Verify(proofData, []string{"35", "extra"}); err == nil {
		t.Fatal("Verify with wrong signal count: want error, got nil")
	}
	if _, err := v.Verify(proofData, []string{"not-a-number"}); err == nil {
		t.Fatal("Verify with non-numeric signal: want error, got nil")
	}
}
