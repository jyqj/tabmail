package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"tabmail/internal/config"
	"tabmail/internal/models"
)

// Internal-package tests exercise the exact private observation UPDATE used by
// public TouchAPIKey for controlled timestamp ties/rollback samples. Real-clock
// and reorder tests call public TouchAPIKey on independent PgStore pools.
// No process-global clock replacement, authority fixture or fake repository.
type r5UsageDB struct {
	cfg  *pgx.ConnConfig
	pool *pgxpool.Pool
}

func r5UsageMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("owned usage fixture operation failed: %T", err)
	}
}

// ConnConfig.ConnString returns the ORIGINAL parsed string, not a serialization
// of later Database/RuntimeParams changes (pgx v5.10 conn.go). Build the desired
// DSN first, then parse it; never mutate a config and round-trip ConnString.
// Keep credentials/connection inputs in memory only, including on parse errors.
func r5UsageScopedDSN(t *testing.T, base, database, application string) string {
	t.Helper()
	var scoped string
	if strings.HasPrefix(base, "postgres://") || strings.HasPrefix(base, "postgresql://") {
		u, err := url.Parse(base)
		r5UsageMust(t, err)
		u.Path, u.RawPath = "/"+database, ""
		query := u.Query()
		query.Del("dbname")
		query.Del("database")
		query.Set("application_name", application)
		u.RawQuery = query.Encode()
		scoped = u.String()
	} else {
		quote := func(s string) string {
			return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
		}
		// pgconn's keyword parser resolves duplicate keys to the final value.
		scoped = base + " dbname=" + quote(database) + " application_name=" + quote(application)
	}
	cfg, err := pgx.ParseConfig(scoped)
	r5UsageMust(t, err)
	if cfg.Database != database || cfg.RuntimeParams["application_name"] != application {
		t.Fatal("scoped fixture DSN did not preserve requested database/application identity")
	}
	return scoped
}

const r5UsageIdentitySQL = `SELECT current_database(),current_setting('application_name')`

func r5UsageAssertIdentity(t *testing.T, row interface{ Scan(...any) error }, database, application string) {
	t.Helper()
	var actualDB, actualApplication string
	r5UsageMust(t, row.Scan(&actualDB, &actualApplication))
	if actualDB != database || actualApplication != application {
		t.Fatalf("owned fixture identity mismatch: database_matches=%t application_matches=%t", actualDB == database, actualApplication == application)
	}
	// Only the generated owned DB name and fixed test role label are logged,
	// never the DSN, credentials, environment, host or a mismatching actual DB.
	t.Logf("owned_connection_identity_verified database=%s application=%s", database, application)
}

func r5UsageEmptyDB(t *testing.T) *r5UsageDB {
	t.Helper()
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Fatal("usage19 acceptance requires an explicit owned TABMAIL_TEST_DB_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	r5UsageMust(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		r5UsageMust(t, admin.Close(ctx))
	})
	name := "tm_usage19_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	r5UsageMust(t, err)
	// Register ownership only after CREATE succeeded. Store/observer cleanup is
	// registered later, so synchronous close precedes this bounded owned drop.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if err != nil {
			t.Errorf("owned usage DB drop failed: %T", err)
		}
	})
	ownedDSN := r5UsageScopedDSN(t, dsn, name, "r5_usage_observer")
	cfg, err := pgx.ParseConfig(ownedDSN)
	r5UsageMust(t, err)
	pc, err := pgxpool.ParseConfig(ownedDSN)
	r5UsageMust(t, err)
	pc.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	r5UsageMust(t, err)
	t.Cleanup(pool.Close)
	r5UsageAssertIdentity(t, pool.QueryRow(ctx, r5UsageIdentitySQL), name, "r5_usage_observer")
	return &r5UsageDB{cfg: cfg, pool: pool}
}

func (f *r5UsageDB) open(t *testing.T, application string) *PgStore {
	t.Helper()
	dsn := r5UsageScopedDSN(t, f.cfg.ConnString(), f.cfg.Database, application)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := New(ctx, config.DB{DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 1, ConnMaxLifetime: time.Minute})
	r5UsageMust(t, err)
	t.Cleanup(func() { r5UsageMust(t, st.Close()) })
	r5UsageAssertIdentity(t, st.pool.QueryRow(ctx, r5UsageIdentitySQL), f.cfg.Database, application)
	return st
}

func r5UsageSeed(t *testing.T) (*r5UsageDB, *PgStore, *models.TenantAPIKey) {
	t.Helper()
	f := r5UsageEmptyDB(t)
	st := f.open(t, "r5_usage_primary")
	tenant := &models.Tenant{Name: "Synthetic usage19", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	r5UsageMust(t, st.CreateTenant(context.Background(), tenant))
	k := &models.TenantAPIKey{TenantID: tenant.ID, KeyHash: hashKey(uuid.NewString()), KeyPrefix: "fixture", Scopes: []string{"send:read", "send:write"}}
	r5UsageMust(t, st.CreateAPIKey(context.Background(), k))
	return f, st, k
}

func r5UsageAwait(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if ok || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func r5UsageWaiter(ctx context.Context, f *r5UsageDB, holder uint32, query string) (uint32, error) {
	var pid uint32
	err := r5UsageAwait(ctx, func() (bool, error) {
		err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database()
 AND query=$2 AND wait_event_type='Lock' AND pg_blocking_pids(pid)=ARRAY[$1::integer]`, holder, query).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	})
	return pid, err
}

func r5UsageRollback(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("owned usage transaction rollback failed: %T", err)
	}
}

type r5UsageFuture struct {
	done chan struct{}
	err  error
}

func r5UsageStart(t *testing.T, release func(), run func() error) *r5UsageFuture {
	t.Helper()
	f := &r5UsageFuture{done: make(chan struct{})}
	go func() { f.err = run(); close(f.done) }()
	t.Cleanup(func() {
		release() // failed assertions must not join while still owning a blocker
		select {
		case <-f.done:
		case <-time.After(6 * time.Second):
			t.Error("usage operation did not join before fixture close/drop")
		}
	})
	return f
}

func (f *r5UsageFuture) join(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-f.done:
		r5UsageMust(t, f.err)
	case <-ctx.Done():
		t.Fatal("usage operation did not complete inside its owned deadline")
	}
}

func r5UsagePair(t *testing.T, st *PgStore, key uuid.UUID) *models.TenantAPIKey {
	t.Helper()
	v, err := st.GetAPIKey(context.Background(), key)
	r5UsageMust(t, err)
	if v == nil || v.KeyHash != "" {
		t.Fatal("metadata key missing or exposed key hash")
	}
	return v
}

func r5UsageExpect(t *testing.T, st *PgStore, key uuid.UUID, at time.Time, ip string) {
	t.Helper()
	v := r5UsagePair(t, st, key)
	if v.LastUsedAt == nil || !v.LastUsedAt.Equal(at) || (ip == "" && v.LastUsedIP != nil) || (ip != "" && (v.LastUsedIP == nil || *v.LastUsedIP != ip)) {
		t.Fatal("timestamp/IP was not the expected indivisible observation")
	}
}

func TestR5APIKeyUsageCreateAtomicAndMissing(t *testing.T) {
	for _, mode := range []string{"create_usage_failure_rolls_back_authority", "missing_usage_is_not_recreated", "touch_never_locks_authority"} {
		t.Run(mode, func(t *testing.T) {
			f, st, k := r5UsageSeed(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			switch mode {
			case "create_usage_failure_rolls_back_authority":
				_, err := f.pool.Exec(ctx, `CREATE FUNCTION r5_usage_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned usage insert failure'; END $$;
CREATE TRIGGER r5_usage_reject BEFORE INSERT ON tenant_api_key_usage FOR EACH ROW EXECUTE FUNCTION r5_usage_reject()`)
				r5UsageMust(t, err)
				failed := &models.TenantAPIKey{TenantID: k.TenantID, KeyHash: hashKey(uuid.NewString()), KeyPrefix: "fixture", Scopes: k.Scopes}
				if st.CreateAPIKey(ctx, failed) == nil {
					t.Fatal("usage failure did not abort key creation")
				}
				var rows int
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_api_keys WHERE id=$1)+(SELECT count(*) FROM tenant_api_key_usage WHERE api_key_id=$1)`, failed.ID).Scan(&rows))
				if rows != 0 {
					t.Fatal("failed key issue left an authority or usage row")
				}
				_, err = f.pool.Exec(ctx, `DROP TRIGGER r5_usage_reject ON tenant_api_key_usage; DROP FUNCTION r5_usage_reject()`)
				r5UsageMust(t, err)
				r5UsageMust(t, st.CreateAPIKey(ctx, failed))
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM tenant_api_key_usage WHERE api_key_id=$1 AND last_used_at IS NULL AND last_used_ip IS NULL`, failed.ID).Scan(&rows))
				if rows != 1 {
					t.Fatal("successful creation did not provision exactly one empty usage row")
				}
			case "missing_usage_is_not_recreated":
				_, err := f.pool.Exec(ctx, `DELETE FROM tenant_api_key_usage WHERE api_key_id=$1`, k.ID)
				r5UsageMust(t, err)
				r5UsageMust(t, st.TouchAPIKey(ctx, k.ID, "192.0.2.31"))
				var rows int
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM tenant_api_key_usage WHERE api_key_id=$1`, k.ID).Scan(&rows))
				if rows != 0 {
					t.Fatal("Touch recreated deliberately missing usage")
				}
				v := r5UsagePair(t, st, k.ID)
				if v.LastUsedAt != nil || v.LastUsedIP != nil {
					t.Fatal("missing usage mutated the legacy fallback")
				}
			case "touch_never_locks_authority":
				hold, err := f.pool.Begin(ctx)
				r5UsageMust(t, err)
				t.Cleanup(func() { r5UsageRollback(t, hold) })
				_, err = hold.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, k.ID, []uuid.UUID{uuid.New()})
				r5UsageMust(t, err)
				r5UsageMust(t, st.TouchAPIKey(ctx, k.ID, "192.0.2.31"))
				v := r5UsagePair(t, st, k.ID)
				if v.LastUsedIP == nil || *v.LastUsedIP != "192.0.2.31" {
					t.Fatal("Touch could not complete while the authority update remained uncommitted")
				}
			}
		})
	}
}

func TestR5APIKeyUsageObservationPairOrder(t *testing.T) {
	f, st, key := r5UsageSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r5UsageMust(t, st.TouchAPIKey(ctx, key.ID, "192.0.2.30"))
	initial := r5UsagePair(t, st, key.ID)
	if initial.LastUsedAt == nil {
		t.Fatal("public Touch did not record its real DB observation")
	}
	// Same actual stored DB timestamp is a controlled tie; lower is a controlled
	// clock-rollback/out-of-order sample. Both call the production UPDATE helper.
	r5UsageMust(t, st.touchAPIKeyObservation(ctx, key.ID, *initial.LastUsedAt, "2001:db8::30"))
	r5UsageExpect(t, st, key.ID, *initial.LastUsedAt, "192.0.2.30")
	r5UsageMust(t, st.touchAPIKeyObservation(ctx, key.ID, initial.LastUsedAt.Add(-time.Second), "192.0.2.99"))
	r5UsageExpect(t, st, key.ID, *initial.LastUsedAt, "192.0.2.30")
	var newer time.Time
	r5UsageMust(t, r5UsageAwait(ctx, func() (bool, error) {
		err := f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&newer)
		return newer.After(*initial.LastUsedAt), err
	}))
	r5UsageMust(t, st.touchAPIKeyObservation(ctx, key.ID, newer, ""))
	r5UsageExpect(t, st, key.ID, newer, "")
	r5UsageMust(t, st.touchAPIKeyObservation(ctx, key.ID, newer, "192.0.2.77"))
	r5UsageExpect(t, st, key.ID, newer, "")
	if st.touchAPIKeyObservation(ctx, key.ID, newer.Add(time.Second), "not-an-IP") == nil {
		t.Fatal("invalid IP unexpectedly committed a new observation")
	}
	r5UsageExpect(t, st, key.ID, newer, "")
	var oldNull bool
	r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT last_used_at IS NULL AND last_used_ip IS NULL FROM tenant_api_keys WHERE id=$1`, key.ID).Scan(&oldNull))
	if !oldNull {
		t.Fatal("Touch wrote the old authority metadata owner")
	}
}

func TestR5APIKeyUsageTwoPoolsLateObservation(t *testing.T) {
	f, newer, key := r5UsageSeed(t)
	older := f.open(t, "r5_usage_delayed")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION r5_usage_delay() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
IF current_setting('application_name')='r5_usage_delayed' THEN PERFORM pg_advisory_xact_lock(529019,1); END IF; RETURN NULL; END $$;
CREATE TRIGGER r5_usage_delay BEFORE UPDATE ON tenant_api_key_usage FOR EACH STATEMENT EXECUTE FUNCTION r5_usage_delay()`)
	r5UsageMust(t, err)
	hold, err := f.pool.Begin(ctx)
	r5UsageMust(t, err)
	t.Cleanup(func() { r5UsageRollback(t, hold) })
	_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(529019,1)`)
	r5UsageMust(t, err)
	delayed := r5UsageStart(t, func() { r5UsageRollback(t, hold) }, func() error { return older.TouchAPIKey(ctx, key.ID, "192.0.2.10") })
	pid, err := r5UsageWaiter(ctx, f, hold.Conn().PgConn().PID(), apiKeyUsageUpdateSQL)
	r5UsageMust(t, err)
	t.Logf("phase=old_observation_sampled_before_statement_gate pid=%d", pid)
	r5UsageMust(t, newer.TouchAPIKey(ctx, key.ID, "2001:db8::20"))
	winner := r5UsagePair(t, newer, key.ID)
	if winner.LastUsedAt == nil || winner.LastUsedIP == nil || *winner.LastUsedIP != "2001:db8::20" {
		t.Fatal("independent pool did not commit while old UPDATE was gated before its row lock")
	}
	r5UsageRollback(t, hold)
	delayed.join(t, ctx)
	r5UsageExpect(t, newer, key.ID, *winner.LastUsedAt, "2001:db8::20")
	t.Log("two independently opened PgStore pools; not a two-OS-process claim")
}

func TestR5APIKeyUsageDeleteRaces(t *testing.T) {
	for _, first := range []string{"usage_update_first", "authority_delete_first"} {
		t.Run(first, func(t *testing.T) {
			f, st, key := r5UsageSeed(t)
			other := f.open(t, "r5_usage_delete")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			hold, err := f.pool.Begin(ctx)
			r5UsageMust(t, err)
			t.Cleanup(func() { r5UsageRollback(t, hold) })
			release := func() { r5UsageRollback(t, hold) }
			if first == "usage_update_first" {
				_, err = f.pool.Exec(ctx, `CREATE FUNCTION r5_usage_hold() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(529019,2); RETURN NEW; END $$;
CREATE TRIGGER r5_usage_hold AFTER UPDATE ON tenant_api_key_usage FOR EACH ROW EXECUTE FUNCTION r5_usage_hold()`)
				r5UsageMust(t, err)
				_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(529019,2)`)
				r5UsageMust(t, err)
				touch := r5UsageStart(t, release, func() error { return st.TouchAPIKey(ctx, key.ID, "192.0.2.35") })
				holder, err := r5UsageWaiter(ctx, f, hold.Conn().PgConn().PID(), apiKeyUsageUpdateSQL)
				r5UsageMust(t, err)
				deleted := r5UsageStart(t, release, func() error { return other.DeleteAPIKey(ctx, key.ID) })
				waiter, err := r5UsageWaiter(ctx, f, holder, `DELETE FROM tenant_api_keys WHERE id=$1`)
				r5UsageMust(t, err)
				t.Logf("phase=authority_to_usage_cascade_wait holder_pid=%d delete_pid=%d", holder, waiter)
				release()
				touch.join(t, ctx)
				deleted.join(t, ctx)
			} else {
				_, err = hold.Exec(ctx, `DELETE FROM tenant_api_keys WHERE id=$1`, key.ID)
				r5UsageMust(t, err)
				touch := r5UsageStart(t, release, func() error { return other.TouchAPIKey(ctx, key.ID, "192.0.2.35") })
				pid, err := r5UsageWaiter(ctx, f, hold.Conn().PgConn().PID(), apiKeyUsageUpdateSQL)
				r5UsageMust(t, err)
				t.Logf("phase=usage_update_waits_for_delete holder_pid=%d touch_pid=%d", hold.Conn().PgConn().PID(), pid)
				r5UsageMust(t, hold.Commit(ctx))
				touch.join(t, ctx)
			}
			r5UsageMust(t, st.TouchAPIKey(ctx, key.ID, "192.0.2.99"))
			var rows int
			r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_api_keys WHERE id=$1)+(SELECT count(*) FROM tenant_api_key_usage WHERE api_key_id=$1)`, key.ID).Scan(&rows))
			if rows != 0 {
				t.Fatal("late Touch resurrected deleted authority or usage")
			}
		})
	}
}

func TestR5APIKeyUsageMigration19(t *testing.T) {
	for _, mode := range []string{"fresh19", "exact18_upgrade_backfill"} {
		t.Run(mode, func(t *testing.T) {
			f := r5UsageEmptyDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			all, err := fs.Sub(migrationsFS, "migrations")
			r5UsageMust(t, err)
			var before string
			if mode == "exact18_upgrade_backfill" {
				old := fstest.MapFS{}
				for n := 1; n <= 18; n++ {
					paths, err := fs.Glob(all, fmt.Sprintf("%05d_*.sql", n))
					r5UsageMust(t, err)
					if len(paths) != 1 {
						t.Fatal("historical migration input is not exact 1..18")
					}
					raw, err := fs.ReadFile(all, paths[0])
					r5UsageMust(t, err)
					old[paths[0]] = &fstest.MapFile{Data: raw}
				}
				db := stdlib.OpenDB(*f.cfg)
				t.Cleanup(func() { r5UsageMust(t, db.Close()) })
				r5UsageAssertIdentity(t, db.QueryRowContext(ctx, r5UsageIdentitySQL), f.cfg.Database, "r5_usage_observer")
				provider, err := goose.NewProvider(goose.DialectPostgres, db, old)
				r5UsageMust(t, err)
				_, err = provider.Up(ctx)
				r5UsageMust(t, err)
				var head int
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&head))
				if head != 18 {
					t.Fatal("upgrade fixture was not actual schema18")
				}
				tenant := uuid.New()
				_, err = f.pool.Exec(ctx, `INSERT INTO tenants(id,name,plan_id) VALUES($1,'Usage upgrade','00000000-0000-0000-0000-000000000002')`, tenant)
				r5UsageMust(t, err)
				for _, ip := range []any{nil, "192.0.2.81", "2001:db8::81"} {
					_, err = f.pool.Exec(ctx, `INSERT INTO tenant_api_keys(id,tenant_id,key_hash,key_prefix,scopes,last_used_at,last_used_ip) VALUES($1,$2,$3,'fixture','["send:read"]',CASE WHEN $4::inet IS NULL THEN NULL ELSE '2025-01-02T03:04:05Z'::timestamptz END,$4)`, uuid.New(), tenant, hashKey(uuid.NewString()), ip)
					r5UsageMust(t, err)
				}
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&before))
			}
			st := f.open(t, "r5_usage_migrated") // official New/Migrate
			var head, missing int
			r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&head))
			r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM tenant_api_keys k LEFT JOIN tenant_api_key_usage u ON u.api_key_id=k.id WHERE u.api_key_id IS NULL OR u.last_used_at IS DISTINCT FROM k.last_used_at OR u.last_used_ip IS DISTINCT FROM k.last_used_ip`).Scan(&missing))
			if head != 19 || missing != 0 {
				t.Fatal("official schema19 startup did not preserve/provision every legacy usage pair")
			}
			if before != "" {
				var after string
				r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(k) ORDER BY id)::text FROM tenant_api_keys k`).Scan(&after))
				if before != after {
					t.Fatal("backfill modified authority or invented legacy observation data")
				}
			}
			if mode == "fresh19" {
				tenant := &models.Tenant{Name: "Fresh usage restart", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
				r5UsageMust(t, st.CreateTenant(ctx, tenant))
				key := &models.TenantAPIKey{TenantID: tenant.ID, KeyHash: hashKey(uuid.NewString()), KeyPrefix: "fixture", Scopes: []string{"send:read"}}
				r5UsageMust(t, st.CreateAPIKey(ctx, key))
				r5UsageMust(t, st.TouchAPIKey(ctx, key.ID, "192.0.2.82"))
			}
			var usageBefore, usageAfter string
			snapshot := `SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY api_key_id),'[]')::text FROM tenant_api_key_usage u`
			r5UsageMust(t, f.pool.QueryRow(ctx, snapshot).Scan(&usageBefore))
			r5UsageMust(t, Migrate(ctx, f.cfg))
			r5UsageMust(t, st.Close())
			_ = f.open(t, "r5_usage_reopened") // actual store reopen, not native restart
			r5UsageMust(t, f.pool.QueryRow(ctx, snapshot).Scan(&usageAfter))
			if usageBefore != usageAfter {
				t.Fatal("repeated Migrate/store reopen changed committed usage")
			}
			db := stdlib.OpenDB(*f.cfg)
			t.Cleanup(func() { r5UsageMust(t, db.Close()) })
			r5UsageAssertIdentity(t, db.QueryRowContext(ctx, r5UsageIdentitySQL), f.cfg.Database, "r5_usage_observer")
			provider, err := goose.NewProvider(goose.DialectPostgres, db, all)
			r5UsageMust(t, err)
			if _, err = provider.Down(ctx); err == nil {
				t.Fatal("schema19 silently allowed destructive automatic rollback")
			}
			r5UsageMust(t, f.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&head))
			if head != 19 {
				t.Fatal("rejected Down changed installed schema")
			}
			r5UsageMust(t, f.pool.QueryRow(ctx, snapshot).Scan(&usageAfter))
			if usageBefore != usageAfter {
				t.Fatal("rejected Down changed usage data")
			}
		})
	}
}
