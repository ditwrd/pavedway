package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ditwrd/pavedway/internal/db"
	"github.com/ditwrd/pavedway/internal/store"
)

// newTestQueries spins up a fresh Postgres, runs migrations, and returns a
// store backed by it. Mirrors the container setup in internal/db/migrate_test.go.
func newTestQueries(t *testing.T) *store.Queries {
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

	return store.New(pool)
}

// bootstrap creates the single v0.1 Organization via the wizard path.
func bootstrap(t *testing.T, q *store.Queries, name string) store.Organization {
	t.Helper()
	org, err := q.BootstrapOrganization(context.Background(), name)
	if err != nil {
		t.Fatalf("BootstrapOrganization(%q) error = %v, want nil", name, err)
	}
	return org
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func mustUnmarshal(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
}
