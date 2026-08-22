package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/config"
	"github.com/ditwrd/pavedway/internal/server"
)

// Ticket #21 (walking skeleton): the server must answer a health check.
func TestHealthCheck(t *testing.T) {
	e, err := server.New(nil, config.Config{})
	require.NoError(t, err, "server.New()")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "GET /healthz")
}

// Ticket #21 AC3: the built frontend is embedded in the binary and served
// from "/" — no separate frontend server/process.
func TestServesEmbeddedFrontend(t *testing.T) {
	e, err := server.New(nil, config.Config{})
	require.NoError(t, err, "server.New()")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "GET /")
}
