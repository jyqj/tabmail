package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// migrationsFS embeds the versioned schema migrations. New schema changes must
// be added as a new numbered file under migrations/ — never by editing an
// already-released migration.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending versioned migrations to the database identified
// by connConfig. A Postgres session-level advisory lock serializes concurrent
// callers, so multiple roles (api / smtp / worker / retention) can boot at the
// same time without racing on DDL.
func Migrate(ctx context.Context, connConfig *pgx.ConnConfig) error {
	db := stdlib.OpenDB(*connConfig)
	defer db.Close()
	var custom, gooseTable bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('tabmail_schema_migrations') IS NOT NULL,to_regclass('goose_db_version') IS NOT NULL`).Scan(&custom, &gooseTable); err != nil {
		return err
	}
	if custom && !gooseTable {
		return fmt.Errorf("custom archived migration database requires an explicitly reviewed conversion before Goose")
	}
	if gooseTable {
		var unknown int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id NOT IN (0,1,2)`).Scan(&unknown); err != nil {
			return err
		}
		if unknown > 0 {
			return fmt.Errorf("unknown installed migration version; mixed archived/new binaries are unsupported")
		}
	}

	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: open embedded migrations: %w", err)
	}
	sessionLocker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("postgres: create migration locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(sessionLocker))
	if err != nil {
		return fmt.Errorf("postgres: create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("postgres: apply migrations: %w", err)
	}
	return nil
}
