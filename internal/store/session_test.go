package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err, "UpsertRefreshToken()")

	assert.Equal(t, "refresh-token-1", got.RefreshToken, "RefreshToken")

	loaded, err := q.GetRefreshToken(ctx, store.GetRefreshTokenParams{OrgID: org.ID, UserID: user.ID})
	require.NoError(t, err, "GetRefreshToken()")

	assert.Equal(t, "sub-1", loaded.Subject, "GetRefreshToken() subject (want from configured provider)")
	assert.Equal(t, "https://idp.example.com", loaded.Provider, "GetRefreshToken() provider")

	// A second login rotates the stored token rather than adding a row.
	updated, err := q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
		OrgID:        org.ID,
		UserID:       user.ID,
		Provider:     "https://idp.example.com",
		Subject:      "sub-1",
		RefreshToken: "refresh-token-2",
	})
	require.NoError(t, err, "UpsertRefreshToken() (update)")

	assert.Equal(t, got.ID, updated.ID, "updated.ID (upsert must not duplicate)")

	assert.Equal(t, "refresh-token-2", updated.RefreshToken, "RefreshToken (want rotated)")
}

// Issue #23 logout: deleting the refresh token kills the session's ability
// to re-issue, and a second delete is a no-op.
func TestRefreshToken_Delete(t *testing.T) {
	t.Parallel()
	q := newTestQueries(t)
	ctx := context.Background()
	org := bootstrap(t, q, "acme")
	user := createEntity(t, q, org.ID, "User", "default", "ada@example.com")

	_, err := q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
		OrgID: org.ID, UserID: user.ID, Provider: "p", Subject: "s", RefreshToken: "rt",
	})
	require.NoError(t, err, "UpsertRefreshToken()")

	rows, err := q.DeleteRefreshToken(ctx, store.DeleteRefreshTokenParams{OrgID: org.ID, UserID: user.ID})
	require.NoError(t, err, "DeleteRefreshToken()")

	assert.Equal(t, int64(1), rows, "DeleteRefreshToken() rows")

	_, err = q.GetRefreshToken(ctx, store.GetRefreshTokenParams{OrgID: org.ID, UserID: user.ID})
	assert.Error(t, err, "GetRefreshToken() after delete (want no rows)")
}
