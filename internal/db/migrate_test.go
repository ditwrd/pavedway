package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ditwrd/pavedway/internal/db"
)

// Ticket #21 (walking skeleton): on a fresh Postgres database, migrations
// must run automatically with no manual step.
func TestRunMigrations_FreshDatabase(t *testing.T) {
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
	defer pool.Close()

	require.NoError(t, db.RunMigrations(pool), "RunMigrations()")

	var versionID int64
	var isApplied bool
	err = pool.QueryRow(ctx, "SELECT version_id, is_applied FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&versionID, &isApplied)
	require.NoError(t, err, "query goose_db_version (migrations did not run)")
	require.True(t, isApplied, "goose_db_version.is_applied")
	// #21's placeholder is 00001; #22 adds the real schema on top of it.
	require.GreaterOrEqual(t, versionID, int64(2), "goose_db_version.version_id")
}
