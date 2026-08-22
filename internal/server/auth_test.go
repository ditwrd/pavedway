package server_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/auth"
	"github.com/ditwrd/pavedway/internal/config"
)

// testClientID is the OIDC client id the test server registers with the
// fake IdP; the fake IdP must see it as the token audience.
const testClientID = "pavedway-test"

const testSessionSecret = "test-session-secret"

// fakeIDP is a minimal standards-compliant OIDC provider for tests: it
// serves discovery, a JWKS endpoint, and a token endpoint, minting ES256 ID
// tokens. Tests drive the real go-oidc client path (discovery → JWKS →
// verification) end to end instead of mocking it.
type fakeIDP struct {
	srv          *httptest.Server
	key          *ecdsa.PrivateKey
	mu           sync.Mutex
	codes        map[string]fakeIdentity
	refreshes    map[string]fakeIdentity
	refreshCalls int

	// omitIDTokenOnRefresh makes refresh grants omit the id_token — OIDC
	// Core §12.2 makes it optional there.
	omitIDTokenOnRefresh bool
	// overrideRefreshSubject replaces the subject of refresh-grant ID
	// tokens, simulating a provider that hands back a different identity.
	overrideRefreshSubject string
}

type fakeIdentity struct {
	subject       string
	email         string
	emailVerified bool
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate test key")

	f := &fakeIDP{
		key:       key,
		codes:     map[string]fakeIdentity{},
		refreshes: map[string]fakeIdentity{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.handleDiscovery)
	mux.HandleFunc("/keys", f.handleJWKS)
	mux.HandleFunc("/token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeIDP) issuer() string { return f.srv.URL }

// registerUser pre-registers a login: the returned authorization code
// exchanges for an ID token identifying the user, and the returned refresh
// token keeps the refresh grant working.
func (f *fakeIDP) registerUser(subject, email string) (code, refreshToken string) {
	code = randomHex(16)
	refreshToken = randomHex(32)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.codes[code] = fakeIdentity{subject: subject, email: email, emailVerified: true}
	f.refreshes[refreshToken] = fakeIdentity{subject: subject, email: email, emailVerified: true}

	return code, refreshToken
}

// registerUnverifiedUser pre-registers a login whose email claim is NOT
// verified (email_verified: false) — OIDC Core §5.4 means such an email is
// not identity proof.
func (f *fakeIDP) registerUnverifiedUser(subject, email string) (code, refreshToken string) {
	code = randomHex(16)
	refreshToken = randomHex(32)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.codes[code] = fakeIdentity{subject: subject, email: email}
	f.refreshes[refreshToken] = fakeIdentity{subject: subject, email: email}

	return code, refreshToken
}

// revokeAllRefreshTokens simulates the IdP invalidating every refresh token
// (revocation, expiry) — the next refresh must fail.
func (f *fakeIDP) revokeAllRefreshTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.refreshes = map[string]fakeIdentity{}
}

func (f *fakeIDP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                f.srv.URL,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
	})
}

func (f *fakeIDP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	// Uncompressed SEC1 point: 0x04 || X(32) || Y(32) for P-256.
	point, err := f.key.PublicKey.Bytes()
	if err != nil {
		http.Error(w, "public key bytes", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "EC", "crv": "P-256", "kid": "test-key", "use": "sig", "alg": "ES256",
			"x": base64.RawURLEncoding.EncodeToString(point[1:33]),
			"y": base64.RawURLEncoding.EncodeToString(point[33:]),
		}},
	})
}

func (f *fakeIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.Form.Get("grant_type") {
	case "authorization_code":
		id, ok := f.codes[r.Form.Get("code")]
		if !ok {
			writeTokenError(w, "invalid_grant")
			return
		}

		writeJSON(w, f.tokenResponse(id))
	case "refresh_token":
		id, ok := f.refreshes[r.Form.Get("refresh_token")]
		if !ok {
			writeTokenError(w, "invalid_grant")
			return
		}

		f.refreshCalls++
		if f.omitIDTokenOnRefresh {
			writeJSON(w, f.tokenResponseNoIDToken(id))
			return
		}

		if f.overrideRefreshSubject != "" {
			id.subject = f.overrideRefreshSubject
		}

		writeJSON(w, f.tokenResponse(id))
	default:
		writeTokenError(w, "unsupported_grant_type")
	}
}

// tokenResponse issues a fresh ID token and rotates the refresh token —
// every grant hands out a new one (registered so the next refresh works),
// exercising the client's rotation-persistence path.
func (f *fakeIDP) tokenResponse(id fakeIdentity) map[string]any {
	refreshToken := randomHex(32)
	f.refreshes[refreshToken] = id

	return map[string]any{
		"access_token":  randomHex(16),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"id_token":      f.mintIDToken(id),
		"refresh_token": refreshToken,
	}
}

// tokenResponseNoIDToken is tokenResponse without the id_token — what a
// standards-compliant provider may legally return on a refresh grant
// (OIDC Core §12.2).
func (f *fakeIDP) tokenResponseNoIDToken(id fakeIdentity) map[string]any {
	refreshToken := randomHex(32)
	f.refreshes[refreshToken] = id

	return map[string]any{
		"access_token":  randomHex(16),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": refreshToken,
	}
}

func (f *fakeIDP) mintIDToken(id fakeIdentity) string {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss":            f.srv.URL,
		"aud":            testClientID,
		"sub":            id.subject,
		"email":          id.email,
		"email_verified": id.emailVerified,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "test-key"

	signed, err := tok.SignedString(f.key)
	if err != nil {
		panic(err)
	}

	return signed
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func writeTokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b)
}

// authCfg returns a server config pointed at the fake IdP with the given
// session JWT TTL.
func authCfg(idp *fakeIDP, ttl time.Duration) config.Config {
	return config.Config{
		OIDC: config.OIDCConfig{
			Issuer:       idp.issuer(),
			ClientID:     testClientID,
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://pavedway.test/api/v1/auth/callback",
			Scopes:       []string{"openid", "email", "offline_access"},
		},
		Session: config.SessionConfig{Secret: testSessionSecret, TTL: ttl},
	}
}

// login runs the full browser flow: start at /auth/login, follow the
// redirect params back into /auth/callback, and return the session cookie.
func login(t *testing.T, e *echo.Echo, code string) *http.Cookie {
	t.Helper()

	rec := doRequest(t, e, http.MethodGet, "/api/v1/auth/login", "")
	require.Equal(t, http.StatusFound, rec.Code, "GET /auth/login")

	loc, err := url.Parse(rec.Result().Header.Get("Location"))
	require.True(t, err == nil && loc.Query().Get("state") != "", "login redirect Location = %q, want IdP URL with state", rec.Result().Header.Get("Location"))

	stateCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_state")

	pkceCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_pkce")
	require.True(t, stateCookie != nil && pkceCookie != nil, "login did not set state and PKCE cookies")

	cb := doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/auth/callback?code="+code+"&state="+loc.Query().Get("state"),
		"", stateCookie, pkceCookie)
	require.Equal(t, http.StatusFound, cb.Code, "GET /auth/callback")

	sess := findCookie(cb.Result().Cookies(), auth.SessionCookie)
	require.NotNil(t, sess, "callback did not set session cookie")

	return sess
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}

	return nil
}

// validSession reports whether decoded claims describe a live session.
func validSession(claims decodedSession) bool {
	return claims.UserID != 0 && claims.ExpiresAt != nil && claims.ExpiresAt.After(time.Now())
}

type decodedSession struct {
	jwt.RegisteredClaims
	UserID int64  `json:"user_id"`
	OrgID  int64  `json:"org_id"`
	Role   string `json:"role"`
}

func decodeSession(t *testing.T, token string) decodedSession {
	t.Helper()

	var claims decodedSession

	parsed, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return []byte(testSessionSecret), nil },
		jwt.WithValidMethods([]string{"HS256"}))
	require.NoError(t, err, "decoding session cookie")

	require.True(t, parsed.Valid, "session cookie JWT invalid")

	return claims
}

// Issue #23 AC2+AC3: completing the OIDC flow creates the User entity in
// the bootstrapped org and sets an HttpOnly, Secure cookie containing a
// pavedway-minted JWT (user_id/org_id/role claims), not the upstream token.
func TestOIDC_LoginCreatesUserAndMintsSession(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))

	// The org must exist before login (first-run bootstrap).
	rec := doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)
	require.Equal(t, http.StatusCreated, rec.Code, "POST /bootstrap")

	code, _ := idp.registerUser("sub-ada", "Ada@Example.COM")
	sess := login(t, e, code)

	assert.True(t, sess.HttpOnly, "session cookie is not HttpOnly")

	assert.True(t, sess.Secure, "session cookie is not Secure")

	claims := decodeSession(t, sess.Value)
	assert.Equal(t, auth.Issuer, claims.Issuer, "session issuer (not the IdP token)")

	assert.True(t, claims.UserID != 0 && claims.OrgID != 0, "session claims = %+v, want non-zero user_id/org_id", claims)

	assert.NotEmpty(t, claims.Role, "session role claim is empty")

	// The User entity exists in the catalog under the (lowercased) email,
	// readable through the API with the session cookie.
	rec = doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/entities/User/default/ada@example.com", "", sess)
	require.Equal(t, http.StatusOK, rec.Code, "GET User entity with session (body: %s)", rec.Body)

	var got struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}

	err := json.Unmarshal(rec.Body.Bytes(), &got)
	require.NoError(t, err, "unmarshal entity response")

	assert.Equal(t, "User", got.Kind, "entity")
	assert.Equal(t, "ada@example.com", got.Name, "entity")
}

// Issue #23 AC2: a second login for the same identity resolves the existing
// User entity instead of minting a duplicate.
func TestOIDC_SecondLoginResolvesExistingUser(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code1, _ := idp.registerUser("sub-ada", "ada@example.com")
	code2, _ := idp.registerUser("sub-ada", "ada@example.com")
	first := decodeSession(t, login(t, e, code1).Value)
	second := decodeSession(t, login(t, e, code2).Value)

	assert.Equal(t, first.UserID, second.UserID, "second login UserID (existing user must resolve)")
}

// Issue #23 AC2: an unverified email is not identity proof (OIDC Core
// §5.4) — the User entity is named by the subject instead, so a spoofed
// email claim cannot squat an existing verified identity.
func TestOIDC_UnverifiedEmailFallsBackToSubject(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code, _ := idp.registerUnverifiedUser("sub-ada", "ada@example.com")
	sess := login(t, e, code)

	rec := doRequestWithCookies(t, e, http.MethodGet, "/api/v1/entities/User/default/sub-ada", "", sess)
	require.Equal(t, http.StatusOK, rec.Code, "GET User entity by subject (body: %s)", rec.Body)

	rec = doRequestWithCookies(t, e, http.MethodGet, "/api/v1/entities/User/default/ada@example.com", "", sess)
	require.Equal(t, http.StatusNotFound, rec.Code, "GET User entity by unverified email")
}

// Issue #23 AC4: an expired session JWT is transparently re-issued via the
// OIDC refresh flow — the expired request itself succeeds and a fresh
// cookie replaces the stale one, with no re-login and no manual refresh
// call. The explicit /auth/refresh endpoint keeps working too.
func TestOIDC_RefreshReissuesExpiredSession(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	// Short TTL: the session is expired shortly after login.
	e := newTestServerCfg(t, authCfg(idp, 200*time.Millisecond))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code, _ := idp.registerUser("sub-ada", "ada@example.com")
	sess := login(t, e, code)

	time.Sleep(300 * time.Millisecond) // let the session JWT lapse

	// The protected request transparently re-issues the session in place.
	rec := doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/entities/User/default/ada@example.com", "", sess)
	require.Equal(t, http.StatusOK, rec.Code, "expired session GET (auto-refresh; body: %s)", rec.Body)

	auto := findCookie(rec.Result().Cookies(), auth.SessionCookie)
	require.NotNil(t, auto, "auto-refresh did not set a new session cookie")

	claims := decodeSession(t, auto.Value)
	assert.True(t, validSession(claims), "auto-refreshed session claims = %+v, want valid user with future expiry", claims)

	// The explicit refresh endpoint still re-issues from the old expired
	// cookie.
	ref := doRequestWithCookies(t, e, http.MethodPost, "/api/v1/auth/refresh", "", sess)
	require.Equal(t, http.StatusOK, ref.Code, "POST /auth/refresh (body: %s)", ref.Body)

	newSess := findCookie(ref.Result().Cookies(), auth.SessionCookie)
	require.NotNil(t, newSess, "refresh did not set a new session cookie")

	claims = decodeSession(t, newSess.Value)
	assert.True(t, validSession(claims), "refreshed session claims = %+v, want valid user with future expiry", claims)

	// The re-issued cookie works on the protected endpoint immediately.
	rec2 := doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/entities/User/default/ada@example.com", "", newSess)
	require.Equal(t, http.StatusOK, rec2.Code, "GET after refresh")

	idp.mu.Lock()
	calls := idp.refreshCalls
	idp.mu.Unlock()

	assert.NotZero(t, calls, "refresh flow never reached the IdP token endpoint")
}

// Issue #23 AC4 negative: when the provider no longer accepts the refresh
// token, refresh fails, the dead token is dropped, and the cookie cleared —
// forcing a fresh login rather than silently keeping a broken session.
func TestOIDC_RefreshFailsWhenRefreshTokenInvalid(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 200*time.Millisecond))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code, _ := idp.registerUser("sub-ada", "ada@example.com")
	sess := login(t, e, code)

	idp.revokeAllRefreshTokens()
	time.Sleep(300 * time.Millisecond)

	ref := doRequestWithCookies(t, e, http.MethodPost, "/api/v1/auth/refresh", "", sess)
	require.Equal(t, http.StatusUnauthorized, ref.Code, "POST /auth/refresh with revoked token")

	cleared := findCookie(ref.Result().Cookies(), auth.SessionCookie)
	assert.True(t, cleared != nil && cleared.MaxAge < 0, "session cookie not cleared after failed refresh")
}

// Issue #23 AC4: OIDC Core §12.2 makes the id_token optional in refresh
// responses — a provider that omits it must not break the refresh flow.
func TestOIDC_RefreshSucceedsWithoutIDToken(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 200*time.Millisecond))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code, _ := idp.registerUser("sub-ada", "ada@example.com")
	sess := login(t, e, code)

	// Login already happened with an id_token; make the *refresh* grant
	// omit it, as a compliant provider may.
	idp.mu.Lock()
	idp.omitIDTokenOnRefresh = true
	idp.mu.Unlock()
	time.Sleep(300 * time.Millisecond)

	ref := doRequestWithCookies(t, e, http.MethodPost, "/api/v1/auth/refresh", "", sess)
	require.Equal(t, http.StatusOK, ref.Code, "POST /auth/refresh without id_token (body: %s)", ref.Body)

	newSess := findCookie(ref.Result().Cookies(), auth.SessionCookie)
	require.NotNil(t, newSess, "refresh did not set a new session cookie")

	claims := decodeSession(t, newSess.Value)
	assert.True(t, validSession(claims), "refreshed session claims = %+v, want valid user with future expiry", claims)
}

// Issue #23 AC4: if the provider returns a different subject on refresh,
// the re-issue must be refused — the refreshed token would vouch for a
// different identity than the session was minted for.
func TestOIDC_RefreshRejectsSubjectMismatch(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 200*time.Millisecond))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	code, _ := idp.registerUser("sub-ada", "ada@example.com")
	sess := login(t, e, code)

	idp.mu.Lock()
	idp.overrideRefreshSubject = "sub-mallory"
	idp.mu.Unlock()
	time.Sleep(300 * time.Millisecond)

	ref := doRequestWithCookies(t, e, http.MethodPost, "/api/v1/auth/refresh", "", sess)
	require.Equal(t, http.StatusUnauthorized, ref.Code, "POST /auth/refresh with mismatched subject")

	cleared := findCookie(ref.Result().Cookies(), auth.SessionCookie)
	assert.True(t, cleared != nil && cleared.MaxAge < 0, "session cookie not cleared after subject mismatch")
}

// Issue #23 AC5: every catalog endpoint requires a valid session once OIDC
// is configured — missing, garbage, or expired cookies are all rejected.
func TestOIDC_ProtectedEndpointsRejectNoSession(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	rec := doRequest(t, e, http.MethodGet, "/api/v1/entities/Component/default/foo", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code, "GET entity without session")

	rec = doRequest(t, e, http.MethodPost, "/api/v1/entities",
		`{"kind":"Component","namespace":"default","name":"foo","metadata":{},"spec":{}}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "POST entity without session")

	rec = doRequestWithCookies(t, e, http.MethodGet, "/api/v1/entities/Component/default/foo", "",
		&http.Cookie{Name: auth.SessionCookie, Value: "not-a-jwt", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	require.Equal(t, http.StatusUnauthorized, rec.Code, "GET entity with garbage cookie")
}

// Issue #23: the callback refuses a state mismatch — the CSRF guard against
// an attacker injecting their own authorization code.
func TestOIDC_CallbackRejectsWrongState(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))

	code, _ := idp.registerUser("sub-eve", "eve@example.com")
	rec := doRequest(t, e, http.MethodGet, "/api/v1/auth/login", "")
	stateCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_state")
	pkceCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_pkce")

	cb := doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/auth/callback?code="+code+"&state=attacker-chosen-state",
		"", stateCookie, pkceCookie)
	require.Equal(t, http.StatusBadRequest, cb.Code, "callback with wrong state")

	assert.Nil(t, findCookie(cb.Result().Cookies(), auth.SessionCookie), "callback with wrong state set a session cookie")
}

// Issue #23: without OIDC configured the auth endpoints answer 503 and the
// catalog API stays open (pre-auth dev mode — covered by catalog tests).
func TestOIDC_AuthEndpointsUnavailableWhenNotConfigured(t *testing.T) {
	t.Parallel()
	e := newTestServer(t)

	rec := doRequest(t, e, http.MethodGet, "/api/v1/auth/login", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "GET /auth/login without OIDC")

	rec = doRequest(t, e, http.MethodPost, "/api/v1/auth/refresh", "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "POST /auth/refresh without OIDC")
}

// Issue #23: login before the first-run bootstrap has no org to attach the
// User to — refuse with a clear error instead of a 500.
func TestOIDC_LoginBeforeBootstrapRefused(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	e := newTestServerCfg(t, authCfg(idp, 15*time.Minute))

	code, _ := idp.registerUser("sub-ada", "ada@example.com")
	rec := doRequest(t, e, http.MethodGet, "/api/v1/auth/login", "")
	stateCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_state")
	pkceCookie := findCookie(rec.Result().Cookies(), "pavedway_oidc_pkce")

	loc, err := url.Parse(rec.Result().Header.Get("Location"))
	require.NoError(t, err, "parse login redirect")

	cb := doRequestWithCookies(t, e, http.MethodGet,
		"/api/v1/auth/callback?code="+code+"&state="+loc.Query().Get("state"),
		"", stateCookie, pkceCookie)
	require.Equal(t, http.StatusServiceUnavailable, cb.Code, "callback before bootstrap (body: %s)", cb.Body)

	assert.Nil(t, findCookie(cb.Result().Cookies(), auth.SessionCookie), "callback before bootstrap set a session cookie")
}
