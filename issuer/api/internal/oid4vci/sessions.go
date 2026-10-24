package oid4vci

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A session is one QR code: from the offer that holds a one-time code, through the access token the code is exchanged for, to the issued credential.
const (
	stateOffer     = "offer"
	stateToken     = "token"
	stateCompleted = "completed"
)

var (
	ErrNotFound     = errors.New("session not found")
	ErrInvalidGrant = errors.New("the code is unknown, used or expired")
)

type Session struct {
	ID              string
	OfferID         string
	EntitlementID   string
	ConfigurationID string
	State           string
	ExpiresAt       time.Time
	CreatedAt       time.Time
}

type NewSession struct {
	EntitlementID   string
	ConfigurationID string
	Lifetime        time.Duration
}

type Sessions struct {
	pool    *pgxpool.Pool
	codeKey []byte
}

// NewSessions keeps sessions in the pool's database. The one-time codes are derived from the offer id with a key made of secret, so an offer can
// show its code while the database only holds a hash of it.
func NewSessions(pool *pgxpool.Pool, secret []byte) *Sessions {
	return &Sessions{pool: pool, codeKey: derive(secret, "code")}
}

func (s *Sessions) code(offerID string) string {
	mac := hmac.New(sha256.New, s.codeKey)
	mac.Write([]byte(offerID))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

const sessionColumns = "id, offer_id, entitlement_id, credential_configuration_id, state, expires_at, created_at"

func scanSession(row pgx.Row) (Session, error) {
	var s Session

	err := row.Scan(&s.ID, &s.OfferID, &s.EntitlementID, &s.ConfigurationID, &s.State, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}

	return s, err
}

func random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hash is what the database keeps of codes and tokens, so a copy of the database does not hold anything a wallet could use.
func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Create starts a session.
func (s *Sessions) Create(ctx context.Context, n NewSession, now time.Time) (Session, error) {
	offerID, err := random()
	if err != nil {
		return Session{}, err
	}

	return scanSession(s.pool.QueryRow(ctx,
		`INSERT INTO oid4vci_sessions (id, offer_id, entitlement_id, credential_configuration_id, code_hash, expires_at, created_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6) RETURNING `+sessionColumns,
		offerID, n.EntitlementID, n.ConfigurationID, hash(s.code(offerID)), now.Add(n.Lifetime), now))
}

func (s *Sessions) FindByOffer(ctx context.Context, offerID string) (Session, error) {
	return scanSession(s.pool.QueryRow(ctx, "SELECT "+sessionColumns+" FROM oid4vci_sessions WHERE offer_id = $1", offerID))
}

// ExchangeCode turns the one-time code into an access token, once.
func (s *Sessions) ExchangeCode(ctx context.Context, code string, now time.Time, tokenLifetime time.Duration) (Session, string, error) {
	token, err := random()
	if err != nil {
		return Session{}, "", err
	}

	session, err := scanSession(s.pool.QueryRow(ctx,
		`UPDATE oid4vci_sessions SET state = 'token', access_token_hash = $2, access_token_expires_at = $3
		 WHERE code_hash = $1 AND state = 'offer' AND expires_at > $4 RETURNING `+sessionColumns,
		hash(code), hash(token), now.Add(tokenLifetime), now))
	if errors.Is(err, ErrNotFound) {
		return Session{}, "", ErrInvalidGrant
	}

	return session, token, err
}

// FindByAccessToken finds the session an access token belongs to, as long as the token is valid and no credential was issued with it yet.
func (s *Sessions) FindByAccessToken(ctx context.Context, token string, now time.Time) (Session, error) {
	return scanSession(s.pool.QueryRow(ctx,
		"SELECT "+sessionColumns+" FROM oid4vci_sessions WHERE access_token_hash = $1 AND state = 'token' AND access_token_expires_at > $2",
		hash(token), now))
}

// Complete records that the credential was issued. It reports false unless the session was waiting for it.
func (s *Sessions) Complete(ctx context.Context, id string, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE oid4vci_sessions SET state = 'completed', completed_at = $2 WHERE id = $1 AND state = 'token'", id, now)
	return tag.RowsAffected() == 1, err
}

// LastCreated is when the entitlement's newest QR code was made; false when it has none.
func (s *Sessions) LastCreated(ctx context.Context, entitlementID string) (time.Time, bool, error) {
	var created time.Time

	err := s.pool.QueryRow(ctx, "SELECT created_at FROM oid4vci_sessions WHERE entitlement_id = $1 ORDER BY created_at DESC LIMIT 1", entitlementID).
		Scan(&created)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}

	return created, err == nil, err
}

// CompletedEntitlements are the entitlements whose credential was issued; used at start to repair an entitlement that was not marked issued.
func (s *Sessions) CompletedEntitlements(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT entitlement_id FROM oid4vci_sessions WHERE state = 'completed'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
