package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestMigrateAppliesCleanly runs the embedded versioned migrations against a
// real PostgreSQL database, twice, to prove both a clean apply and that a
// fully-migrated database is a no-op. Skipped unless TABMAIL_TEST_DB_DSN is
// set (e.g. postgres://user:pass@127.0.0.1:5432/dbname?sslmode=disable).
func TestMigrateAppliesCleanly(t *testing.T) {
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TABMAIL_TEST_DB_DSN not set; skipping live migration test")
	}
	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, connCfg); err != nil {
			t.Fatalf("migrate pass %d: %v", i+1, err)
		}
	}

	conn, err := pgx.ConnectConfig(ctx, connCfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var version int64
	if err := conn.QueryRow(ctx,
		`SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if version < 1 {
		t.Fatalf("expected at least migration version 1, got %d", version)
	}

	var tables int
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('plans','tenants','mailboxes','messages','outbound_jobs',
		                     'webhook_endpoints','ingest_jobs','orphan_objects')`).Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 8 {
		t.Fatalf("expected 8 core tables after migration, got %d", tables)
	}
}
