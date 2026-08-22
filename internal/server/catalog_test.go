package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ditwrd/pavedway/internal/config"
	"github.com/ditwrd/pavedway/internal/db"
	"github.com/ditwrd/pavedway/internal/server"
	"github.com/ditwrd/pavedway/internal/store"
)

// newTestServer spins up a fresh Postgres, runs migrations, and returns an
// HTTP server backed by it with auth disabled.
func newTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	return newTestServerCfg(t, config.Config{})
}

// newTestServerCfg is newTestServer with an explicit server config — auth
// tests point cfg.OIDC at a fake IdP to exercise the full handshake.
func newTestServerCfg(t *testing.T, cfg config.Config) *echo.Echo {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("pavedway"),
		tcpostgres.WithUsername("pavedway"),
		tcpostgres.WithPassword("pavedway"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err, "start postgres container")
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "connection string")

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "open pool")
	t.Cleanup(pool.Close)

	require.NoError(t, db.RunMigrations(pool), "RunMigrations()")

	e, err := server.New(store.New(pool), cfg)
	require.NoError(t, err, "server.New()")

	return e
}

func doRequest(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequestWithCookies(t, e, method, path, body)
}

func doRequestWithCookies(t *testing.T, e *echo.Echo, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, method, path string, want int) {
	t.Helper()
	require.Equal(t, want, rec.Code, "%s %s (body: %s)", method, path, rec.Body.String())
}

// Ticket #22 AC1 (REST form): the first-run wizard creates exactly one
// Organization; a second bootstrap is refused.
func TestBootstrap_FirstRunCreatesOrg_SecondRefused(t *testing.T) {
	e := newTestServer(t)

	rec := doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)
	wantStatus(t, rec, http.MethodPost, "/api/v1/bootstrap", http.StatusCreated)

	var org struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &org), "decode bootstrap response")
	require.Equal(t, "Acme Corp", org.Name, "bootstrap response name")

	rec = doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Globex"}`)
	wantStatus(t, rec, http.MethodPost, "/api/v1/bootstrap", http.StatusConflict)
}

// Ticket #22 AC2 (REST form): create, read, update, delete through the API,
// with annotations surviving the trip.
func TestEntities_CRUD_ThroughREST(t *testing.T) {
	e := newTestServer(t)
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	// Create
	create := `{"kind":"Component","namespace":"default","name":"svc","metadata":{"annotations":{"example.com/x":"y"}},"spec":{"type":"website"}}`
	rec := doRequest(t, e, http.MethodPost, "/api/v1/entities", create)
	wantStatus(t, rec, http.MethodPost, "/api/v1/entities", http.StatusCreated)

	// Read
	rec = doRequest(t, e, http.MethodGet, "/api/v1/entities/Component/default/svc", "")
	wantStatus(t, rec, http.MethodGet, "/api/v1/entities/Component/default/svc", http.StatusOK)
	var got struct {
		Kind     string          `json:"kind"`
		Name     string          `json:"name"`
		Metadata json.RawMessage `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), "decode entity response")
	require.Equal(t, "Component", got.Kind, "GET entity")
	require.Equal(t, "svc", got.Name, "GET entity")
	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	require.NoError(t, json.Unmarshal(got.Metadata, &meta), "decode entity metadata")
	require.Equal(t, "y", meta.Annotations["example.com/x"], "annotation after create")

	// Update
	update := `{"metadata":{"annotations":{"example.com/x":"z"}},"spec":{"type":"service"}}`
	rec = doRequest(t, e, http.MethodPut, "/api/v1/entities/Component/default/svc", update)
	wantStatus(t, rec, http.MethodPut, "/api/v1/entities/Component/default/svc", http.StatusOK)

	// Delete, then read back is 404
	rec = doRequest(t, e, http.MethodDelete, "/api/v1/entities/Component/default/svc", "")
	wantStatus(t, rec, http.MethodDelete, "/api/v1/entities/Component/default/svc", http.StatusNoContent)

	rec = doRequest(t, e, http.MethodGet, "/api/v1/entities/Component/default/svc", "")
	wantStatus(t, rec, http.MethodGet, "/api/v1/entities/Component/default/svc", http.StatusNotFound)
}

// Ticket #22 AC2: the kind path segment is not hardcoded to Component — a
// second kind (Group) routes through the same handlers.
func TestEntities_SecondKind_Group(t *testing.T) {
	e := newTestServer(t)
	doRequest(t, e, http.MethodPost, "/api/v1/bootstrap", `{"name":"Acme Corp"}`)

	rec := doRequest(t, e, http.MethodPost, "/api/v1/entities",
		`{"kind":"Group","namespace":"default","name":"team-a","metadata":{},"spec":{}}`)
	wantStatus(t, rec, http.MethodPost, "/api/v1/entities", http.StatusCreated)

	rec = doRequest(t, e, http.MethodGet, "/api/v1/entities/Group/default/team-a", "")
	wantStatus(t, rec, http.MethodGet, "/api/v1/entities/Group/default/team-a", http.StatusOK)
}
