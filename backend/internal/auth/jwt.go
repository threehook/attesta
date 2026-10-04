// Package auth provides the mock login used by examples/simple-gui: it issues a JWT carrying whatever role(s) the caller asked for, with no real
// credential check behind it. The JWT identifies the session for audit/logging; it is never what an /v1/authorize decision is based on — that comes
// from the ZK proof and the Gno policy evaluating it.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Subject string   `json:"sub"`
	Roles   []string `json:"roles"`
	jwt.RegisteredClaims
}

// Issuer signs and verifies the mock login's JWTs.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: time.Hour}
}

// MockLogin issues a JWT for subject with the given roles, no credential check performed.
func (i *Issuer) MockLogin(subject string, roles []string) (string, error) {
	now := time.Now()
	claims := Claims{
		Subject: subject,
		Roles:   roles,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a JWT issued by MockLogin, returning its claims.
func (i *Issuer) Verify(tokenString string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if claims.Subject == "" {
		return nil, errors.New("token missing subject")
	}
	return &claims, nil
}
