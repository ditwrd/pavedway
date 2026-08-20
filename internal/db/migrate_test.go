package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	defer pool.Close()

	if err := db.RunMigrations(pool); err != nil {
		t.Fatalf("RunMigrations() error = %v, want nil", err)
	}

	var versionID int64
	var isApplied bool
	err = pool.QueryRow(ctx, "SELECT version_id, is_applied FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&versionID, &isApplied)
	if err != nil {
		t.Fatalf("query goose_db_version: %v (migrations did not run)", err)
	}
	if !isApplied {
		t.Fatalf("goose_db_version.is_applied = false, want true")
	}
	if versionID != 1 {
		t.Fatalf("goose_db_version.version_id = %d, want 1", versionID)
	}
}
