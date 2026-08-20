package db

import (
	"embed"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigration embed.FS

func RunMigrations(pool *pgxpool.Pool) error {
	goose.SetBaseFS(embedMigration)

	err := goose.SetDialect("postgres")
	if err != nil {
		return err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	err = goose.Up(db, "migrations")
	if err != nil {
		return err
	}

	return nil
}
