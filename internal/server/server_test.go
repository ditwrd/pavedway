package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ditwrd/pavedway/internal/server"
)

// Ticket #21 (walking skeleton): the server must answer a health check.
func TestHealthCheck(t *testing.T) {
	e := server.New()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d", rec.Code, http.StatusOK)
	}
}
