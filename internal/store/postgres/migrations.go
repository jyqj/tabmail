package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// applyMigrations serializes fresh install and legacy adoption, verifies immutable
// checksums, and runs each version transactionally. No startup-time DROP grants.
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(77441092831001)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS tabmail_schema_migrations(version INT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var latest int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM tabmail_schema_migrations`).Scan(&latest); err != nil {
		return err
	}
	if latest > 4 {
		return fmt.Errorf("database schema %d is newer than this binary", latest)
	}
	company, err := migrationFiles.ReadFile("migrations/0002_company.sql")
	if err != nil {
		return err
	}
	ingress, err := migrationFiles.ReadFile("migrations/0004_ingress_recovery.sql")
	if err != nil {
		return err
	}
	// A reserved gap is not permission to run against an unknown schema.
	var unknown bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tabmail_schema_migrations WHERE version NOT IN (1,2,4))`).Scan(&unknown); err != nil {
		return err
	}
	if unknown {
		return fmt.Errorf("database contains an unsupported migration; integrate its code before upgrading")
	}
	for _, migration := range []struct {
		version int
		sql     string
	}{{1, schemaSQL}, {2, string(company)}, {4, string(ingress)}} {
		version, sql := migration.version, migration.sql
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(sql)))
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tabmail_schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			var previous string
			if err = tx.QueryRow(ctx, `SELECT checksum FROM tabmail_schema_migrations WHERE version=$1`, version).Scan(&previous); err != nil {
				return err
			}
			if previous != sum {
				return fmt.Errorf("migration %d checksum mismatch; add a new migration instead", version)
			}
			continue
		}
		if _, err = tx.Exec(ctx, sql); err != nil {
			return fmt.Errorf("migration %d: %w", version, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO tabmail_schema_migrations(version,checksum) VALUES($1,$2)`, version, sum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
