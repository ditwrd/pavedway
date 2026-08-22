package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/auth"
)

// Issue #23 AC3: a minted session token round-trips — parse returns the
// exact user_id/org_id/role claims the caller signed.
func TestSessions_MintParseRoundTrip(t *testing.T) {
	t.Parallel()

	s := auth.NewSessions("test-secret", 15*time.Minute)

	token, err := s.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	require.NoError(t, err, "Mint() error")

	got, err := s.Parse(token, false)
	require.NoError(t, err, "Parse() error")

	assert.Equal(t, auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole}, got, "Parse()")
}

// Issue #23: an expired session token is rejected by the normal path but
// still parseable by the refresh path (allowExpired) — that is exactly how
// the transparent re-issue flow reads the cookie it is about to replace.
func TestSessions_ExpiredToken_RejectedButRefreshable(t *testing.T) {
	t.Parallel()

	s := auth.NewSessions("test-secret", 1*time.Nanosecond)

	token, err := s.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	require.NoError(t, err, "Mint() error")

	time.Sleep(2 * time.Millisecond)

	_, err = s.Parse(token, false)
	require.Error(t, err, "Parse() on expired token")

	got, err := s.Parse(token, true)
	require.NoError(t, err, "Parse(allowExpired) error")

	assert.Equal(t, int64(42), got.UserID, "Parse(allowExpired).UserID")
}

// Issue #23: the signing secret is the session's integrity boundary — a
// different secret (or a tampered token) must never parse.
func TestSessions_TamperedOrWrongSecret_Rejected(t *testing.T) {
	t.Parallel()

	issuer := auth.NewSessions("secret-a", 15*time.Minute)
	other := auth.NewSessions("secret-b", 15*time.Minute)

	token, err := issuer.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	require.NoError(t, err, "Mint() error")

	_, err = other.Parse(token, false)
	require.Error(t, err, "Parse() with wrong secret")

	// Flip a payload byte — signature no longer matches.
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "token segments")

	payload := []byte(parts[1])
	payload[0] = 'A' + (payload[0]-'A'+1)%26

	parts[1] = string(payload)
	_, err = issuer.Parse(strings.Join(parts, "."), false)
	assert.Error(t, err, "Parse() on tampered token")
}

// Issue #23: a token minted by some other issuer (e.g. the upstream OIDC
// provider's raw token) is not a pavedway session, even if it happens to be
// signed with the same algorithm.
func TestSessions_ForeignIssuer_Rejected(t *testing.T) {
	t.Parallel()

	s := auth.NewSessions("test-secret", 15*time.Minute)

	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://idp.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "42",
	}).SignedString([]byte("test-secret"))
	require.NoError(t, err, "signing foreign token")

	_, err = s.Parse(foreign, false)
	require.Error(t, err, "Parse() on foreign-issuer token")

	_, err = s.Parse(foreign, true)
	assert.Error(t, err, "Parse(allowExpired) on foreign-issuer token")
}
