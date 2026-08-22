package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/config"
)

// Ticket #21 (walking skeleton): the server refuses to boot without a
// database connection string — no silent fallback to a default DSN.
func TestWatch_MissingDatabaseURL(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "")

	_, err := config.Watch(viper.New(), "", nil)
	require.Error(t, err, "Watch() for missing PAVEDWAY_DATABASE_URL")
}

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pavedway.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600), "writing config fixture")
	return path
}

func TestWatch_ReadsConfigFile(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "")
	t.Setenv("PAVEDWAY_PORT", "")
	path := writeConfigFile(t, "database_url: postgres://file/db\nport: \"9090\"\n")

	cfg, err := config.Watch(viper.New(), path, nil)
	require.NoError(t, err, "Watch() error")
	assert.Equal(t, "postgres://file/db", cfg.DatabaseURL, "DatabaseURL")
	assert.Equal(t, "9090", cfg.Port, "Port")
}

// Viper's own precedence puts environment variables above config file
// values — an operator's env override must win without editing the file.
func TestWatch_EnvOverridesConfigFile(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "postgres://env/db")
	path := writeConfigFile(t, "database_url: postgres://file/db\n")

	cfg, err := config.Watch(viper.New(), path, nil)
	require.NoError(t, err, "Watch() error")
	assert.Equal(t, "postgres://env/db", cfg.DatabaseURL, "DatabaseURL (env value)")
}

func TestWatch_ReloadsOnFileChange(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "")
	path := writeConfigFile(t, "database_url: postgres://file/db\nport: \"9090\"\n")

	changed := make(chan config.Config, 1)
	_, err := config.Watch(viper.New(), path, func(cfg config.Config) { changed <- cfg })
	require.NoError(t, err, "Watch() error")

	require.NoError(t, os.WriteFile(path, []byte("database_url: postgres://file/db\nport: \"9091\"\n"), 0o600), "rewriting config fixture")

	select {
	case cfg := <-changed:
		assert.Equal(t, "9091", cfg.Port, "reloaded Port")
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for config reload")
	}
}

// A non-atomic write (e.g. `cat > file`) can truncate-then-write, firing
// fsnotify events for a momentarily-empty or partial file before the final
// content lands. Watch must coalesce a burst of rapid writes into exactly
// one reload carrying the settled final value, not one per raw event.
func TestWatch_DebouncesRapidWrites(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "")
	path := writeConfigFile(t, "database_url: postgres://file/db\nport: \"9090\"\n")

	changed := make(chan config.Config, 4)
	_, err := config.Watch(viper.New(), path, func(cfg config.Config) { changed <- cfg })
	require.NoError(t, err, "Watch() error")

	require.NoError(t, os.WriteFile(path, []byte("database_url: postgres://file/db\nport: \"9091\"\n"), 0o600), "rewriting config fixture")
	require.NoError(t, os.WriteFile(path, []byte("database_url: postgres://file/db\nport: \"9092\"\n"), 0o600), "rewriting config fixture")

	select {
	case cfg := <-changed:
		assert.Equal(t, "9092", cfg.Port, "reloaded Port (settled value)")
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for config reload")
	}

	select {
	case cfg := <-changed:
		require.Fail(t, fmt.Sprintf("got a second reload delivery (Port = %q), want exactly one debounced callback", cfg.Port))
	case <-time.After(300 * time.Millisecond):
	}
}

// Issue #23: enabling OIDC requires the full credential set. A missing
// client secret (or any other required field) with an issuer configured is
// a silent auth bypass waiting to happen — refuse to boot instead.
func TestWatch_OIDCIssuerRequiresFullConfig(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "postgres://test/db")
	t.Setenv("PAVEDWAY_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("PAVEDWAY_OIDC_CLIENT_ID", "pavedway")
	// PAVEDWAY_OIDC_CLIENT_SECRET / PAVEDWAY_OIDC_REDIRECT_URL / PAVEDWAY_SESSION_SECRET intentionally unset

	_, err := config.Watch(viper.New(), "", nil)
	require.Error(t, err, "Watch() for incomplete OIDC config")
}

// Issue #23 AC1: a fully-configured provider is accepted, scopes default to
// the pavedway needs (offline_access for refresh), and the session JWT TTL
// defaults to 15 minutes.
func TestWatch_OIDCConfigLoadsWithDefaults(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "postgres://test/db")
	t.Setenv("PAVEDWAY_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("PAVEDWAY_OIDC_CLIENT_ID", "pavedway")
	t.Setenv("PAVEDWAY_OIDC_CLIENT_SECRET", "s3cret")
	t.Setenv("PAVEDWAY_OIDC_REDIRECT_URL", "https://pavedway.example.com/api/v1/auth/callback")
	t.Setenv("PAVEDWAY_SESSION_SECRET", "random-signing-key")

	cfg, err := config.Watch(viper.New(), "", nil)
	require.NoError(t, err, "Watch() error")

	assert.Equal(t, "https://idp.example.com", cfg.OIDC.Issuer, "OIDC.Issuer (configured issuer)")

	assert.Equal(t, config.DefaultSessionTTL, cfg.Session.TTL, "Session.TTL (default)")

	want := []string{"openid", "profile", "email", "offline_access"}
	assert.Equal(t, want, cfg.OIDC.Scopes, "OIDC.Scopes")
}

// Issue #23: session TTL and OIDC scopes are operator-tunable via the
// config file (list values don't survive env vars in viper's model).
func TestWatch_OIDCSessionTTLAndScopesConfigurable(t *testing.T) {
	t.Setenv("PAVEDWAY_DATABASE_URL", "postgres://test/db")
	path := writeConfigFile(t, `database_url: postgres://file/db
oidc:
  issuer: https://idp.example.com
  client_id: pavedway
  client_secret: s3cret
  redirect_url: https://pavedway.example.com/api/v1/auth/callback
  scopes:
    - openid
    - email
session:
  secret: random-signing-key
  ttl: 30m
`)

	cfg, err := config.Watch(viper.New(), path, nil)
	require.NoError(t, err, "Watch() error")

	assert.Equal(t, 30*time.Minute, cfg.Session.TTL, "Session.TTL")
	assert.Equal(t, []string{"openid", "email"}, cfg.OIDC.Scopes, "OIDC.Scopes")
}
