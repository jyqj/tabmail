package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

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
		// Derive the allowlist from this binary, not a manually maintained list:
		// adding a migration must never make the next process restart fail.
		entries, err := fs.ReadDir(migrationsFS, "migrations")
		if err != nil {
			return fmt.Errorf("postgres: read migration versions: %w", err)
		}
		versions := []string{"0"}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			prefix, _, ok := strings.Cut(entry.Name(), "_")
			version, err := strconv.ParseInt(prefix, 10, 64)
			if !ok || err != nil || version <= 0 {
				return fmt.Errorf("postgres: invalid embedded migration %q", entry.Name())
			}
			versions = append(versions, strconv.FormatInt(version, 10))
		}
		var unknown int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id NOT IN (`+strings.Join(versions, ",")+`)`).Scan(&unknown); err != nil {
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
