package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

// The disposable database is created by testpg; no production connection or
// data is enumerated. Stable catalog definitions deliberately exclude OIDs.
func TestR5SchemaInventoryAndRestart(t *testing.T) {
	_, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	queries := map[string]string{
		"tables":            `SELECT jsonb_build_object('table',c.relname,'rls',c.relrowsecurity,'forced_rls',c.relforcerowsecurity)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' ORDER BY c.relname`,
		"columns":           `SELECT jsonb_build_object('table',table_name,'name',column_name,'type',udt_name,'nullable',is_nullable,'default',column_default,'position',ordinal_position)::text FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name,ordinal_position`,
		"constraints":       `SELECT jsonb_build_object('table',c.relname,'name',p.conname,'type',p.contype,'definition',pg_get_constraintdef(p.oid),'validated',p.convalidated,'deferrable',p.condeferrable)::text FROM pg_constraint p JOIN pg_class c ON c.oid=p.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' ORDER BY c.relname,p.conname`,
		"indexes":           `SELECT jsonb_build_object('table',tablename,'name',indexname,'definition',indexdef)::text FROM pg_indexes WHERE schemaname='public' ORDER BY tablename,indexname`,
		"triggers":          `SELECT jsonb_build_object('table',c.relname,'name',t.tgname,'definition',pg_get_triggerdef(t.oid),'function',p.proname)::text FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_proc p ON p.oid=t.tgfoid WHERE n.nspname='public' AND NOT t.tgisinternal ORDER BY c.relname,t.tgname`,
		"sequences":         `SELECT jsonb_build_object('name',sequencename,'type',data_type,'start',start_value,'min',min_value,'max',max_value,'increment',increment_by,'cycle',cycle)::text FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename`,
		"trigger_functions": `SELECT DISTINCT jsonb_build_object('name',p.proname,'definition',pg_get_functiondef(p.oid))::text AS body FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prorettype='trigger'::regtype ORDER BY body`,
	}
	collect := func() map[string][]json.RawMessage {
		out := map[string][]json.RawMessage{}
		for k, q := range queries {
			rows, e := pool.Query(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			out[k] = []json.RawMessage{}
			for rows.Next() {
				var s string
				if e = rows.Scan(&s); e != nil {
					rows.Close()
					t.Fatal(e)
				}
				out[k] = append(out[k], json.RawMessage(s))
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
		return out
	}
	before := collect()
	if len(before["tables"]) == 0 || len(before["triggers"]) == 0 {
		t.Fatal("empty catalog")
	}
	cfg, e := pgx.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	if e = postgres.Migrate(ctx, cfg); e != nil {
		t.Fatal(e)
	}
	after := collect()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeated migration changes catalog")
	}
	var version int64
	if e = pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	if version != 19 {
		t.Fatalf("expected current schema v19 (independent API key usage owner), got %d; review current schema inventory", version)
	}
	var usageColumns, usageAllColumns, usagePrimary, usageFK, usageAllFK, usageTriggers int
	if e = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='tenant_api_key_usage' AND ((column_name='api_key_id' AND udt_name='uuid' AND is_nullable='NO') OR (column_name='last_used_at' AND udt_name='timestamptz' AND is_nullable='YES') OR (column_name='last_used_ip' AND udt_name='inet' AND is_nullable='YES'))),
 (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='tenant_api_key_usage'),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='tenant_api_key_usage'::regclass AND contype='p' AND pg_get_constraintdef(oid)='PRIMARY KEY (api_key_id)'),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='tenant_api_key_usage'::regclass AND contype='f' AND conname='tenant_api_key_usage_key_fk' AND confrelid='tenant_api_keys'::regclass AND confdeltype='c' AND confupdtype='a' AND convalidated AND NOT condeferrable AND pg_get_constraintdef(oid)='FOREIGN KEY (api_key_id) REFERENCES tenant_api_keys(id) ON DELETE CASCADE'),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='tenant_api_key_usage'::regclass AND contype='f'),
 (SELECT count(*) FROM pg_trigger WHERE tgrelid IN ('tenant_api_key_usage'::regclass,'tenant_api_keys'::regclass) AND NOT tgisinternal)`).Scan(&usageColumns, &usageAllColumns, &usagePrimary, &usageFK, &usageAllFK, &usageTriggers); e != nil {
		t.Fatal(e)
	}
	if usageColumns != 3 || usageAllColumns != 3 || usagePrimary != 1 || usageFK != 1 || usageAllFK != 1 || usageTriggers != 0 {
		t.Fatalf("usage owner contract: columns=%d/%d primary=%d cascade_fk=%d/%d custom_triggers=%d", usageColumns, usageAllColumns, usagePrimary, usageFK, usageAllFK, usageTriggers)
	}
	var revisionColumns, domainColumns, revisionTriggers, allocators, intentConstraints int
	if e = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('users','permission_profiles') AND column_name='permission_revision' AND udt_name='int8' AND is_nullable='NO' AND column_default LIKE '%nextval%permission_editor_revision_seq%'),
 (SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='user_permission_overrides' AND column_name='domain_access_mode' AND udt_name='text' AND is_nullable='YES'),
 (SELECT count(*) FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE NOT t.tgisinternal AND t.tgenabled='O' AND NOT t.tgdeferrable AND NOT t.tginitdeferred AND ((t.tgrelid='users'::regclass AND t.tgname='permission_user_assignment_revision' AND p.proname='permission_user_assignment_revision' AND t.tgtype=19 AND t.tgattr::text=(SELECT attnum::text FROM pg_attribute WHERE attrelid='users'::regclass AND attname='permission_profile_id')) OR (t.tgrelid='permission_profiles'::regclass AND t.tgname='permission_profile_revision' AND p.proname='permission_profile_revision' AND t.tgtype=19 AND t.tgattr::text='') OR (t.tgrelid='user_permission_overrides'::regclass AND t.tgname='permission_override_revision' AND p.proname='permission_override_revision' AND t.tgtype=29 AND t.tgattr::text=''))),
 (SELECT count(*) FROM pg_sequences WHERE schemaname='public' AND sequencename='permission_editor_revision_seq' AND data_type='bigint'::regtype AND NOT cycle AND increment_by>0),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='user_permission_overrides'::regclass AND conname='user_permission_domain_intent' AND contype='c' AND convalidated)`).Scan(&revisionColumns, &domainColumns, &revisionTriggers, &allocators, &intentConstraints); e != nil {
		t.Fatal(e)
	}
	if revisionColumns != 2 || domainColumns != 1 || revisionTriggers != 3 || allocators != 1 || intentConstraints != 1 {
		t.Fatalf("incomplete version16 owners: revision columns=%d domain=%d triggers=%d allocator=%d intent=%d", revisionColumns, domainColumns, revisionTriggers, allocators, intentConstraints)
	}
	for function, required := range map[string][]string{
		"permission_user_assignment_revision": {"NEW.permission_profile_id IS DISTINCT FROM OLD.permission_profile_id", "NEW.permission_revision := nextval('permission_editor_revision_seq')"},
		"permission_profile_revision":         {"NEW.permission_revision := nextval('permission_editor_revision_seq')"},
		"permission_override_revision":        {"TG_OP='UPDATE' AND NEW.user_id IS DISTINCT FROM OLD.user_id", "ERRCODE='23514'", "TG_OP='DELETE'", "WHERE id=OLD.user_id", "WHERE id=NEW.user_id", "permission_revision=nextval('permission_editor_revision_seq')"},
	} {
		var definition string
		if e = pool.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, function+"()").Scan(&definition); e != nil {
			t.Fatal(e)
		}
		for _, fragment := range required {
			if !strings.Contains(definition, fragment) {
				t.Fatalf("version16 revision owner %s lacks %q", function, fragment)
			}
		}
	}
	// Observe actual domain intent CHECK semantics on disposable rows, rather
	// than accepting a catalog name/count as proof of the four-mode contract.
	tenant, user := uuid.New(), uuid.New()
	_, e = pool.Exec(ctx, `INSERT INTO tenants(id,name,plan_id) VALUES($1,'schema-domain-probe','00000000-0000-0000-0000-000000000002');`, tenant)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `INSERT INTO users(id,tenant_id,email,password_hash,role,is_active) VALUES($1,$2,$3,'schema-only','user',true)`, user, tenant, user.String()+"@schema.invalid")
	if e != nil {
		t.Fatal(e)
	}
	for _, probe := range []struct {
		mode  *string
		zones []uuid.UUID
		valid bool
	}{
		{nil, nil, true}, {nil, []uuid.UUID{}, true}, {nil, []uuid.UUID{uuid.New()}, true},
		{r5SchemaMode("inherit"), nil, true}, {r5SchemaMode("all"), []uuid.UUID{}, true}, {r5SchemaMode("none"), []uuid.UUID{}, true}, {r5SchemaMode("list"), []uuid.UUID{uuid.New()}, true},
		{r5SchemaMode("inherit"), []uuid.UUID{}, false}, {r5SchemaMode("inherit"), []uuid.UUID{uuid.New()}, false}, {r5SchemaMode("all"), nil, false}, {r5SchemaMode("all"), []uuid.UUID{uuid.New()}, false}, {r5SchemaMode("none"), nil, false}, {r5SchemaMode("none"), []uuid.UUID{uuid.New()}, false}, {r5SchemaMode("list"), nil, false}, {r5SchemaMode("list"), []uuid.UUID{}, false}, {r5SchemaMode("unknown"), nil, false},
	} {
		_, e = pool.Exec(ctx, `INSERT INTO user_permission_overrides(user_id,domain_access_mode,allowed_zone_ids) VALUES($1,$2,$3)`, user, probe.mode, probe.zones)
		if probe.valid {
			if e != nil {
				t.Fatalf("valid domain intent rejected: %v", e)
			}
			_, e = pool.Exec(ctx, `DELETE FROM user_permission_overrides WHERE user_id=$1`, user)
			if e != nil {
				t.Fatal(e)
			}
		} else {
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "23514" || (pg.ConstraintName != "user_permission_domain_intent" && pg.ConstraintName != "user_permission_overrides_domain_access_mode_check") {
				t.Fatalf("invalid domain intent not CHECK-refused: %v", e)
			}
		}
	}
	hashes := map[string]string{}
	names, e := filepath.Glob("migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		b, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(b)
		hashes[filepath.Base(name)] = hex.EncodeToString(sum[:])
	}
	var pinned map[string]string
	b, e := os.ReadFile("../../../docs/company-mail/evidence/R4-MIGRATION-HASHES.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &pinned); e != nil {
		t.Fatal(e)
	}
	for name, h := range pinned {
		observed, exists := hashes[name]
		decoded, decodeErr := hex.DecodeString(h)
		if !exists || decodeErr != nil || len(decoded) != sha256.Size || observed != h {
			t.Fatalf("historical migration changed: %s", name)
		}
	}
	if len(pinned) != 13 {
		t.Fatal("incomplete historical migration pin set")
	}
	evidence := map[string]any{"goose_version": version, "migration_sha256": hashes, "catalog": after, "restart_catalog_unchanged": true}
	if path := os.Getenv("TABMAIL_SCHEMA_INVENTORY_OUTPUT"); path != "" {
		b, e := json.MarshalIndent(evidence, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.Write(append(b, '\n'))
		ce := f.Close()
		if e != nil {
			t.Fatal(e)
		}
		if ce != nil {
			t.Fatal(ce)
		}
	}
	t.Logf("schema v%d: tables=%d constraints=%d indexes=%d triggers=%d; restart identical", version, len(after["tables"]), len(after["constraints"]), len(after["indexes"]), len(after["triggers"]))
}

func r5SchemaMode(mode string) *string { return &mode }
