package testpg

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseTemplateCancelledBeforeInitialization(t *testing.T) {
	owner := newDatabaseTemplate("postgres://not-used.invalid/test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := owner.prepare(ctx, func(string) error { t.Fatal("cancelled initializer ran"); return nil })
	if !errors.Is(err, context.Canceled) || owner.admin != nil || owner.name != "" {
		t.Fatal("cancelled template acquired resources", err)
	}
	if err = owner.close(); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.prepare(context.Background(), func(string) error { return nil }); err == nil {
		t.Fatal("closed owner admitted a new initialization")
	}
}

func TestDatabaseTemplateCopiesAreIsolatedAndOwned(t *testing.T) {
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("owned PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	fingerprint := func(t *testing.T, pool *pgxpool.Pool) string {
		t.Helper()
		var value string
		err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
          'columns',(SELECT jsonb_agg(jsonb_build_array(table_name,column_name,data_type,column_default,is_nullable) ORDER BY table_name,ordinal_position) FROM information_schema.columns WHERE table_schema='public'),
          'constraints',(SELECT jsonb_agg(jsonb_build_array(c.relname,k.conname,pg_get_constraintdef(k.oid)) ORDER BY c.relname,k.conname) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'),
          'indexes',(SELECT jsonb_agg(jsonb_build_array(tablename,indexname,indexdef) ORDER BY tablename,indexname) FROM pg_indexes WHERE schemaname='public'),
          'migrations',(SELECT jsonb_agg(version_id ORDER BY version_id) FROM goose_db_version WHERE is_applied))::text`).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	var cold string
	if !t.Run("cold-migration-reference", func(t *testing.T) {
		_, pool, _ := NewPostgres(t) // No template scope: actual empty-database migration.
		cold = fingerprint(t, pool)
	}) {
		return
	}
	var template string
	var databases []string
	code := RunWithDatabaseTemplate(func() int {
		for _, mode := range []string{"write-first-copy", "independent-second-copy"} {
			if !t.Run(mode, func(t *testing.T) {
				_, pool, _ := NewPostgres(t)
				if fingerprint(t, pool) != cold {
					t.Fatal("template schema/constraints/indexes/migrations differ from cold initialization")
				}
				databases = append(databases, pool.Config().ConnConfig.Database)
				templateScope.RLock()
				name := templateScope.owner.name
				templateScope.RUnlock()
				if template != "" && template != name {
					t.Fatal("template rebuilt between fixtures")
				}
				template = name
				if template == databases[len(databases)-1] || template == "" {
					t.Fatal("fixture reused template database")
				}
				var accepts bool
				if err := admin.QueryRow(ctx, `SELECT datallowconn FROM pg_database WHERE datname=$1`, template).Scan(&accepts); err != nil || accepts {
					t.Fatal("template permits mutation connections", err)
				}
				var table *string
				if err := pool.QueryRow(ctx, `SELECT to_regclass('template_isolation_probe')::text`).Scan(&table); err != nil || table != nil {
					t.Fatal("another fixture contaminated this database", err)
				}
				if _, err := pool.Exec(ctx, `CREATE TABLE template_isolation_probe(value text); INSERT INTO template_isolation_probe VALUES('private to one copy')`); err != nil {
					t.Fatal(err)
				}
				var versions int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version WHERE is_applied`).Scan(&versions); err != nil || versions < 2 {
					t.Fatal("complete migration ledger missing", err)
				}
			}) {
				return 1
			}
			var exists bool
			if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, databases[len(databases)-1]).Scan(&exists); err != nil || exists {
				t.Fatal("fixture cleanup did not drop its own clone", err)
			}
		}
		return 0
	})
	if code != 0 {
		t.Fatal("template scope cleanup failed")
	}
	if databases[0] == databases[1] {
		t.Fatal("fixtures share a database")
	}
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, template).Scan(&exists); err != nil || exists {
		t.Fatal("suite did not remove its owned template", err)
	}
}

func TestDatabaseTemplateFailedMigrationNeverPublished(t *testing.T) {
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("owned PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := newDatabaseTemplate(dsn)
	t.Cleanup(func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	})
	want := errors.New("controlled migration failure")
	called := 0
	for i := 0; i < 2; i++ {
		name, err := owner.prepare(ctx, func(string) error { called++; return want })
		if name != "" || !errors.Is(err, want) || owner.ready {
			t.Fatal("failed template admitted", err)
		}
	}
	if called != 1 || owner.name == "" {
		t.Fatal("failure retried or created database ownership lost")
	}
	name := owner.name
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&exists); err != nil || exists {
		t.Fatal("failed initialization leaked its owned template", err)
	}
}
