package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestMockLoginAndVerify(t *testing.T) {
	issuer := NewIssuer("test-secret")

	token, err := issuer.MockLogin("alice", []string{"student"})
	if err != nil {
		t.Fatalf("MockLogin: %v", err)
	}

	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "alice")
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "student" {
		t.Errorf("Roles = %v, want [student]", claims.Roles)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token, err := NewIssuer("secret-a").MockLogin("alice", nil)
	if err != nil {
		t.Fatalf("MockLogin: %v", err)
	}

	if _, err := NewIssuer("secret-b").Verify(token); err == nil {
		t.Fatal("Verify with wrong secret: want error, got nil")
	}
}

func TestVerifyRejectsMalformedToken(t *testing.T) {
	if _, err := NewIssuer("test-secret").Verify("not-a-jwt"); err == nil {
		t.Fatal("Verify with malformed token: want error, got nil")
	}
}

func TestVerifyRejectsMissingSubject(t *testing.T) {
	issuer := NewIssuer("test-secret")
	claims := Claims{Roles: []string{"student"}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(issuer.secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := issuer.Verify(signed); err == nil {
		t.Fatal("Verify with no subject claim: want error, got nil")
	}
}

func TestVerifyRejectsWrongSigningMethod(t *testing.T) {
	issuer := NewIssuer("test-secret")
	claims := Claims{Subject: "alice"}
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := issuer.Verify(signed); err == nil {
		t.Fatal("Verify with alg=none: want error, got nil")
	}
}
