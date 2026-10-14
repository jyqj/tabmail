package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

// Deterministic tenant UUIDs, but actual creation/reservation/completion and
// draft commands. These fixtures share one disposable database, not 21 stores
// whose separate databases could never exercise cross-tenant scheduling.
func r5GCTenantFixture(t *testing.T, n int) ([]*companyFixture, []*company.Attachment, string) {
	t.Helper()
	st, pool, dsn := testpg.NewPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fs := make([]*companyFixture, n)
	as := make([]*company.Attachment, n)
	for i := range fs {
		f := &companyFixture{st: st, pool: pool}
		f.tenant = &models.Tenant{ID: uuid.MustParse(fmt.Sprintf("10000000-0000-0000-0000-%012d", i+1)), Name: fmt.Sprintf("GC tenant %d", i+1), PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
		must(t, st.CreateTenant(ctx, f.tenant))
		profile := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO permission_profiles(id,tenant_id,name,can_send,daily_send_quota,can_create_api_keys) VALUES($1,$2,'GC employee',true,200,false)`, profile, f.tenant.ID)
		must(t, err)
		f.employee = &models.User{TenantID: f.tenant.ID, Email: fmt.Sprintf("owner@tenant-%d.gc.test", i+1), Role: models.RoleUser, IsActive: true, PasswordHash: "test-only-hash", PermissionProfileID: &profile}
		must(t, st.CreateUser(ctx, f.employee))
		f.u = authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
		f.zone = &models.DomainZone{TenantID: f.tenant.ID, Domain: fmt.Sprintf("tenant-%d.gc.test", i+1), IsVerified: true, MXVerified: true}
		must(t, st.CreateZone(ctx, f.zone))
		f.personal = &models.Mailbox{TenantID: f.tenant.ID, ZoneID: f.zone.ID, OwnerUserID: &f.employee.ID, LocalPart: "owner", FullAddress: f.employee.Email, ResolvedDomain: f.zone.Domain, AccessMode: models.AccessToken}
		must(t, st.CreateMailbox(ctx, f.personal))
		fs[i] = f
		as[i] = r5GCProgressAttachments(t, f, ctx, 1)[0]
	}
	return fs, as, dsn
}

func r5GCTenantExpireAll(t *testing.T, f *companyFixture, ctx context.Context) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 hour'`)
	must(t, err)
}

func r5GCTenantCursor(t *testing.T, f *companyFixture, ctx context.Context, want uuid.UUID) {
	t.Helper()
	var got uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT last_tenant FROM company_attachment_gc_cursor WHERE singleton`).Scan(&got))
	if got != want {
		t.Fatalf("durable tenant cursor=%s, want %s", got, want)
	}
}

func r5GCTenantNoSession(t *testing.T, f *companyFixture, ctx context.Context) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var n int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='tabmail-attachment-gc'`).Scan(&n))
		if n == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("scheduler session/lock was not released")
		case <-tick.C:
		}
	}
}

func TestR5AttachmentGCTenantProtectedPrefixBoundedRounds(t *testing.T) {
	fs, as, _ := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		r5GCProgressProtect(t, fs[i], ctx, as[i:i+1], "draft")
	}
	r5GCTenantExpireAll(t, fs[0], ctx)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5GCTenantCursor(t, fs[0], ctx, fs[19].tenant.ID)
	r5ReferenceCounts(t, fs[20], ctx, as[20], 1, 0)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, fs[20], ctx, as[20], 0, 1)
	for i := 0; i < 20; i++ {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
	}
	r5GCTenantNoSession(t, fs[0], ctx)
}

func TestR5AttachmentGCTenantDefaultWrapperPositiveControl(t *testing.T) {
	fs, as, _ := r5GCTenantFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r5GCProgressProtect(t, fs[0], ctx, as, "draft")
	orphan := r5GCProgressAttachments(t, fs[0], ctx, 1)[0]
	r5GCTenantExpireAll(t, fs[0], ctx)
	must(t, postgres.R5SweepCompanyAttachmentsForTest(ctx, fs[0].st, fs[0].tenant.ID))
	r5ReferenceCounts(t, fs[0], ctx, as[0], 1, 0)
	r5ReferenceCounts(t, fs[0], ctx, orphan, 0, 1)
	var untouched bool
	must(t, fs[0].pool.QueryRow(ctx, `SELECT last_tenant IS NULL FROM company_attachment_gc_cursor WHERE singleton`).Scan(&untouched))
	if !untouched {
		t.Fatal("default inner wrapper unexpectedly changed outer scheduler state")
	}
}

func TestR5AttachmentGCTenantFailuresDoNotStarveOthers(t *testing.T) {
	fs, as, _ := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r5GCTenantExpireAll(t, fs[0], ctx)
	_, err := fs[0].pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION r5_tenant_fail_orphan() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.object_key<>'%s' THEN RAISE EXCEPTION 'controlled tenant GC failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER r5_tenant_fail_orphan BEFORE INSERT ON orphan_objects FOR EACH ROW EXECUTE FUNCTION r5_tenant_fail_orphan()`, as[20].ObjectKey))
	must(t, err)
	err = fs[0].st.SweepCompanyMetadata(ctx)
	if err == nil || strings.Count(err.Error(), "controlled tenant GC failure") != 20 {
		t.Fatalf("expected all 20 tenant errors, got %v", err)
	}
	r5GCTenantCursor(t, fs[0], ctx, fs[19].tenant.ID)
	if err = fs[0].st.SweepCompanyMetadata(ctx); err == nil {
		t.Fatal("wrapped tenant failures were swallowed")
	}
	r5ReferenceCounts(t, fs[20], ctx, as[20], 0, 1)
	for i := 0; i < 20; i++ {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
	}
	r5GCTenantNoSession(t, fs[0], ctx)
}

func TestR5AttachmentGCTenantLockTimeoutContinuesRound(t *testing.T) {
	r5ParallelFreshDB(t)
	fs, as, _ := r5GCTenantFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	r5GCTenantExpireAll(t, fs[0], ctx)
	hold, err := fs[0].pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, fs[0].tenant.ID)
	must(t, err)
	err = fs[0].st.SweepCompanyMetadata(ctx)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "57014" {
		t.Fatalf("expected bounded server statement timeout, got %v", err)
	}
	// The timeout rolls back only tenant 1, not the session advisory lock or
	// remaining page. The still-live same session then services tenant 2.
	r5GCTenantCursor(t, fs[0], ctx, fs[1].tenant.ID)
	r5ReferenceCounts(t, fs[0], ctx, as[0], 1, 0)
	r5ReferenceCounts(t, fs[1], ctx, as[1], 0, 1)
	r5GCTenantNoSession(t, fs[0], ctx)
}

func TestR5AttachmentGCTenantContinuousWritesAndWrap(t *testing.T) {
	fs, as, _ := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for i := 1; i < 20; i++ {
		r5GCProgressProtect(t, fs[i], ctx, as[i:i+1], "draft")
	}
	r5GCTenantExpireAll(t, fs[0], ctx)
	for round := 0; round < 3; round++ {
		// Real writes refill the same first tenant every round. They cannot
		// reset the persistent UUID boundary or monopolize a new candidate page.
		more := r5GCProgressAttachments(t, fs[0], ctx, 1)[0]
		r5GCProgressExpire(t, fs[0], ctx)
		must(t, fs[0].st.SweepCompanyMetadata(ctx))
		r5ReferenceCounts(t, fs[0], ctx, more, 0, 1)
		if round == 0 {
			r5GCTenantCursor(t, fs[0], ctx, fs[19].tenant.ID)
			r5ReferenceCounts(t, fs[20], ctx, as[20], 1, 0)
		} else {
			r5ReferenceCounts(t, fs[20], ctx, as[20], 0, 1)
		}
	}
	for i := 1; i < 20; i++ {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
	}
}

// Only the private TestMain child mode calls this ordinary function. It is not
// registered as a test, so a normal full suite has neither a helper SKIP nor an
// empty helper PASS. Real restart/concurrency assertions remain in its parents.
func r5RunGCTenantScannerProcess(dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := postgres.New(ctx, config.DB{DSN: dsn, MaxOpenConns: 1, MaxIdleConns: 0, ConnMaxLifetime: time.Minute})
	if err != nil {
		return err
	}
	defer st.Close()
	return st.SweepCompanyMetadata(ctx)
}

func r5GCTenantProcess(ctx context.Context, dsn string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, os.Args[0], r5GCScannerChildArgument)
	cmd.Env = r5GCScannerEnvironment(dsn)
	out := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = out, out
	return cmd, out
}

func TestR5AttachmentGCTenantActualProcessRestart(t *testing.T) {
	fs, as, dsn := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		r5GCProgressProtect(t, fs[i], ctx, as[i:i+1], "draft")
	}
	r5GCTenantExpireAll(t, fs[0], ctx)
	for round := 0; round < 2; round++ {
		cmd, out := r5GCTenantProcess(ctx, dsn)
		if err := cmd.Run(); err != nil {
			t.Fatalf("scanner process failed: %v\n%s", err, out)
		}
		if round == 0 {
			r5GCTenantCursor(t, fs[0], ctx, fs[19].tenant.ID)
			r5ReferenceCounts(t, fs[20], ctx, as[20], 1, 0)
		}
	}
	r5ReferenceCounts(t, fs[20], ctx, as[20], 0, 1)
	r5GCTenantNoSession(t, fs[0], ctx)
}

func TestR5AttachmentGCTenantDoubleProcessDoesNotReenterBatch(t *testing.T) {
	fs, as, dsn := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r5GCTenantExpireAll(t, fs[0], ctx)
	hold, err := fs[0].pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, fs[0].tenant.ID)
	must(t, err)
	first, firstOut := r5GCTenantProcess(ctx, dsn)
	must(t, first.Start())
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	r5WaitBlockedBy(t, fs[0], ctx, hold.Conn().PgConn().PID(), "SELECT id FROM tenants")
	r5GCTenantCursor(t, fs[0], ctx, fs[0].tenant.ID)
	second, secondOut := r5GCTenantProcess(ctx, dsn)
	if err := second.Run(); err != nil {
		t.Fatalf("busy scanner should skip: %v\n%s", err, secondOut)
	}
	// Busy exit must neither move the cursor nor masquerade as actual GC.
	r5GCTenantCursor(t, fs[0], ctx, fs[0].tenant.ID)
	for i := range fs {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
	}
	must(t, hold.Rollback(ctx))
	if err := r5ConcurrentResult(t, ctx, firstDone); err != nil {
		t.Fatalf("first scanner failed: %v\n%s", err, firstOut)
	}
	for i := 0; i < 20; i++ {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 0, 1)
	}
	r5ReferenceCounts(t, fs[20], ctx, as[20], 1, 0)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, fs[20], ctx, as[20], 0, 1)
	r5GCTenantNoSession(t, fs[0], ctx)
}

func TestR5AttachmentGCTenantInterruptedSessionResumes(t *testing.T) {
	for _, kind := range []string{"backend_terminated", "context_cancelled", "process_killed"} {
		t.Run(kind, func(t *testing.T) {
			fs, as, dsn := r5GCTenantFixture(t, 21)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r5GCTenantExpireAll(t, fs[0], ctx)
			hold, err := fs[0].pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, fs[0].tenant.ID)
			must(t, err)
			runCtx, runCancel := context.WithCancel(ctx)
			defer runCancel()
			done := make(chan error, 1)
			var child *exec.Cmd
			if kind == "process_killed" {
				child, _ = r5GCTenantProcess(runCtx, dsn)
				must(t, child.Start())
				go func() { done <- child.Wait() }()
			} else {
				go func() { done <- fs[0].st.SweepCompanyMetadata(runCtx) }()
			}
			pid := r5WaitBlockedBy(t, fs[0], ctx, hold.Conn().PgConn().PID(), "SELECT id FROM tenants")
			r5GCTenantCursor(t, fs[0], ctx, fs[0].tenant.ID)
			switch kind {
			case "backend_terminated":
				var killed bool
				must(t, fs[0].pool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, int32(pid)).Scan(&killed))
				if !killed {
					t.Fatal("scheduler backend was not terminated")
				}
			case "context_cancelled":
				runCancel()
			case "process_killed":
				must(t, child.Process.Kill())
			}
			if err := r5ConcurrentResult(t, ctx, done); err == nil {
				t.Fatal("interrupted scanner reported success")
			}
			r5GCTenantNoSession(t, fs[0], ctx)
			for i := range fs {
				r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
			}
			// Tenant 1's attempt was interrupted AFTER its durable checkpoint.
			// Restart services tenants 2..21, not tenant 1's still-held lock.
			must(t, fs[0].st.SweepCompanyMetadata(ctx))
			for i := 1; i < 21; i++ {
				r5ReferenceCounts(t, fs[i], ctx, as[i], 0, 1)
			}
			r5ReferenceCounts(t, fs[0], ctx, as[0], 1, 0)
			must(t, hold.Rollback(ctx))
			must(t, fs[0].st.SweepCompanyMetadata(ctx))
			r5ReferenceCounts(t, fs[0], ctx, as[0], 0, 1)
		})
	}
}

func TestR5AttachmentGCTenantCursorFailureIsClosed(t *testing.T) {
	for _, kind := range []string{"update_rejected", "missing_singleton"} {
		t.Run(kind, func(t *testing.T) {
			fs, as, _ := r5GCTenantFixture(t, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r5GCTenantExpireAll(t, fs[0], ctx)
			var err error
			if kind == "update_rejected" {
				_, err = fs[0].pool.Exec(ctx, `ALTER TABLE company_attachment_gc_cursor ADD CONSTRAINT r5_no_advance CHECK(last_tenant IS NULL)`)
			} else {
				_, err = fs[0].pool.Exec(ctx, `DELETE FROM company_attachment_gc_cursor`)
			}
			must(t, err)
			err = fs[0].st.SweepCompanyMetadata(ctx)
			if err == nil {
				t.Fatal("cursor failure was ignored")
			}
			if kind == "missing_singleton" && !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("missing cursor error lost: %v", err)
			}
			r5ReferenceCounts(t, fs[0], ctx, as[0], 1, 0)
			r5GCTenantNoSession(t, fs[0], ctx)
		})
	}
}

func TestR5AttachmentGCTenantEmptySetDeletedCursorAndNewTenant(t *testing.T) {
	fs, as, _ := r5GCTenantFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r5GCTenantExpireAll(t, fs[0], ctx)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5GCTenantCursor(t, fs[0], ctx, fs[1].tenant.ID)
	for i := range fs {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 0, 1)
	}
	// Removing the former cursor tenant must not delete/reset scheduler state.
	_, err := fs[0].pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, fs[1].tenant.ID)
	must(t, err)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5GCTenantCursor(t, fs[0], ctx, fs[1].tenant.ID)
	// A finite new tenant above the cursor and new work below it are both
	// serviced in the next wrapped round. This is not an infinite-arrival bound.
	newID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	must(t, fs[0].st.CreateTenant(ctx, &models.Tenant{ID: newID, Name: "New eligible tenant", PlanID: fs[0].tenant.PlanID}))
	profile := uuid.New()
	_, err = fs[0].pool.Exec(ctx, `INSERT INTO permission_profiles(id,tenant_id,name,can_send,daily_send_quota,can_create_api_keys) VALUES($1,$2,'GC employee',true,200,false)`, profile, newID)
	must(t, err)
	newUser := &models.User{TenantID: newID, Email: "owner@new.gc.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only-hash", PermissionProfileID: &profile}
	must(t, fs[0].st.CreateUser(ctx, newUser))
	newZone := &models.DomainZone{TenantID: newID, Domain: "new.gc.test", IsVerified: true, MXVerified: true}
	must(t, fs[0].st.CreateZone(ctx, newZone))
	newMailbox := &models.Mailbox{TenantID: newID, ZoneID: newZone.ID, OwnerUserID: &newUser.ID, FullAddress: newUser.Email, LocalPart: "owner", ResolvedDomain: newZone.Domain, AccessMode: models.AccessToken}
	must(t, fs[0].st.CreateMailbox(ctx, newMailbox))
	newFixture := &companyFixture{st: fs[0].st, pool: fs[0].pool, tenant: &models.Tenant{ID: newID}, personal: newMailbox, employee: newUser, zone: newZone, u: authz.Actor{Type: authz.PrincipalUser, ID: newUser.ID, TenantID: newID, Role: models.RoleUser}}
	newAttachment := r5GCProgressAttachments(t, newFixture, ctx, 1)[0]
	more := r5GCProgressAttachments(t, fs[0], ctx, 1)[0]
	r5GCTenantExpireAll(t, fs[0], ctx)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, newFixture, ctx, newAttachment, 0, 1)
	r5ReferenceCounts(t, fs[0], ctx, more, 0, 1)
	r5GCTenantCursor(t, fs[0], ctx, fs[0].tenant.ID)
	r5GCTenantNoSession(t, fs[0], ctx)
}
