package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
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
	if version != 14 {
		t.Fatalf("expected recipient-ledger schema v14, got %d; review current schema inventory", version)
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
