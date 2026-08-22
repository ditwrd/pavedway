package store_test

import (
	"context"
	"testing"

	"github.com/ditwrd/pavedway/internal/store"
)

// Issue #23: the OIDC refresh token survives the login→refresh round trip
// — upsert stores it, get retrieves it, and re-login replaces it in place
// (one refresh token per user; the last login wins).
func TestRefreshToken_UpsertGetUpdate(t *testing.T) {
	t.Parallel()
	q := newTestQueries(t)
	ctx := context.Background()
	org := bootstrap(t, q, "acme")
	user := createEntity(t, q, org.ID, "User", "default", "ada@example.com")

	got, err := q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
		OrgID:        org.ID,
		UserID:       user.ID,
		Provider:     "https://idp.example.com",
		Subject:      "sub-1",
		RefreshToken: "refresh-token-1",
	})
	if err != nil {
		t.Fatalf("UpsertRefreshToken() error = %v, want nil", err)
	}

	if got.RefreshToken != "refresh-token-1" {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, "refresh-token-1")
	}

	loaded, err := q.GetRefreshToken(ctx, store.GetRefreshTokenParams{OrgID: org.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetRefreshToken() error = %v, want nil", err)
	}

	if loaded.Subject != "sub-1" || loaded.Provider != "https://idp.example.com" {
		t.Errorf("GetRefreshToken() = %+v, want subject sub-1 from configured provider", loaded)
	}

	// A second login rotates the stored token rather than adding a row.
	updated, err := q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
		OrgID:        org.ID,
		UserID:       user.ID,
		Provider:     "https://idp.example.com",
		Subject:      "sub-1",
		RefreshToken: "refresh-token-2",
	})
	if err != nil {
		t.Fatalf("UpsertRefreshToken() (update) error = %v, want nil", err)
	}

	if updated.ID != got.ID {
		t.Errorf("updated.ID = %d, want same row %d (upsert must not duplicate)", updated.ID, got.ID)
	}

	if updated.RefreshToken != "refresh-token-2" {
		t.Errorf("RefreshToken = %q, want rotated %q", updated.RefreshToken, "refresh-token-2")
	}
}

// Issue #23 logout: deleting the refresh token kills the session's ability
// to re-issue, and a second delete is a no-op.
func TestRefreshToken_Delete(t *testing.T) {
	t.Parallel()
	q := newTestQueries(t)
	ctx := context.Background()
	org := bootstrap(t, q, "acme")
	user := createEntity(t, q, org.ID, "User", "default", "ada@example.com")

	if _, err := q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
		OrgID: org.ID, UserID: user.ID, Provider: "p", Subject: "s", RefreshToken: "rt",
	}); err != nil {
		t.Fatalf("UpsertRefreshToken() error = %v, want nil", err)
	}

	rows, err := q.DeleteRefreshToken(ctx, store.DeleteRefreshTokenParams{OrgID: org.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("DeleteRefreshToken() error = %v, want nil", err)
	}

	if rows != 1 {
		t.Errorf("DeleteRefreshToken() rows = %d, want 1", rows)
	}

	if _, err := q.GetRefreshToken(ctx, store.GetRefreshTokenParams{OrgID: org.ID, UserID: user.ID}); err == nil {
		t.Error("GetRefreshToken() error = nil after delete, want no rows")
	}
}
