package authz

import (
	"math/big"
	"testing"
)

func encodeFieldString(t *testing.T, s string) string {
	t.Helper()
	return new(big.Int).SetBytes([]byte(s)).String()
}

func TestDecodePublicSignals(t *testing.T) {
	root := "123456789"
	typeField := encodeFieldString(t, "Diploma")
	issuerField := encodeFieldString(t, "trusted-university")

	input, ok, err := DecodePublicSignals([]string{root, typeField, issuerField}, "diploma-vault", root)
	if err != nil {
		t.Fatalf("DecodePublicSignals: %v", err)
	}
	if !ok {
		t.Fatal("DecodePublicSignals: got ok=false, want true")
	}
	want := Input{Resource: "diploma-vault", Type: "Diploma", Issuer: "trusted-university"}
	if input != want {
		t.Fatalf("DecodePublicSignals = %+v, want %+v", input, want)
	}
}

func TestDecodePublicSignalsRootMismatch(t *testing.T) {
	typeField := encodeFieldString(t, "Diploma")
	issuerField := encodeFieldString(t, "trusted-university")

	_, ok, err := DecodePublicSignals([]string{"111", typeField, issuerField}, "diploma-vault", "222")
	if err != nil {
		t.Fatalf("DecodePublicSignals: %v", err)
	}
	if ok {
		t.Fatal("DecodePublicSignals with mismatched root: got ok=true, want false")
	}
}

func TestDecodePublicSignalsMalformed(t *testing.T) {
	if _, _, err := DecodePublicSignals([]string{"1", "2"}, "diploma-vault", "1"); err == nil {
		t.Fatal("DecodePublicSignals with wrong signal count: want error, got nil")
	}
	if _, _, err := DecodePublicSignals([]string{"1", "not-a-number", "2"}, "diploma-vault", "1"); err == nil {
		t.Fatal("DecodePublicSignals with non-numeric field: want error, got nil")
	}
}
