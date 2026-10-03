package proof

import "testing"

func TestCubicSchemeRoundTrip(t *testing.T) {
	scheme, err := NewCubicScheme()
	if err != nil {
		t.Fatalf("NewCubicScheme: %v", err)
	}

	proofHex, signals, err := scheme.Prove(3, 35) // 3^3 + 3 + 5 == 35
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	ok, err := scheme.Verify(proofHex, signals)
	if err != nil {
		t.Fatalf("Verify valid proof: %v", err)
	}
	if !ok {
		t.Fatal("Verify valid proof: got false, want true")
	}

	ok, err = scheme.Verify(proofHex, []string{"999"})
	if err != nil {
		t.Fatalf("Verify with mismatched public signal: %v", err)
	}
	if ok {
		t.Fatal("Verify with mismatched public signal: got true, want false")
	}
}

func TestCubicSchemeRejectsBadProof(t *testing.T) {
	scheme, err := NewCubicScheme()
	if err != nil {
		t.Fatalf("NewCubicScheme: %v", err)
	}

	if _, err := scheme.Verify("not-hex", []string{"35"}); err == nil {
		t.Fatal("Verify with non-hex proof: want error, got nil")
	}

	if _, err := scheme.Verify("", []string{}); err == nil {
		t.Fatal("Verify with no public signals: want error, got nil")
	}
}
