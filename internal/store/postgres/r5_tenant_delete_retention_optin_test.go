//go:build r5tenantcascade

package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

// Opt-in relationship evidence only. Each direction first proves normal
// same-source tenant cascade viability. Catalog refusal is reported as an
// explicit inapplicable leaf, not concurrent execution or mandatory PASS.
func TestR5TenantDeleteRetentionCompleteCommands(t *testing.T) {
	for _, order := range []string{"tenant-delete-first", "retention-first"} {
		t.Run(order, func(t *testing.T) {
			// First prove that this exact normal cascade is viable in a separate
			// fresh database. A NO ACTION/trigger refusal is not a lock-cycle
			// fixture, and is classified instead of bypassed or patched in SQL.
			probe := seedCompany(t)
			r5MaintenanceFixture(t, probe)
			probeBefore := r5TenantDeleteSnapshot(t, probe)
			if err := r5TenantDeleteService(probe).DeleteTenant(context.Background(), probe.tenant.ID, "super_admin:test-only"); err != nil {
				description := r5TenantDeleteConstraint(t, probe, err)
				if r5TenantDeleteSnapshot(t, probe) != probeBefore {
					t.Fatal("normal viability probe changed data before refusing cascade")
				}
				t.Skipf("no viable normal same-message tenant cascade; actual catalog: %s", description)
			}
			f := seedCompany(t)
			id := r5MaintenanceFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			before := r5MaintenanceState(t, f, id)
			var key string
			must(t, f.pool.QueryRow(ctx, `SELECT raw_object_key FROM messages WHERE id=$1`, id).Scan(&key))
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			deletionDone := make(chan error, 1)
			type receipt struct {
				n    int
				keys []string
				err  error
			}
			retentionDone := make(chan receipt, 1)
			deleteTenant := func() {
				deletionDone <- r5TenantDeleteService(f).DeleteTenant(ctx, f.tenant.ID, "super_admin:test-only")
			}
			retain := func() {
				n, keys, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
				retentionDone <- receipt{n, keys, e}
			}
			if order == "tenant-delete-first" {
				name := "r5_tenant_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				gate := "r5-tenant-delete:" + f.tenant.ID.String()
				_, err = f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='`+f.tenant.ID.String()+`'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+gate+`',0)); END IF; RETURN OLD; END $$; CREATE TRIGGER `+name+` BEFORE DELETE ON tenants FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
				must(t, err)
				_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, gate)
				must(t, err)
				go deleteTenant()
				deletionPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM tenants")
				go retain()
				retentionPID := r5WaitBlockedBy(t, f, ctx, deletionPID, "DELETE FROM messages")
				r5MaintenanceTrace(t, f, ctx, hold.Conn().PgConn().PID(), deletionPID, retentionPID)
			} else {
				_, err = hold.Exec(ctx, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, id)
				must(t, err)
				go retain()
				retentionPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "DELETE FROM messages")
				go deleteTenant()
				deletionPID := r5WaitBlockedBy(t, f, ctx, retentionPID, "DELETE FROM tenants")
				r5MaintenanceTrace(t, f, ctx, hold.Conn().PgConn().PID(), retentionPID, deletionPID)
			}
			must(t, hold.Rollback(ctx))
			deletionErr := r5ConcurrentResult(t, ctx, deletionDone)
			var result receipt
			select {
			case result = <-retentionDone:
			case <-ctx.Done():
				t.Fatal("retention terminal missing; timeout is not deadlock evidence")
			}
			deletionDead, retentionDead := r5SQLState(deletionErr) == "40P01", r5SQLState(result.err) == "40P01"
			if result.n < 0 || result.n > 1 || len(result.keys) != result.n || (result.n == 1 && result.keys[0] != key) {
				t.Fatal("retention receipt names a noncommitted or wrong source")
			}
			if result.err != nil && !retentionDead {
				t.Fatalf("unexpected retention SQLSTATE=%s err=%v", r5SQLState(result.err), result.err)
			}
			if deletionErr != nil && !deletionDead {
				t.Fatalf("normal viability passed but concurrent tenant deletion failed SQLSTATE=%s err=%v", r5SQLState(deletionErr), deletionErr)
			}
			if deletionErr == nil {
				r5TenantDeleteAbsent(t, f)
				r5RestoreReferences(t, f, ctx, key, 0)
			} else if result.err == nil {
				if result.n != 1 {
					t.Fatal("retention winner did not delete exact expired source")
				}
				r5MaintenanceExpect(t, r5MaintenanceState(t, f, id), before, 0, 0, 1)
				r5RestoreReferences(t, f, ctx, key, 0)
				must(t, r5TenantDeleteService(f).DeleteTenant(ctx, f.tenant.ID, "super_admin:test-only"))
				r5TenantDeleteAbsent(t, f)
			} else {
				t.Fatal("neither complete command committed")
			}
			if deletionDead || retentionDead {
				t.Errorf("actual tenant-delete/retention 40P01; final cascade/refs/receipts/victim effects verified: tenant=%s retention=%s", r5SQLState(deletionErr), r5SQLState(result.err))
			}
		})
	}
}
