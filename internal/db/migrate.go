// Package db applies and verifies the pavedway schema migrations.
package db

import (
	"embed"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigration embed.FS

// init configures goose's package-level state exactly once per process.
// goose keeps the migration FS and dialect store in unsynchronized globals
// (SetBaseFS/SetDialect are plain assignments); configuring them per call
// races when concurrent callers migrate — parallel tests each booting their
// own Postgres tripped this under -race. Setting them at package load makes
// every runtime migration a read-only use of the globals.
func init() {
	goose.SetBaseFS(embedMigration)

	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
}

// RunMigrations applies all pending schema migrations to the pool's
// database. goose's dialect and migration FS are configured once at package
// load; this call only reads them, so concurrent callers are safe.
func RunMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	return goose.Up(db, "migrations")
}
