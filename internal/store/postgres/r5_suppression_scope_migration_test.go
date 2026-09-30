package postgres_test

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"tabmail/internal/store/postgres"
)

//go:embed migrations/*.sql
var r5ScopeMigrationFiles embed.FS

// Creates only a run-owned empty database, never resets the DSN's database.
func r5ScopeMigrationDB(t *testing.T) (*pgx.ConnConfig, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TABMAIL_TEST_DB_DSN required for actual migration integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	must(t, err)
	name := "tm_scope_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	must(t, err)
	cfg, err := pgx.ParseConfig(dsn)
	must(t, err)
	cfg.Database = name
	pc, err := pgxpool.ParseConfig(dsn)
	must(t, err)
	pc.ConnConfig = cfg.Copy()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	must(t, err)
	t.Cleanup(func() {
		pool.Close()
		_, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if e != nil {
			t.Errorf("owned migration database cleanup failed: %T", e)
		}
		admin.Close()
	})
	return cfg, pool
}

func TestR5SuppressionScopesMigrationFreshUpgradeRepeat(t *testing.T) {
	for _, mode := range []string{"fresh", "actual-14-upgrade"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cfg, pool := r5ScopeMigrationDB(t)
			var prior string
			if mode == "actual-14-upgrade" {
				// Run the exact released 1..14 bytes through Goose, not hand-written DDL
				// or a CHECK-disabled fixture. The new binary then performs real Up15.
				all, err := fs.Sub(r5ScopeMigrationFiles, "migrations")
				must(t, err)
				old := fstest.MapFS{}
				entries, err := fs.ReadDir(all, ".")
				must(t, err)
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "00015_") {
						continue
					}
					b, err := fs.ReadFile(all, e.Name())
					must(t, err)
					old[e.Name()] = &fstest.MapFile{Data: b}
				}
				db := stdlib.OpenDB(*cfg)
				defer db.Close()
				provider, err := goose.NewProvider(goose.DialectPostgres, db, old)
				must(t, err)
				_, err = provider.Up(ctx)
				must(t, err)
				var version int64
				must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
				if version != 14 {
					t.Fatal("old migration fixture is not installed version14")
				}
				tenant, user := r5ScopeSeedIdentity(t, ctx, pool)
				r5ScopeInsert(t, ctx, pool, tenant, &user, []string{"send:read"}, true)
				must(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&prior))
				r5ScopeInsert(t, ctx, pool, tenant, nil, []string{"suppression:manage"}, false)
			}
			must(t, postgres.Migrate(ctx, cfg))
			var version int64
			must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
			if version != 15 {
				t.Fatal("migration15 not installed")
			}
			if prior != "" {
				var after string
				must(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&after))
				if after != prior {
					t.Fatal("upgrade rewrote existing key/owner/scopes/expiry bytes")
				}
			}
			tenant, user := r5ScopeSeedIdentity(t, ctx, pool)
			allowed := []string{"domains:read", "domains:write", "routes:read", "routes:write", "mailboxes:read", "mailboxes:write", "messages:read", "messages:write", "send:read", "send:write", "webhooks:read", "webhooks:write", "suppression:read", "suppression:manage"}
			for _, scope := range allowed {
				r5ScopeInsert(t, ctx, pool, tenant, nil, []string{scope}, true)
				r5ScopeInsert(t, ctx, pool, tenant, &user, []string{scope}, true)
			}
			r5ScopeInsert(t, ctx, pool, tenant, nil, []string{}, false)
			r5ScopeInsert(t, ctx, pool, tenant, nil, []string{"suppression:unknown"}, false)
			var before string
			must(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&before))
			for i := 0; i < 2; i++ {
				must(t, postgres.Migrate(ctx, cfg))
			}
			var after string
			must(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&after))
			if before != after {
				t.Fatal("repeated startup migration rewrote key data")
			}
			// Real Goose Down refuses new-scope data atomically, never erases keys.
			dbDown := stdlib.OpenDB(*cfg)
			allDown, err := fs.Sub(r5ScopeMigrationFiles, "migrations")
			must(t, err)
			providerDown, err := goose.NewProvider(goose.DialectPostgres, dbDown, allDown)
			must(t, err)
			_, downErr := providerDown.DownTo(ctx, 14)
			dbDown.Close()
			var downPG *pgconn.PgError
			if !errors.As(downErr, &downPG) || downPG.Code != "23514" || downPG.ConstraintName != "tenant_api_keys_scopes_check" {
				t.Fatalf("new-scope downgrade did not refuse exact CHECK: %T", downErr)
			}
			must(t, pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
			if version != 15 {
				t.Fatal("failed downgrade changed applied version")
			}
			must(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&after))
			if before != after {
				t.Fatal("failed downgrade erased or rewrote key bytes")
			}
			var definition string
			must(t, pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='tenant_api_keys'::regclass AND conname='tenant_api_keys_scopes_check'`).Scan(&definition))
			if !strings.Contains(definition, "suppression:read") || !strings.Contains(definition, "suppression:manage") || !strings.Contains(definition, "jsonb_array_length") {
				t.Fatal("final catalog CHECK lost required scope/array/nonempty predicate")
			}
			t.Log("actual version15 scope catalog verified; old12+new2, ownerless/owned, invalid/empty rejection, existing bytes/two repeats intact; real Down14 refused with version15/check/data preserved")
		})
	}
}

func r5ScopeSeedIdentity(t *testing.T, ctx context.Context, p *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	tenant, user := uuid.New(), uuid.New()
	_, err := p.Exec(ctx, `INSERT INTO tenants(id,name,plan_id) VALUES($1,'scope-migration-fixture','00000000-0000-0000-0000-000000000002')`, tenant)
	must(t, err)
	_, err = p.Exec(ctx, `INSERT INTO users(id,tenant_id,email,password_hash,role,is_active) VALUES($1,$2,$3,'synthetic-fixture','user',true)`, user, tenant, user.String()+"@scope.invalid")
	must(t, err)
	return tenant, user
}
func r5ScopeInsert(t *testing.T, ctx context.Context, p *pgxpool.Pool, tenant uuid.UUID, owner *uuid.UUID, scopes []string, accept bool) {
	t.Helper()
	id := uuid.New()
	b, err := json.Marshal(scopes)
	must(t, err)
	_, err = p.Exec(ctx, `INSERT INTO tenant_api_keys(id,tenant_id,key_hash,key_prefix,label,scopes,owner_user_id) VALUES($1,$2,$3,'fixture','fixture',$4,$5)`, id, tenant, id.String(), b, owner)
	if accept {
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) {
				t.Fatalf("valid fixture scope rejected: SQLSTATE=%s constraint=%s", pg.Code, pg.ConstraintName)
			}
			t.Fatalf("valid fixture insert failed: %T", err)
		}
		return
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "tenant_api_keys_scopes_check" {
		t.Fatalf("invalid/empty scope not rejected by exact CHECK: %T", err)
	}
}
