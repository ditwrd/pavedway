package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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

	return store.New(pool)
}

// bootstrap creates the single v0.1 Organization via the wizard path.
func bootstrap(t *testing.T, q *store.Queries, name string) store.Organization {
	t.Helper()
	org, err := q.BootstrapOrganization(context.Background(), name)
	require.NoError(t, err, "BootstrapOrganization(%q)", name)
	return org
}

// createEntity inserts a bare catalog entity (empty metadata/spec) and
// returns the stored row.
func createEntity(t *testing.T, q *store.Queries, orgID int64, kind, namespace, name string) store.Entity {
	t.Helper()

	ent, err := q.CreateEntity(context.Background(), store.CreateEntityParams{
		OrgID:     orgID,
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Metadata:  []byte("{}"),
		Spec:      []byte("{}"),
	})
	require.NoError(t, err, "CreateEntity(%q %q/%q)", kind, namespace, name)

	return ent
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err, "json.Marshal")
	return b
}

func mustUnmarshal(t *testing.T, b []byte, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(b, v), "json.Unmarshal")
}
