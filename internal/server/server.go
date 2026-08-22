// Package server exposes pavedway's HTTP API: the OIDC login flow (issue
// #23), catalog entity endpoints, and health checks.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/ditwrd/pavedway/frontend"
	"github.com/ditwrd/pavedway/internal/auth"
	"github.com/ditwrd/pavedway/internal/config"
	"github.com/ditwrd/pavedway/internal/store"
	"github.com/labstack/echo/v5"
	"golang.org/x/oauth2"
)

// New builds the pavedway HTTP server. When cfg configures an OIDC issuer,
// the server performs provider discovery at boot (failing fast on a bad or
// unreachable IdP) and protects the catalog API with the session
// middleware; without an issuer the API stays open (pre-auth dev mode).
func New(q *store.Queries, cfg config.Config) (*echo.Echo, error) {
	e := echo.New()
	// echo's default logger writes JSON to stdout, which would corrupt
	// piped program output — keep diagnostics on stderr like the rest of
	// the process.
	e.Logger = slog.Default()
	h := &handlers{q: q}

	if cfg.OIDC.Issuer != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)

		cancel()

		if err != nil {
			return nil, fmt.Errorf("OIDC discovery for %q: %w", cfg.OIDC.Issuer, err)
		}

		h.provider = provider
		h.oauth2Cfg = &oauth2.Config{
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.OIDC.RedirectURL,
			Scopes:       cfg.OIDC.Scopes,
		}
		h.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID})
		h.sessions = auth.NewSessions(cfg.Session.Secret, cfg.Session.TTL)
		h.issuer = cfg.OIDC.Issuer
	}

	dist, err := frontend.Dist()
	if err != nil {
		return nil, fmt.Errorf("loading embedded frontend: %w", err)
	}

	e.FileFS("/", "index.html", dist)
	e.StaticFS("/", dist)

	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, "Ok")
	})

	api := e.Group("/api/v1")
	api.POST("/bootstrap", h.bootstrap)

	authAPI := api.Group("/auth", h.requireOIDCEnabled)
	authAPI.GET("/login", h.login)
	authAPI.GET("/callback", h.callback)
	authAPI.POST("/refresh", h.refresh)
	authAPI.POST("/logout", h.logout)

	// The catalog API requires a valid session once auth is configured.
	secured := api
	if h.authEnabled() {
		secured = api.Group("", h.requireAuth)
	}

	secured.POST("/entities", h.createEntity)
	secured.GET("/entities/:kind/:namespace/:name", h.getEntity)
	secured.PUT("/entities/:kind/:namespace/:name", h.updateEntity)
	secured.DELETE("/entities/:kind/:namespace/:name", h.deleteEntity)

	return e, nil
}
