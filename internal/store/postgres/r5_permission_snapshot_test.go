package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Real PgStore commands, isolated testpg databases, and observed lock waits.
// The 10ms ticker only polls pg_blocking_pids; it does not establish ordering.
func r5SnapshotProfile(t *testing.T, f *companyFixture, canSend, global bool) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{Name: "Snapshot " + uuid.NewString(), CanSend: canSend, DailySendQuota: 31, DailyReceiveQuota: 41, MaxMailboxes: 8, MaxDomains: 2, CanCreateAPIKeys: true}
	if !global {
		p.TenantID = &f.tenant.ID
	}
	must(t, f.st.CreatePermissionProfile(context.Background(), p))
	_, e := f.pool.Exec(context.Background(), `UPDATE users SET permission_profile_id=$1 WHERE id=$2`, p.ID, f.employee.ID)
	must(t, e)
	return p
}

// Returns false only when the competing operation actually completed before it
// was blocked. This gives the old implementation a concrete stale-write result,
// rather than depending on a timeout or a substring of its SQL.
func r5SnapshotWaitOrDone(t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, done <-chan error) (bool, error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e := <-done:
			return false, e
		default:
		}
		var waiting bool
		e := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)))`, int32(blocker)).Scan(&waiting)
		must(t, e)
		if waiting {
			return true, nil
		}
		select {
		case e := <-done:
			return false, e
		case <-ctx.Done():
			t.Fatal("neither a real wait nor completion was observed")
		case <-ticker.C:
		}
	}
}

func r5SnapshotDraftWait(t *testing.T, f *companyFixture, ctx context.Context, draft *company.Draft) (pgx.Tx, uint32, <-chan error) {
	t.Helper()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	t.Cleanup(func() { _ = hold.Rollback(context.Background()) })
	_, e = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, draft.ID)
	must(t, e)
	input := *draft
	input.Payload.Subject = "snapshot-protected edit"
	done := make(chan error, 1)
	go func() { _, e := f.st.SaveMailDraft(ctx, f.u, input); done <- e }()
	pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_drafts")
	return hold, pid, done
}

func TestR5PermissionSnapshotOrdersChanges(t *testing.T) {
	for _, kind := range []string{"profile-local", "profile-global", "override-insert", "override-update", "override-clear"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, kind != "override-clear", kind == "profile-global")
			allow := true
			if kind == "override-update" || kind == "override-clear" {
				must(t, f.st.UpsertUserPermissionOverride(context.Background(), &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &allow}))
			}
			draft := r5Draft(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, readerPID, saved := r5SnapshotDraftWait(t, f, ctx, draft)
			changed := make(chan error, 1)
			go func() {
				if kind == "profile-local" || kind == "profile-global" {
					value := *p
					value.CanSend = false
					changed <- f.st.UpdatePermissionProfile(ctx, &value)
				} else if kind == "override-clear" {
					changed <- f.st.DeleteUserPermissionOverride(ctx, f.employee.ID)
				} else {
					deny := false
					changed <- f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny})
				}
			}()
			ordered, writeErr := r5SnapshotWaitOrDone(t, f, ctx, readerPID, changed)
			must(t, writeErr)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, saved)
			if ordered {
				r5AwaitOperation(t, ctx, changed)
			}
			effective, e := f.st.EffectivePermission(ctx, f.employee.ID)
			must(t, e)
			if effective.CanSend {
				t.Fatal("revocation did not reach the canonical effective query")
			}
			if !ordered {
				t.Fatal("permission revocation committed first, but the old authorized draft still committed afterwards")
			}
			// New requests cannot use an old Actor or draft after the revoke.
			draft.Revision++
			_, e = f.st.SaveMailDraft(ctx, f.u, *draft)
			r5AuditedForbidden(t, e)
			var revision int
			must(t, f.pool.QueryRow(ctx, `SELECT revision FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&revision))
			if revision != draft.Revision {
				t.Fatal("denied request advanced the draft")
			}
		})
	}
}

func TestR5PermissionSnapshotWriterFirstReloads(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprintf("clear=%v", clear), func(t *testing.T) {
			f := seedCompany(t)
			r5SnapshotProfile(t, f, !clear, false)
			allow := true
			must(t, f.st.UpsertUserPermissionOverride(context.Background(), &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &allow}))
			draft := r5Draft(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `LOCK TABLE user_permission_overrides IN SHARE MODE`)
			must(t, e)
			changed := make(chan error, 1)
			go func() {
				if clear {
					changed <- f.st.DeleteUserPermissionOverride(ctx, f.employee.ID)
					return
				}
				deny := false
				changed <- f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny})
			}()
			writerPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "user_permission_overrides")
			saved := make(chan error, 1)
			go func() { _, e := f.st.SaveMailDraft(ctx, f.u, *draft); saved <- e }()
			ordered, readErr := r5SnapshotWaitOrDone(t, f, ctx, writerPID, saved)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, changed)
			if ordered {
				readErr = r5ConcurrentResult(t, ctx, saved)
			}
			if !ordered {
				t.Fatalf("read crossed an already-running override write: result=%v", readErr)
			}
			r5AuditedForbidden(t, readErr)
			var revision int
			must(t, f.pool.QueryRow(ctx, `SELECT revision FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&revision))
			if revision != draft.Revision {
				t.Fatal("stale write escaped the authority reload")
			}
		})
	}
}

func TestR5PermissionSnapshotBusyProfileFailsClosed(t *testing.T) {
	for _, change := range []string{"update", "delete"} {
		t.Run(change, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			draft := r5Draft(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM permission_profiles WHERE id=$1 FOR UPDATE`, p.ID)
			must(t, e)
			// A profile deleter may next need users through ON DELETE SET NULL.
			// A reader holding users must not wait on that busy profile.
			value, readErr := f.st.SaveMailDraft(ctx, f.u, *draft)
			conflict, ok := app.As(readErr)
			if !ok || conflict.Kind != app.KindConflict {
				t.Fatalf("busy profile must reject before resource changes, result=%v err=%v", value != nil, readErr)
			}
			var revision int
			must(t, f.pool.QueryRow(ctx, `SELECT revision FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&revision))
			if revision != draft.Revision {
				t.Fatal("busy profile left a draft change")
			}
			if change == "delete" {
				_, e = hold.Exec(ctx, `DELETE FROM permission_profiles WHERE id=$1`, p.ID)
			} else {
				_, e = hold.Exec(ctx, `UPDATE permission_profiles SET can_send=false WHERE id=$1`, p.ID)
			}
			must(t, e)
			must(t, hold.Commit(ctx))
			_, e = f.st.SaveMailDraft(ctx, f.u, *draft)
			r5AuditedForbidden(t, e)
		})
	}
}

func TestR5PermissionSnapshotIndependentUsersShareProfile(t *testing.T) {
	f := seedCompany(t)
	p := r5SnapshotProfile(t, f, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, e := f.pool.Exec(ctx, `UPDATE users SET permission_profile_id=$1 WHERE id=$2`, p.ID, f.other.ID)
	must(t, e)
	otherMailbox, e := f.st.GetMailboxByAddress(ctx, "successor@company.test")
	must(t, e)
	draft := r5Draft(t, f)
	hold, _, saved := r5SnapshotDraftWait(t, f, ctx, draft)
	actor := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	short, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	_, e = f.st.GetWorkMailbox(short, f.u, f.personal.ID)
	must(t, e)
	_, e = f.st.SaveMailDraft(short, actor, company.Draft{MailboxID: otherMailbox.ID, Payload: company.DraftPayload{Subject: "independent"}})
	must(t, e)
	deny := false
	must(t, f.st.UpsertUserPermissionOverride(short, &models.UserPermissionOverride{UserID: f.other.ID, CanSend: &deny}))
	must(t, f.st.DeleteUserPermissionOverride(short, f.other.ID))
	// No global tenant or profile-exclusive lock for unrelated readers/writers.
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, saved)
}

func TestR5PermissionSnapshotFailureReleasesLocks(t *testing.T) {
	for _, mode := range []string{"cancel", "audit"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if mode == "cancel" {
				draft := r5Draft(t, f)
				run, stop := context.WithCancel(ctx)
				hold, _, saved := r5SnapshotDraftWait(t, f, run, draft)
				stop()
				e := r5ConcurrentResult(t, ctx, saved)
				if !errors.Is(e, context.Canceled) {
					t.Fatalf("cancellation lost: %v", e)
				}
				must(t, hold.Rollback(ctx))
			} else {
				m, _, _ := r5ReadFixture(t, f)
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true, CanSend: true}))
				before := r5AuditedSnapshot(t, f)
				_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT snapshot_audit_failure CHECK(action<>'message.archive') NOT VALID`)
				must(t, e)
				if e = f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "archive"); e == nil {
					t.Fatal("audit failure ignored")
				}
				if r5AuditedSnapshot(t, f) != before {
					t.Fatal("failed snapshot transaction left writes")
				}
			}
			short, stop := context.WithTimeout(ctx, 3*time.Second)
			defer stop()
			p.CanSend = false
			must(t, f.st.UpdatePermissionProfile(short, p))
			deny := false
			must(t, f.st.UpsertUserPermissionOverride(short, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny}))
			must(t, f.st.DeleteUserPermissionOverride(short, f.employee.ID))
		})
	}
}

func TestR5PermissionSnapshotPreservesEffectiveSemantics(t *testing.T) {
	for _, mode := range []string{"inherit", "empty-allowlist", "deny-zone", "override-false-zero", "clear", "no-profile"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			ctx := context.Background()
			p.AllowedZoneIDs = []uuid.UUID{f.zone.ID}
			if mode == "empty-allowlist" {
				p.AllowedZoneIDs = []uuid.UUID{}
			}
			if mode == "deny-zone" {
				p.AllowedZoneIDs = []uuid.UUID{uuid.Nil}
			}
			must(t, f.st.UpdatePermissionProfile(ctx, p))
			if mode == "override-false-zero" || mode == "clear" {
				deny, zero := false, 0
				must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny, DailySendQuota: &zero}))
			}
			if mode == "clear" {
				must(t, f.st.DeleteUserPermissionOverride(ctx, f.employee.ID))
			}
			if mode == "no-profile" {
				_, e := f.pool.Exec(ctx, `UPDATE users SET permission_profile_id=NULL WHERE id=$1`, f.employee.ID)
				must(t, e)
			}
			effective, e := f.st.EffectivePermission(ctx, f.employee.ID)
			must(t, e)
			got, e := f.st.GetWorkMailbox(ctx, f.u, f.personal.ID)
			if mode == "deny-zone" {
				if e == nil {
					t.Fatal("domain restriction was widened")
				}
				return
			}
			must(t, e)
			if got.CanSend != effective.CanSend || !got.CanRead {
				t.Fatal("snapshot changed owner/read/send policy")
			}
			if mode == "override-false-zero" && (effective.CanSend || effective.DailySendQuota != 0) {
				t.Fatal("explicit false/zero lost")
			}
			if mode == "clear" && (!effective.CanSend || effective.DailySendQuota != 31) {
				t.Fatal("clear did not restore inheritance")
			}
			if mode == "no-profile" && effective.CanSend {
				t.Fatal("default send denial changed")
			}
		})
	}
}

func TestR5PermissionSnapshotOrdersContentScope(t *testing.T) {
	for _, source := range []string{"profile", "override"} {
		t.Run(source, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			message, _, _ := r5ReadFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `LOCK TABLE mail_documents IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			read := make(chan error, 1)
			go func() {
				v, e := f.st.GetParsedMessage(ctx, f.u, f.shared.ID, message.ID)
				if e == nil && (v == nil || v.TextBody != "cached synthetic content") {
					e = errors.New("valid cached read lost its content")
				}
				read <- e
			}()
			readerPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_documents")
			revoked := make(chan error, 1)
			go func() {
				if source == "profile" {
					value := *p
					value.AllowedZoneIDs = []uuid.UUID{uuid.Nil}
					revoked <- f.st.UpdatePermissionProfile(ctx, &value)
				} else {
					revoked <- f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, AllowedZoneIDs: []uuid.UUID{uuid.Nil}})
				}
			}()
			ordered, e := r5SnapshotWaitOrDone(t, f, ctx, readerPID, revoked)
			must(t, e)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, read)
			if ordered {
				r5AwaitOperation(t, ctx, revoked)
			}
			if !ordered {
				t.Fatal("domain revoke committed but the blocked content read still returned its old-authority body")
			}
			if v, e := f.st.GetParsedMessage(ctx, f.u, f.shared.ID, message.ID); e == nil || v != nil {
				t.Fatal("cached content escaped the new domain restriction")
			}
			var count int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE message_id=$1`, message.ID).Scan(&count))
			if count != 1 {
				t.Fatal("revocation succeeded by deleting stored content")
			}
		})
	}
}

func TestR5PermissionSnapshotOverrideFailureAndMissingUser(t *testing.T) {
	f := seedCompany(t)
	r5SnapshotProfile(t, f, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	allow := true
	must(t, f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &allow}))
	_, e := f.pool.Exec(ctx, `ALTER TABLE user_permission_overrides ADD CONSTRAINT snapshot_override_failure CHECK(can_send IS DISTINCT FROM false) NOT VALID`)
	must(t, e)
	deny := false
	if e = f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny}); e == nil {
		t.Fatal("invalid override unexpectedly committed")
	}
	p, e := f.st.EffectivePermission(ctx, f.employee.ID)
	must(t, e)
	if !p.CanSend {
		t.Fatal("failed override changed effective permissions")
	}
	must(t, f.st.DeleteUserPermissionOverride(ctx, f.employee.ID))
	missing := uuid.New()
	must(t, f.st.DeleteUserPermissionOverride(ctx, missing))
	if e = f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: missing, CanSend: &allow}); e == nil {
		t.Fatal("override created for a missing user")
	}
}
