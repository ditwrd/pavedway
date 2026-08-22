package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ditwrd/pavedway/internal/auth"
)

// Issue #23 AC3: a minted session token round-trips — parse returns the
// exact user_id/org_id/role claims the caller signed.
func TestSessions_MintParseRoundTrip(t *testing.T) {
	t.Parallel()

	s := auth.NewSessions("test-secret", 15*time.Minute)

	token, err := s.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	if err != nil {
		t.Fatalf("Mint() error = %v, want nil", err)
	}

	got, err := s.Parse(token, false)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if got.UserID != 42 || got.OrgID != 7 || got.Role != auth.DefaultRole {
		t.Errorf("Parse() = %+v, want user 42 in org 7 with role %q", got, auth.DefaultRole)
	}
}

// Issue #23: an expired session token is rejected by the normal path but
// still parseable by the refresh path (allowExpired) — that is exactly how
// the transparent re-issue flow reads the cookie it is about to replace.
func TestSessions_ExpiredToken_RejectedButRefreshable(t *testing.T) {
	t.Parallel()

	s := auth.NewSessions("test-secret", 1*time.Nanosecond)

	token, err := s.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	if err != nil {
		t.Fatalf("Mint() error = %v, want nil", err)
	}

	time.Sleep(2 * time.Millisecond)

	if _, err := s.Parse(token, false); err == nil {
		t.Error("Parse() error = nil on expired token, want expiry rejection")
	}

	got, err := s.Parse(token, true)
	if err != nil {
		t.Fatalf("Parse(allowExpired) error = %v, want nil", err)
	}

	if got.UserID != 42 {
		t.Errorf("Parse(allowExpired).UserID = %d, want 42", got.UserID)
	}
}

// Issue #23: the signing secret is the session's integrity boundary — a
// different secret (or a tampered token) must never parse.
func TestSessions_TamperedOrWrongSecret_Rejected(t *testing.T) {
	t.Parallel()

	issuer := auth.NewSessions("secret-a", 15*time.Minute)
	other := auth.NewSessions("secret-b", 15*time.Minute)

	token, err := issuer.Mint(auth.Session{UserID: 42, OrgID: 7, Role: auth.DefaultRole})
	if err != nil {
		t.Fatalf("Mint() error = %v, want nil", err)
	}

	if _, err := other.Parse(token, false); err == nil {
		t.Error("Parse() with wrong secret: error = nil, want signature rejection")
	}

	// Flip a payload byte — signature no longer matches.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	payload := []byte(parts[1])
	payload[0] = 'A' + (payload[0]-'A'+1)%26

	parts[1] = string(payload)
	if _, err := issuer.Parse(strings.Join(parts, "."), false); err == nil {
		t.Error("Parse() on tampered token: error = nil, want signature rejection")
	}
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
	if err != nil {
		t.Fatalf("signing foreign token: %v", err)
	}

	if _, err := s.Parse(foreign, false); err == nil {
		t.Error("Parse() on foreign-issuer token: error = nil, want rejection")
	}

	if _, err := s.Parse(foreign, true); err == nil {
		t.Error("Parse(allowExpired) on foreign-issuer token: error = nil, want rejection")
	}
}
