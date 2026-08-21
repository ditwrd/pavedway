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
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ditwrd/pavedway/internal/db"
	"github.com/ditwrd/pavedway/internal/server"
	"github.com/ditwrd/pavedway/internal/store"
)

// newTestServer spins up a fresh Postgres, runs migrations, and returns an
// HTTP server backed by it.
func newTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("pavedway"),
		tcpostgres.WithUsername("pavedway"),
		tcpostgres.WithPassword("pavedway"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.RunMigrations(pool); err != nil {
		t.Fatalf("RunMigrations() error = %v, want nil", err)
	}

	return server.New(store.New(pool))
}

func doRequest(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, method, path string, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s %s = %d, want %d\nbody: %s", method, path, rec.Code, want, rec.Body.String())
	}
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
	if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}
	if org.Name != "Acme Corp" {
		t.Fatalf("bootstrap response name = %q, want %q", org.Name, "Acme Corp")
	}

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
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode entity response: %v", err)
	}
	if got.Kind != "Component" || got.Name != "svc" {
		t.Fatalf("GET entity = %s %q, want Component %q", got.Kind, got.Name, "svc")
	}
	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("decode entity metadata: %v", err)
	}
	if meta.Annotations["example.com/x"] != "y" {
		t.Fatalf("annotation after create = %q, want %q", meta.Annotations["example.com/x"], "y")
	}

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
