package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/ditwrd/pavedway/internal/auth"
	"github.com/ditwrd/pavedway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v5"
	"golang.org/x/oauth2"
)

// OIDC handshake + pavedway-minted session endpoints (issue #23).
//
// Flow: /auth/login redirects to the configured IdP (with state + PKCE);
// /auth/callback exchanges the code, resolves-or-creates the User catalog
// entity, stores the OIDC refresh token server-side, and mints the pavedway
// session JWT into an HttpOnly, Secure cookie. When that short-lived JWT
// expires, /auth/refresh transparently re-issues it via the stored refresh
// token. /auth/logout drops both.

const (
	stateCookie = "pavedway_oidc_state"
	pkceCookie  = "pavedway_oidc_pkce"

	// sessionContextKey carries the parsed Session to handlers behind the
	// auth middleware.
	sessionContextKey = "pavedway.session"
)

// cookieLifetime keeps the session cookie around far beyond the JWT TTL so
// the browser keeps presenting the expired token and the refresh endpoint
// swaps it for a fresh one in place. Refresh resets the lifetime.
// ponytail: fixed 7-day cookie, operator-tunable if anyone asks.
const cookieLifetime = 7 * 24 * time.Hour

func (h *handlers) authEnabled() bool { return h.provider != nil }

// requireOIDCEnabled rejects every /auth/* route with 503 when the server
// was booted without an OIDC issuer, instead of repeating the same guard in
// each handler.
func (h *handlers) requireOIDCEnabled(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if !h.authEnabled() {
			return c.JSON(http.StatusServiceUnavailable, errorMessage("OIDC is not configured"))
		}

		return next(c)
	}
}

// GET /api/v1/auth/login — start the authorization-code flow.
func (h *handlers) login(c *echo.Context) error {
	state, err := randomHex(32)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	verifier := oauth2.GenerateVerifier()

	setStateCookie(c, stateCookie, state)
	setStateCookie(c, pkceCookie, verifier)

	return c.Redirect(http.StatusFound, h.oauth2Cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)))
}

// oidcHandshake validates the callback's state query param against the
// state cookie — the CSRF guard that stops an attacker from injecting
// their own authorization code into a victim's session — and returns the
// PKCE verifier cookie and the authorization code. Errors are already
// client-safe messages.
func oidcHandshake(c *echo.Context) (pkce, code string, err error) {
	state := c.QueryParam("state")

	stateCookieVal, err := c.Cookie(stateCookie)
	stateValid := err == nil && stateCookieVal.Value != "" && stateCookieVal.Value == state

	if !stateValid {
		return "", "", errors.New("invalid state")
	}

	pkceCookieVal, err := c.Cookie(pkceCookie)
	if err != nil {
		return "", "", errors.New("missing PKCE verifier")
	}

	code = c.QueryParam("code")
	if code == "" {
		return "", "", errors.New("missing authorization code")
	}

	return pkceCookieVal.Value, code, nil
}

// GET /api/v1/auth/callback — IdP redirect target.
func (h *handlers) callback(c *echo.Context) error {
	ctx := c.Request().Context()

	pkce, code, err := oidcHandshake(c)
	if err != nil {
		clearStateCookies(c)
		return c.JSON(http.StatusBadRequest, errorMessage(err.Error()))
	}
	defer clearStateCookies(c)

	oauth2Token, err := h.oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(pkce))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse(err))
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return c.JSON(http.StatusBadRequest, errorMessage("token response has no id_token"))
	}

	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, errorResponse(err))
	}

	org, err := h.q.GetOrganization(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.JSON(http.StatusServiceUnavailable, errorMessage("organization is not bootstrapped"))
		}

		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	user, err := h.resolveAndPersist(ctx, org.ID, idToken, oauth2Token)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	sessionToken, err := h.sessions.Mint(auth.Session{UserID: user.ID, OrgID: org.ID, Role: auth.DefaultRole})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	setSessionCookie(c, sessionToken)

	return c.Redirect(http.StatusFound, "/")
}

// resolveAndPersist resolves-or-creates the User catalog entity for the
// OIDC identity and stores the provider's refresh token (when the provider
// hands one out) for the AC4 refresh flow.
func (h *handlers) resolveAndPersist(ctx context.Context, orgID int64, idToken *oidc.IDToken, oauth2Token *oauth2.Token) (store.Entity, error) {
	user, err := h.resolveUser(ctx, orgID, idToken)
	if err != nil {
		return store.Entity{}, err
	}

	if rt := oauth2Token.RefreshToken; rt != "" {
		if _, err := h.q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
			OrgID:        orgID,
			UserID:       user.ID,
			Provider:     h.issuer,
			Subject:      idToken.Subject,
			RefreshToken: rt,
		}); err != nil {
			return store.Entity{}, err
		}
	}

	return user, nil
}

// resolveUser finds the User catalog entity for an OIDC identity, creating
// it on first login. Resolution is by the verified email (the entity name);
// the refresh_tokens row keeps the provider subject ↔ user link.
func (h *handlers) resolveUser(ctx context.Context, orgID int64, idToken *oidc.IDToken) (store.Entity, error) {
	name := userEntityName(idToken)
	params := store.GetEntityParams{OrgID: orgID, Kind: "User", Namespace: "default", Name: name}

	ent, err := h.q.GetEntity(ctx, params)
	if err == nil {
		return ent, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return store.Entity{}, err
	}

	ent, err = h.q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID:     orgID,
		Kind:      "User",
		Namespace: "default",
		Name:      name,
		Metadata:  json.RawMessage(`{}`),
		Spec:      json.RawMessage(`{}`),
	})
	if err == nil {
		return ent, nil
	}
	// Two concurrent first logins for the same email race the UNIQUE
	// constraint; the loser reads back the winner's row.
	if isUniqueViolation(err) {
		return h.q.GetEntity(ctx, params)
	}

	return store.Entity{}, err
}

// userEntityName maps an OIDC identity to the catalog User entity name: the
// verified email, lowercased, falling back to the subject — the only claim
// OIDC guarantees to exist. OIDC Core §5.4 makes email_verified a
// precondition for treating email as identity proof; an unverified (or
// spoofed) email claim must not name a User entity, or a first login could
// squat an existing verified identity.
func userEntityName(idToken *oidc.IDToken) string {
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}

	_ = idToken.Claims(&claims)
	if claims.Email != "" && claims.EmailVerified {
		return strings.ToLower(claims.Email)
	}

	return idToken.Subject
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// POST /api/v1/auth/refresh — transparently re-issue an expired session via
// the stored OIDC refresh token (issue #23 AC4). The cookie's JWT may
// already be expired — that is the trigger — so only signature and issuer
// must still hold.
func (h *handlers) refresh(c *echo.Context) error {
	ctx := c.Request().Context()

	cookie, err := c.Cookie(auth.SessionCookie)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, errorMessage("missing session cookie"))
	}

	sess, err := h.sessions.Parse(cookie.Value, true)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, errorResponse(err))
	}

	sessionToken, err := h.refreshSession(ctx, sess)
	if err != nil {
		// The old cookie is dead either way — no stored token, a revoked
		// token, or an identity the IdP no longer vouches for — so clear
		// it and force a fresh login.
		clearSessionCookie(c)

		if errors.Is(err, pgx.ErrNoRows) {
			return c.JSON(http.StatusUnauthorized, errorMessage("no refresh token for session"))
		}

		return c.JSON(http.StatusUnauthorized, errorResponse(err))
	}

	setSessionCookie(c, sessionToken)

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// refreshSession re-issues a session via the stored OIDC refresh token.
// Shared by the /auth/refresh endpoint and the requireAuth middleware so an
// expired cookie is re-issued transparently in place.
//
// The refreshed response's id_token is optional per OIDC Core §12.2, so it
// is verified only when present; when it is, its subject must still match
// the stored one — a provider handing back a different identity is rejected
// instead of silently minting a session for it.
func (h *handlers) refreshSession(ctx context.Context, sess auth.Session) (string, error) {
	rt, err := h.q.GetRefreshToken(ctx, store.GetRefreshTokenParams{OrgID: sess.OrgID, UserID: sess.UserID})
	if err != nil {
		return "", fmt.Errorf("loading refresh token: %w", err)
	}

	newToken, err := h.oauth2Cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: rt.RefreshToken}).Token()
	if err != nil {
		// The provider refused the refresh token (revoked/expired): the
		// session is dead — drop the stored token and force re-login.
		if _, delErr := h.q.DeleteRefreshToken(ctx, store.DeleteRefreshTokenParams{OrgID: sess.OrgID, UserID: sess.UserID}); delErr != nil {
			slog.Error("refresh: drop stale refresh token", "org_id", sess.OrgID, "user_id", sess.UserID, "err", delErr)
		}

		return "", fmt.Errorf("refreshing OIDC token: %w", err)
	}

	// Verify the refreshed ID token too — the oauth2 client only checks the
	// HTTP exchange, not that the IdP still vouches for this identity.
	if rawIDToken, ok := newToken.Extra("id_token").(string); ok {
		refreshed, err := h.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			return "", err
		}

		if refreshed.Subject != rt.Subject {
			return "", fmt.Errorf("refreshed id_token subject %q does not match stored %q", refreshed.Subject, rt.Subject)
		}
	}

	// Some providers rotate the refresh token on use; persist the new one.
	if newToken.RefreshToken != "" && newToken.RefreshToken != rt.RefreshToken {
		if _, err := h.q.UpsertRefreshToken(ctx, store.UpsertRefreshTokenParams{
			OrgID:        sess.OrgID,
			UserID:       sess.UserID,
			Provider:     h.issuer,
			Subject:      rt.Subject,
			RefreshToken: newToken.RefreshToken,
		}); err != nil {
			return "", err
		}
	}

	return h.sessions.Mint(auth.Session{UserID: sess.UserID, OrgID: sess.OrgID, Role: sess.Role})
}

// POST /api/v1/auth/logout — drop the refresh token and clear the cookie.
func (h *handlers) logout(c *echo.Context) error {
	ctx := c.Request().Context()

	cookie, err := c.Cookie(auth.SessionCookie)
	if err != nil {
		clearSessionCookie(c)
		return c.NoContent(http.StatusNoContent)
	}

	// Best-effort cleanup: a forged or expired session cookie is dropped
	// silently — logout still clears the cookie and returns 204.
	if sess, parseErr := h.sessions.Parse(cookie.Value, true); parseErr == nil {
		if _, delErr := h.q.DeleteRefreshToken(ctx, store.DeleteRefreshTokenParams{OrgID: sess.OrgID, UserID: sess.UserID}); delErr != nil {
			slog.Error("logout: drop refresh token", "org_id", sess.OrgID, "user_id", sess.UserID, "err", delErr)
		}
	}

	clearSessionCookie(c)

	return c.NoContent(http.StatusNoContent)
}

// requireAuth rejects requests without a valid session cookie and exposes
// the parsed Session to downstream handlers (issue #23 AC5). An expired but
// still-signed session is re-issued transparently via the OIDC refresh flow
// (AC4) before the request is rejected, so a client never sees a 401 while
// its refresh token is still valid.
func (h *handlers) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		cookie, err := c.Cookie(auth.SessionCookie)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, errorMessage("missing session cookie"))
		}

		sess, err := h.sessions.Parse(cookie.Value, false)
		if err == nil {
			c.Set(sessionContextKey, sess)
			return next(c)
		}
		// Strict parse failed: distinguish a merely expired token (a
		// refresh candidate) from a forged/garbage one (reject outright).
		expired, parseErr := h.sessions.Parse(cookie.Value, true)
		if parseErr != nil {
			return c.JSON(http.StatusUnauthorized, errorResponse(parseErr))
		}

		sessionToken, refreshErr := h.refreshSession(c.Request().Context(), expired)
		if refreshErr != nil {
			clearSessionCookie(c)
			return c.JSON(http.StatusUnauthorized, errorResponse(refreshErr))
		}

		setSessionCookie(c, sessionToken)
		c.Set(sessionContextKey, expired)

		return next(c)
	}
}

// orgID returns the org scoping the request: the session's org when auth is
// enabled, otherwise the single bootstrapped org (pre-auth dev mode).
func (h *handlers) orgID(c *echo.Context) (int64, error) {
	if sess, ok := c.Get(sessionContextKey).(auth.Session); ok {
		return sess.OrgID, nil
	}

	org, err := h.q.GetOrganization(c.Request().Context())
	if err != nil {
		return 0, err
	}

	return org.ID, nil
}

// cookie builds a pavedway auth cookie: HttpOnly + Secure + Lax so the
// browser never exposes it to JS and only same-site requests carry it.
// A negative MaxAge expires the cookie immediately.
func cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
		MaxAge: maxAge,
	}
}

func setStateCookie(c *echo.Context, name, value string) {
	// state/verifier live only for the redirect round trip
	http.SetCookie(c.Response(), cookie(name, value, 600))
}

func clearStateCookies(c *echo.Context) {
	for _, name := range []string{stateCookie, pkceCookie} {
		http.SetCookie(c.Response(), cookie(name, "", -1))
	}
}

func setSessionCookie(c *echo.Context, token string) {
	http.SetCookie(c.Response(), cookie(auth.SessionCookie, token, int(cookieLifetime.Seconds())))
}

func clearSessionCookie(c *echo.Context) {
	http.SetCookie(c.Response(), cookie(auth.SessionCookie, "", -1))
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}
