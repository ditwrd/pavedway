// Package auth mints and verifies pavedway's own session tokens: a
// short-lived signed JWT (issue #23) carried in an HttpOnly, Secure cookie,
// completely decoupled from the upstream OIDC provider's tokens. Only the
// OIDC refresh token — stored server-side — connects the two.
package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// golang-jwt/v5 defaults NumericDate to whole-second precision (see
// jwt.TimePrecision), truncating both iat and exp down to the current
// second. Any session TTL under 1s then collapses exp == iat, minting an
// already-expired token; even at normal TTLs it silently loses up to 1s of
// accuracy. This package owns all golang-jwt usage for pavedway's own
// session tokens (OIDC ID-token verification goes through go-oidc/go-jose,
// unaffected), so raising precision here is safe and exact.
func init() {
	jwt.TimePrecision = time.Millisecond
}

// SessionCookie is the name of the cookie that carries the pavedway session
// JWT. HttpOnly + Secure are set by the server; see internal/server.
const SessionCookie = "pavedway_session"

// Issuer identifies pavedway as the minting party of a session token.
const Issuer = "pavedway"

// DefaultRole is the session's role claim until ticket #24 wires
// Casbin-seeded Org/Team roles into the JWT.
const DefaultRole = "viewer"

// Session is the identity the middleware attaches to an authenticated
// request.
type Session struct {
	UserID int64
	OrgID  int64
	Role   string
}

// Sessions signs and verifies session JWTs (HS256). The secret is the HMAC
// key configured by the operator; the TTL keeps each token short-lived so
// compromise of one cookie is bounded.
type Sessions struct {
	secret []byte
	ttl    time.Duration
}

// NewSessions returns a session signer/verifier keyed by secret with the
// given token lifetime.
func NewSessions(secret string, ttl time.Duration) *Sessions {
	return &Sessions{secret: []byte(secret), ttl: ttl}
}

type sessionClaims struct {
	jwt.RegisteredClaims
	UserID int64  `json:"user_id"`
	OrgID  int64  `json:"org_id"`
	Role   string `json:"role"`
}

// Mint signs a fresh session JWT valid for the configured TTL.
func (s *Sessions) Mint(sess Session) (string, error) {
	now := time.Now()
	claims := sessionClaims{
		Issuer:    Issuer,
		Subject:   strconv.FormatInt(sess.UserID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		UserID:    sess.UserID,
		OrgID:     sess.OrgID,
		Role:      sess.Role,
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Parse verifies signature, algorithm, and issuer; unless allowExpired it
// also rejects expired tokens. allowExpired is reserved for the refresh
// endpoint, which must read an expired-but-signed cookie to re-issue it.
func (s *Sessions) Parse(token string, allowExpired bool) (Session, error) {
	opts := []jwt.ParserOption{jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})}
	if allowExpired {
		opts = append(opts, jwt.WithoutClaimsValidation())
	}

	var claims sessionClaims
	if _, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil }, opts...); err != nil {
		return Session{}, err
	}
	// Enforced here (not via parser option) so it holds in both validation
	// modes: the raw upstream provider token must never be accepted.
	if claims.Issuer != Issuer {
		return Session{}, errors.New("session token: unexpected issuer")
	}

	return Session{UserID: claims.UserID, OrgID: claims.OrgID, Role: claims.Role}, nil
}
