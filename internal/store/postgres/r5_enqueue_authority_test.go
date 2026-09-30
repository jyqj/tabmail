package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

// The tenant parent-key wait is after the real application's preliminary
// authorization but before the enqueue's final authorization. Revocation is
// committed by the blocker before releasing that wait, not by guessed sleeps.
// Every case owns a disposable testpg database and calls the actual Service.
func TestR5EnqueueRechecksAuthorityAfterParentWait(t *testing.T) {
	modes := []string{"authorized", "profile-local", "profile-global", "override-insert", "user-freeze", "key-scope", "key-delete", "key-owner", "key-zone", "key-expiry", "mailbox-grant", "mailbox-disable", "mailbox-expiry", "tenant-policy", "zone-unverify", "template-grant", "template-revoke", "template-retire"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, mode == "profile-global")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			mailbox := f.personal
			if mode == "mailbox-grant" {
				mailbox = f.shared
				must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mailbox.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			}
			req := outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &mailbox.ID, ZoneID: f.zone.ID, From: mailbox.FullAddress, To: []string{"recipient@enqueue.test"}, Subject: "authority fixture", TextBody: "synthetic fixture", IdempotencyKey: "r5-final-enqueue"}
			var key *models.TenantAPIKey
			if strings.HasPrefix(mode, "key-") {
				key, _ = r5LegacyKey(t, f)
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, key.ID)
				must(t, e)
				req.APIKeyID = &key.ID
			}
			var template *company.Template
			if strings.HasPrefix(mode, "template-") {
				var e error
				template, e = f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Enqueue fixture", Draft: templateDraft()})
				must(t, e)
				version, e := f.st.PublishMailTemplate(ctx, f.a, template.ID, template.Revision)
				must(t, e)
				must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: template.ID, MailboxID: mailbox.ID, UserID: f.employee.ID}, true))
				req.TemplateVersionID = &version.ID
				req.TemplateVars = map[string]string{"customer": "Synthetic"}
			}
			attachment, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: mailbox.ID, Filename: "enqueue.txt", Size: 3})
			must(t, e)
			must(t, f.st.FinishMailAttachment(ctx, f.u, attachment.ID, company.Hash("abc")))
			req.AttachmentIDs = []uuid.UUID{attachment.ID}
			draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: mailbox.ID, Payload: company.DraftPayload{Subject: req.Subject, TextBody: req.TextBody, AttachmentIDs: req.AttachmentIDs}})
			must(t, e)
			req.Draft = &store.DraftConsumption{ID: draft.ID, TenantID: f.tenant.ID, UserID: f.employee.ID, Revision: draft.Revision}
			gate, e := f.pool.Begin(ctx)
			must(t, e)
			defer gate.Rollback(context.Background())
			_, e = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
			must(t, e)
			svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
			type result struct {
				job    *models.OutboundJob
				replay bool
				err    error
			}
			done := make(chan result, 1)
			go func() { j, replay, e := svc.SubmitWithReplay(ctx, req); done <- result{j, replay, e} }()
			r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "tenants")
			switch mode {
			case "profile-local", "profile-global":
				_, e = gate.Exec(ctx, `UPDATE permission_profiles SET can_send=false WHERE id=$1`, p.ID)
			case "override-insert":
				_, e = gate.Exec(ctx, `INSERT INTO user_permission_overrides(user_id,can_send) VALUES($1,false) ON CONFLICT(user_id) DO UPDATE SET can_send=false`, f.employee.ID)
			case "user-freeze":
				_, e = gate.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
			case "key-scope":
				_, e = gate.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read"]'::jsonb WHERE id=$1`, key.ID)
			case "key-delete":
				_, e = gate.Exec(ctx, `DELETE FROM tenant_api_keys WHERE id=$1`, key.ID)
			case "key-owner":
				_, e = gate.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$2 WHERE id=$1`, key.ID, f.other.ID)
			case "key-zone":
				_, e = gate.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, key.ID, []uuid.UUID{uuid.New()})
			case "key-expiry":
				_, e = gate.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp() WHERE id=$1`, key.ID)
			case "mailbox-grant":
				_, e = gate.Exec(ctx, `UPDATE mailbox_grants SET can_send=false WHERE mailbox_id=$1 AND user_id=$2`, mailbox.ID, f.employee.ID)
			case "mailbox-disable":
				_, e = gate.Exec(ctx, `UPDATE mailboxes SET send_policy='disabled' WHERE id=$1`, mailbox.ID)
			case "mailbox-expiry":
				_, e = gate.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, mailbox.ID)
			case "tenant-policy":
				_, e = gate.Exec(ctx, `UPDATE tenants SET mail_send_policy='disabled' WHERE id=$1`, f.tenant.ID)
			case "zone-unverify":
				_, e = gate.Exec(ctx, `UPDATE domain_zones SET is_verified=false WHERE id=$1`, f.zone.ID)
			case "template-grant":
				_, e = gate.Exec(ctx, `DELETE FROM mail_template_grants WHERE template_id=$1 AND mailbox_id=$2 AND user_id=$3`, template.ID, mailbox.ID, f.employee.ID)
			case "template-revoke":
				_, e = gate.Exec(ctx, `UPDATE mail_template_versions SET revoked_at=clock_timestamp() WHERE id=$1`, *req.TemplateVersionID)
			case "template-retire":
				_, e = gate.Exec(ctx, `UPDATE mail_templates SET retired=true WHERE id=$1`, template.ID)
			}
			must(t, e)
			must(t, gate.Commit(ctx))
			var got result
			select {
			case got = <-done:
			case <-ctx.Done():
				t.Fatal("enqueue did not finish after parent fence release")
			}
			var jobs, assets, pins, audits, drafts int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM outbound_attachments WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='outbound.submit'),(SELECT count(*) FROM mail_drafts WHERE id=$2)`, f.tenant.ID, draft.ID).Scan(&jobs, &assets, &pins, &audits, &drafts))
			if mode == "authorized" {
				must(t, got.err)
				if got.job == nil || got.replay || jobs != 1 || assets != 1 || pins != 1 || audits != 1 || drafts != 0 {
					t.Fatalf("authorized enqueue incomplete: job=%v replay=%v counts=%v", got.job != nil, got.replay, []int{jobs, assets, pins, audits, drafts})
				}
			} else {
				failure, ok := app.As(app.FromAuthz(got.err))
				if !ok || failure.Kind != app.KindForbidden {
					t.Errorf("revocation committed before enqueue but result=%v; expected authority denial, not SQL/setup failure", got.err)
				}
				if got.job != nil || got.replay || jobs != 0 || assets != 0 || pins != 0 || audits != 0 || drafts != 1 {
					t.Errorf("rejected enqueue left side effects: job=%v replay=%v jobs/assets/pins/audits/drafts=%v", got.job != nil, got.replay, []int{jobs, assets, pins, audits, drafts})
				}
			}
			t.Log(fmt.Sprintf("revocation committed before parent-key release: %s", mode))
		})
	}
}

// A reload alone is insufficient: authorization dependencies must remain
// protected through the last INSERT and commit. The late trigger is installed
// only in this disposable database. A concurrent revoker either waits behind
// this enqueue or commits first and forces a clean denial, never stale accept.
func TestR5EnqueueProtectsAuthorityThroughInsert(t *testing.T) {
	for _, mode := range []string{"profile", "override-insert", "key-scope", "mailbox-grant", "template-grant"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			p := r5SnapshotProfile(t, f, true, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			mailbox := f.personal
			if mode == "mailbox-grant" {
				mailbox = f.shared
				must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: mailbox.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			}
			req := outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &mailbox.ID, ZoneID: f.zone.ID, From: mailbox.FullAddress, To: []string{"recipient@enqueue.test"}, Subject: "late authorization fixture", TextBody: "synthetic fixture", IdempotencyKey: "r5-late-enqueue"}
			var key *models.TenantAPIKey
			if mode == "key-scope" {
				key, _ = r5LegacyKey(t, f)
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, key.ID)
				must(t, e)
				req.APIKeyID = &key.ID
			}
			var template *company.Template
			if mode == "template-grant" {
				var e error
				template, e = f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Late enqueue fixture", Draft: templateDraft()})
				must(t, e)
				version, e := f.st.PublishMailTemplate(ctx, f.a, template.ID, template.Revision)
				must(t, e)
				must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: template.ID, MailboxID: mailbox.ID, UserID: f.employee.ID}, true))
				req.TemplateVersionID = &version.ID
				req.TemplateVars = map[string]string{"customer": "Synthetic"}
			}
			gate := r5PauseCommand(t, f, ctx, "outbound_jobs", "INSERT", "ROW")
			svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
			queued := make(chan error, 1)
			go func() { _, e := svc.Submit(ctx, req); queued <- e }()
			writer := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "INSERT INTO outbound_jobs")
			revoked := make(chan error, 1)
			go func() {
				var e error
				switch mode {
				case "profile":
					value := *p
					value.CanSend = false
					e = f.st.UpdatePermissionProfile(ctx, &value)
				case "override-insert":
					deny := false
					e = f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.employee.ID, CanSend: &deny})
				case "key-scope":
					_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read"]'::jsonb WHERE id=$1`, key.ID)
				case "mailbox-grant":
					_, e = f.pool.Exec(ctx, `UPDATE mailbox_grants SET can_send=false WHERE mailbox_id=$1 AND user_id=$2`, mailbox.ID, f.employee.ID)
				case "template-grant":
					_, e = f.pool.Exec(ctx, `DELETE FROM mail_template_grants WHERE template_id=$1 AND mailbox_id=$2 AND user_id=$3`, template.ID, mailbox.ID, f.employee.ID)
				}
				revoked <- e
			}()
			ordered, early := r5SnapshotWaitOrDone(t, f, ctx, writer, revoked)
			must(t, early)
			must(t, gate.Rollback(ctx))
			e := r5ConcurrentResult(t, ctx, queued)
			if ordered {
				must(t, r5ConcurrentResult(t, ctx, revoked))
				must(t, e)
			} else {
				failure, ok := app.As(app.FromAuthz(e))
				if !ok || failure.Kind != app.KindForbidden {
					t.Errorf("revocation committed during INSERT wait but enqueue returned %v", e)
				}
			}
			var jobs, assets, audits int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='outbound.submit')`, f.tenant.ID).Scan(&jobs, &assets, &audits))
			want := 0
			if ordered {
				want = 1
			}
			if jobs != want || assets != want || audits != want {
				t.Errorf("late authority effects: ordered=%v jobs/assets/audits=%v", ordered, []int{jobs, assets, audits})
			}
			t.Logf("revocation_waited=%v enqueue_error=%v", ordered, e)
		})
	}
}

func r5EnqueueDraftRequest(t *testing.T, f *companyFixture) (outbound.SendRequest, *company.Draft) {
	t.Helper()
	ctx := context.Background()
	a, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "atomic.txt", Size: 3})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
	d, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "atomic enqueue", TextBody: "synthetic fixture", AttachmentIDs: []uuid.UUID{a.ID}}})
	must(t, e)
	return outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, ZoneID: f.zone.ID, From: f.personal.FullAddress, To: []string{"recipient@enqueue.test"}, Subject: d.Payload.Subject, TextBody: d.Payload.TextBody, AttachmentIDs: []uuid.UUID{a.ID}, IdempotencyKey: "r5-atomic-enqueue", Draft: &store.DraftConsumption{ID: d.ID, TenantID: f.tenant.ID, UserID: f.employee.ID, Revision: d.Revision}}, d
}

func r5EnqueueCounts(t *testing.T, f *companyFixture, d *company.Draft) []int {
	t.Helper()
	counts := make([]int, 6)
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM outbound_attachments WHERE tenant_id=$1),(SELECT count(*) FROM outbound_recipients WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='outbound.submit'),(SELECT count(*) FROM mail_drafts WHERE id=$2)`, f.tenant.ID, d.ID).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5]))
	return counts
}

func r5EnqueueRequireRollback(t *testing.T, f *companyFixture, d *company.Draft) {
	t.Helper()
	counts := r5EnqueueCounts(t, f, d)
	for i, n := range counts {
		want := 0
		if i == 5 {
			want = 1
		}
		if n != want {
			t.Fatalf("enqueue not atomic: jobs/assets/pins/recipients/audits/drafts=%v", counts)
		}
	}
}

// Credentials can expire while audit insertion waits, after every earlier
// authorization check and all archive/recipient/attachment writes succeeded.
// Deadline checks must use the final database clock and roll back the draft.
func TestR5EnqueueAuditWaitExpiryAndCancellation(t *testing.T) {
	for _, mode := range []string{"key-expiry", "mailbox-expiry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			r5SnapshotProfile(t, f, true, false)
			req, draft := r5EnqueueDraftRequest(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var deadline time.Time
			if mode == "key-expiry" {
				k, _ := r5LegacyKey(t, f)
				must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb,expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, k.ID).Scan(&deadline))
				req.APIKeyID = &k.ID
			} else if mode == "mailbox-expiry" {
				must(t, f.pool.QueryRow(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, f.personal.ID).Scan(&deadline))
			}
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			runCtx, stop := context.WithCancel(ctx)
			defer stop()
			svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
			done := make(chan error, 1)
			go func() { _, e := svc.Submit(runCtx, req); done <- e }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			if mode == "cancel" {
				stop()
			} else {
				r5AwaitSentDeadline(t, f, ctx, writer, deadline)
			}
			must(t, hold.Rollback(ctx))
			e = r5ConcurrentResult(t, ctx, done)
			if mode == "cancel" {
				if e == nil {
					t.Fatal("cancelled enqueue reported success")
				}
			} else {
				value, ok := app.As(app.FromAuthz(e))
				if !ok || value.Kind != app.KindForbidden {
					t.Fatalf("audit wait expiry returned %v instead of authority denial", e)
				}
			}
			// Acquire the draft lock, not NOWAIT, to wait for a cancelled PostgreSQL
			// connection's transaction rollback before inspecting permanent effects.
			probe, e := f.pool.Begin(ctx)
			must(t, e)
			defer probe.Rollback(context.Background())
			_, e = probe.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, draft.ID)
			must(t, e)
			must(t, probe.Rollback(ctx))
			r5EnqueueRequireRollback(t, f, draft)
		})
	}
}

func TestR5EnqueueRequiredAuditFailureRollsBack(t *testing.T) {
	f := seedCompany(t)
	r5SnapshotProfile(t, f, true, false)
	req, draft := r5EnqueueDraftRequest(t, f)
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_enqueue_audit_failure CHECK(action<>'outbound.submit') NOT VALID`)
	must(t, e)
	svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
	j, e := svc.Submit(ctx, req)
	if e == nil || j != nil || r5SQLState(e) != "23514" {
		t.Fatalf("required audit did not expose its actual injected failure: job=%v error=%v", j != nil, e)
	}
	r5EnqueueRequireRollback(t, f, draft)
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_enqueue_audit_failure`)
	must(t, e)
	j, replay, e := svc.SubmitWithReplay(ctx, req)
	must(t, e)
	if j == nil || replay {
		t.Fatal("rolled back identity/draft was not reusable")
	}
	counts := r5EnqueueCounts(t, f, draft)
	for i, n := range counts {
		want := 1
		if i == 5 {
			want = 0
		}
		if n != want {
			t.Fatalf("post-rollback retry incomplete: %v", counts)
		}
	}
}

func TestR5EnqueueAuthorizedIndependentSubmissionsShareParent(t *testing.T) {
	f := seedCompany(t)
	r5SnapshotProfile(t, f, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gate := r5PauseCommand(t, f, ctx, "outbound_jobs", "INSERT", "ROW")
	svc := outbound.NewService(config.Outbound{Enabled: true, MaxRetries: 3}, f.st, f.st, zerolog.Nop())
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		req := outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, ZoneID: f.zone.ID, From: f.personal.FullAddress, To: []string{"recipient@enqueue.test"}, Subject: "parallel authorized fixture", TextBody: "synthetic fixture", IdempotencyKey: fmt.Sprintf("r5-authorized-parallel-%d", i)}
		go func() { _, e := svc.Submit(ctx, req); done <- e }()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiters int
		must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND query LIKE '%INSERT INTO outbound_jobs%' AND $1::int=ANY(pg_blocking_pids(pid))`, int32(gate.Conn().PgConn().PID())).Scan(&waiters))
		if waiters == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("authorized enqueues serialized instead of sharing parent and authorization locks")
		case <-ticker.C:
		}
	}
	must(t, gate.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	must(t, r5ConcurrentResult(t, ctx, done))
	var jobs, assets, audits int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1),(SELECT count(*) FROM sent_mail_assets WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='outbound.submit')`, f.tenant.ID).Scan(&jobs, &assets, &audits))
	if jobs != 2 || assets != 2 || audits != 2 {
		t.Fatalf("parallel authorized commands incomplete: %v", []int{jobs, assets, audits})
	}
}
