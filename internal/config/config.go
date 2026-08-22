package config

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// reloadDebounce coalesces the burst of fsnotify events a single file save
// can produce (e.g. `cat > file` truncates then writes, firing two events;
// reacting to the first would read a momentarily-empty file). Waiting this
// long after the last event before re-reading means only the settled
// on-disk content ever reaches onChange.
const reloadDebounce = 150 * time.Millisecond

type Config struct {
	DatabaseURL string
	Port        string
	OIDC        OIDCConfig
	Session     SessionConfig
}

// OIDCConfig configures the single built-in OIDC identity provider
// (issue #23). Any standards-compliant provider works: Google Workspace,
// Okta, Auth0, Keycloak, etc. Pavedway performs discovery against Issuer
// at boot, so a bad issuer URL or unreachable IdP fails fast rather than
// half-starting.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// SessionConfig configures the pavedway-minted session JWT (issue #23).
// Secret is the HMAC key that signs the session cookie; it must be a
// random value kept out of version control.
type SessionConfig struct {
	Secret string
	TTL    time.Duration
}

// DefaultSessionTTL is how long a minted session JWT stays valid before
// the client must transparently refresh it via the OIDC refresh-token
// flow. Short by design: the cookie outlives the JWT so a browser keeps
// sending the expired token and the server re-issues it in place.
const DefaultSessionTTL = 15 * time.Minute

// defaultOIDCScopes requests the claims pavedway needs (profile identity)
// plus offline_access so the OIDC refresh-token flow can re-mint sessions.
var defaultOIDCScopes = []string{"openid", "profile", "email", "offline_access"}

// Watch loads Config from v — flags, env vars, an optional YAML file, and
// defaults, in viper's own precedence order — and returns the initial
// value. Pass "" for configPath to skip an explicit file and instead
// discover pavedway.* in the working directory.
//
// If onChange is non-nil, Watch also starts watching the config file and
// invokes onChange with the newly loaded Config on every change. A reload
// that fails to parse (e.g. mid-write) is dropped — the previous config
// keeps serving rather than crashing the process on a transient bad read.
func Watch(v *viper.Viper, configPath string, onChange func(Config)) (Config, error) {
	// Map nested keys to env vars, namespaced so a stray PORT or
	// DATABASE_URL from another tool can't override us:
	// oidc.issuer <-> PAVEDWAY_OIDC_ISSUER.
	v.SetEnvPrefix("PAVEDWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	v.SetDefault("port", "8080")

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("pavedway")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if configPath != "" || !errors.As(err, &notFound) {
			return Config{}, err
		}
		// no default pavedway.* file in cwd — flags/env/defaults still apply
	}

	cfg, err := build(v)
	if err != nil {
		return Config{}, err
	}

	if onChange != nil {
		var mu sync.Mutex
		var timer *time.Timer

		v.OnConfigChange(func(_ fsnotify.Event) {
			mu.Lock()
			defer mu.Unlock()

			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(reloadDebounce, func() {
				if cfg, err := build(v); err == nil {
					onChange(cfg)
				}
			})
		})
		v.WatchConfig()
	}

	return cfg, nil
}

func build(v *viper.Viper) (Config, error) {
	dbURL := v.GetString("database_url")
	if dbURL == "" {
		return Config{}, errors.New("PAVEDWAY_DATABASE_URL is required")
	}

	cfg := Config{
		DatabaseURL: dbURL,
		Port:        v.GetString("port"),
		OIDC: OIDCConfig{
			Issuer:       v.GetString("oidc.issuer"),
			ClientID:     v.GetString("oidc.client_id"),
			ClientSecret: v.GetString("oidc.client_secret"),
			RedirectURL:  v.GetString("oidc.redirect_url"),
			Scopes:       v.GetStringSlice("oidc.scopes"),
		},
		Session: SessionConfig{
			Secret: v.GetString("session.secret"),
			TTL:    v.GetDuration("session.ttl"),
		},
	}

	if cfg.Session.TTL == 0 {
		cfg.Session.TTL = DefaultSessionTTL
	}

	if len(cfg.OIDC.Scopes) == 0 {
		cfg.OIDC.Scopes = defaultOIDCScopes
	}

	// Auth is opt-in: no issuer means pavedway runs with the API open
	// (dev-mode stance). Once an issuer is configured, every credential
	// needed to complete the handshake must be present — a half-configured
	// IdP is a silent auth bypass, so refuse to boot.
	if cfg.OIDC.Issuer != "" {
		var missing []string

		for _, f := range []struct {
			name  string
			value string
		}{
			{"oidc.client_id", cfg.OIDC.ClientID},
			{"oidc.client_secret", cfg.OIDC.ClientSecret},
			{"oidc.redirect_url", cfg.OIDC.RedirectURL},
			{"session.secret", cfg.Session.Secret},
		} {
			if f.value == "" {
				missing = append(missing, f.name)
			}
		}

		if len(missing) > 0 {
			return Config{}, fmt.Errorf("OIDC issuer configured but missing: %s", strings.Join(missing, ", "))
		}
	}

	return cfg, nil
}
