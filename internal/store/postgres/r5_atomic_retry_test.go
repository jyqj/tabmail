package postgres_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func r5RetryFixture(t *testing.T, withKey bool) (*companyFixture, *models.OutboundJob, string) {
	t.Helper()
	f := seedCompany(t)
	var key *uuid.UUID
	token := r3Token(t, f.employee)
	if withKey {
		k, _ := r5LegacyKey(t, f)
		_, e := f.pool.Exec(context.Background(), `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, k.ID)
		must(t, e)
		key = &k.ID
		token = "tm_content_" + k.ID.String()
	}
	var j *models.OutboundJob
	if key != nil {
		j = r5LegacyJob(t, f, key)
	} else {
		j = r5LegacyJob(t, f)
	}
	_, e := f.pool.Exec(context.Background(), `UPDATE outbound_jobs SET state='dead',last_error='retained failure',attempts=2 WHERE id=$1`, j.ID)
	must(t, e)
	return f, j, token
}

func r5RetryRequest(t *testing.T, f *companyFixture, j *models.OutboundJob, token string, key bool, ctx context.Context) (<-chan error, *int) {
	t.Helper()
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	req := httptest.NewRequest("POST", "/api/v1/outbound/"+j.ID.String()+"/retry", nil).WithContext(ctx)
	if key {
		req.Header.Set("X-API-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	done := make(chan error, 1)
	status := new(int)
	go func() { rr := httptest.NewRecorder(); h.ServeHTTP(rr, req); *status = rr.Code; done <- nil }()
	return done, status
}

func TestR5AtomicRetryOrdersAuthorizationChanges(t *testing.T) {
	for _, mode := range []string{"user-freeze", "profile-revoke", "mailbox-disable", "zone-unverify", "key-delete", "key-scope", "requester-freeze", "original-sender-freeze", "grant-revoke", "tenant-policy"} {
		t.Run(mode, func(t *testing.T) {
			key := mode == "key-delete" || mode == "key-scope"
			f, j, token := r5RetryFixture(t, key)
			if mode == "requester-freeze" || mode == "original-sender-freeze" || mode == "grant-revoke" {
				must(t, f.st.SetMailboxGrant(context.Background(), &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true, CanSend: true}))
				token = r3Token(t, f.other)
			}
			p := r5SnapshotProfile(t, f, true, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`, j.ID)
			must(t, e)
			done, status := r5RetryRequest(t, f, j, token, key, ctx)
			pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_jobs")
			change := make(chan error, 1)
			go func() {
				var e error
				switch mode {
				case "requester-freeze":
					_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.other.ID)
				case "original-sender-freeze":
					_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				case "grant-revoke":
					_, e = f.pool.Exec(ctx, `UPDATE mailbox_grants SET can_send=false WHERE mailbox_id=$1 AND user_id=$2`, f.personal.ID, f.other.ID)
				case "tenant-policy":
					_, e = f.pool.Exec(ctx, `UPDATE tenants SET mail_send_policy='disabled' WHERE id=$1`, f.tenant.ID)
				case "user-freeze":
					_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				case "profile-revoke":
					_, e = f.pool.Exec(ctx, `UPDATE permission_profiles SET can_send=false WHERE id=$1`, p.ID)
				case "mailbox-disable":
					_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET send_policy='disabled' WHERE id=$1`, f.personal.ID)
				case "zone-unverify":
					_, e = f.pool.Exec(ctx, `UPDATE domain_zones SET is_verified=false WHERE id=$1`, f.zone.ID)
				case "key-delete":
					e = f.st.DeleteAPIKey(ctx, *j.SenderKeyID)
				case "key-scope":
					_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:read"]'::jsonb WHERE id=$1`, *j.SenderKeyID)
				}
				change <- e
			}()
			ordered, early := r5SnapshotWaitOrDone(t, f, ctx, pid, change)
			must(t, early)
			must(t, hold.Rollback(ctx))
			must(t, r5ConcurrentResult(t, ctx, done))
			if ordered {
				must(t, r5ConcurrentResult(t, ctx, change))
			}
			stored, e := f.st.GetOutboundJob(ctx, j.ID)
			must(t, e)
			if !ordered && (*status == 200 || stored.State != models.OutboundDead) {
				t.Fatalf("revocation committed first but retry returned %d and state=%s", *status, stored.State)
			}
			if ordered && *status != 200 {
				t.Fatalf("ordered authorized retry failed: status=%d state=%s last_error=%q", *status, stored.State, stored.LastError)
			}
			t.Logf("revocation_waited=%v retry_status=%d state=%s", ordered, *status, stored.State)
		})
	}
}

func TestR5AtomicRetryRechecksLockedJobAndRecipientState(t *testing.T) {
	for _, mode := range []string{"corrupt-content", "uncertain-recipient", "already-pending"} {
		t.Run(mode, func(t *testing.T) {
			f, j, token := r5RetryFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`, j.ID)
			must(t, e)
			done, status := r5RetryRequest(t, f, j, token, false, ctx)
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_jobs")
			switch mode {
			case "corrupt-content":
				_, e = hold.Exec(ctx, `UPDATE outbound_jobs SET content_digest='invalid-fixture-digest' WHERE id=$1`, j.ID)
			case "uncertain-recipient":
				_, e = hold.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain' WHERE job_id=$1`, j.ID)
			case "already-pending":
				_, e = hold.Exec(ctx, `UPDATE outbound_jobs SET state='pending' WHERE id=$1`, j.ID)
			}
			must(t, e)
			must(t, hold.Commit(ctx))
			must(t, r5ConcurrentResult(t, ctx, done))
			if *status == 200 {
				t.Fatalf("%s changed while waiting but retry reported success", mode)
			}
			stored, e := f.st.GetOutboundJob(ctx, j.ID)
			must(t, e)
			if mode != "already-pending" && stored.State != models.OutboundDead {
				t.Fatal("rejected retry advanced the job")
			}
		})
	}
}

func TestR5AtomicRetryKeyExpiryDuringJobWait(t *testing.T) {
	f, j, token := r5RetryFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, *j.SenderKeyID).Scan(&deadline))
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`, j.ID)
	must(t, e)
	done, status := r5RetryRequest(t, f, j, token, true, ctx)
	pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_jobs")
	r5AwaitSentDeadline(t, f, ctx, pid, deadline)
	must(t, hold.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	stored, e := f.st.GetOutboundJob(ctx, j.ID)
	must(t, e)
	if *status == 200 || stored.State != models.OutboundDead {
		t.Fatalf("expired key retried after waiting: status=%d state=%s", *status, stored.State)
	}
}

func TestR5AtomicRetryAuditWaitExpiryAndCancellation(t *testing.T) {
	for _, mode := range []string{"expiry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, j, token := r5RetryFixture(t, mode == "expiry")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var deadline time.Time
			if mode == "expiry" {
				must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, *j.SenderKeyID).Scan(&deadline))
			}
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `LOCK TABLE audit_log IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			runCtx, stop := context.WithCancel(ctx)
			defer stop()
			done, status := r5RetryRequest(t, f, j, token, mode == "expiry", runCtx)
			waiting, early := r5SnapshotWaitOrDone(t, f, ctx, hold.Conn().PgConn().PID(), done)
			must(t, early)
			if waiting {
				if mode == "expiry" {
					pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
					r5AwaitSentDeadline(t, f, ctx, pid, deadline)
				} else {
					stop()
				}
			}
			must(t, hold.Rollback(ctx))
			if waiting {
				must(t, r5ConcurrentResult(t, ctx, done))
			}
			// Acquire, not NOWAIT-probe: cancellation can close a connection while PG
			// is still releasing its transaction; the original 12s bound remains.
			probe, e := f.pool.Begin(ctx)
			must(t, e)
			defer probe.Rollback(context.Background())
			_, e = probe.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`, j.ID)
			must(t, e)
			must(t, probe.Rollback(ctx))
			stored, e := f.st.GetOutboundJob(ctx, j.ID)
			must(t, e)
			if !waiting || *status == 200 || stored.State != models.OutboundDead || stored.LastError != "retained failure" {
				t.Fatalf("late rejection not atomic: waited=%v status=%d state=%s", waiting, *status, stored.State)
			}
			var n int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.retry' AND resource_id=$1`, j.ID).Scan(&n))
			if n != 0 {
				t.Fatal("rejected retry retained audit")
			}
		})
	}
}

func TestR5AtomicRetryTemplatesAndLegacyIdentity(t *testing.T) {
	for _, mode := range []string{"template-live", "template-revoke", "legacy-exact", "legacy-busy-gap"} {
		t.Run(mode, func(t *testing.T) {
			f, j, token := r5RetryFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if mode == "template-live" || mode == "template-revoke" {
				tpl, e := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Retry", Draft: templateDraft()})
				must(t, e)
				v, e := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
				must(t, e)
				must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: tpl.ID, MailboxID: f.personal.ID, UserID: f.employee.ID}, true))
				svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
				j, e = svc.Submit(ctx, outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, ZoneID: f.zone.ID, From: f.personal.FullAddress, To: []string{"client@retry.test"}, TemplateVersionID: &v.ID, TemplateVars: map[string]string{"customer": "Client"}})
				must(t, e)
				_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, j.ID)
				must(t, e)
				hold, e := f.pool.Begin(ctx)
				must(t, e)
				defer hold.Rollback(context.Background())
				_, e = hold.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`, j.ID)
				must(t, e)
				done, status := r5RetryRequest(t, f, j, token, false, ctx)
				pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_jobs")
				var changed chan error
				ordered := true
				if mode == "template-revoke" {
					changed = make(chan error, 1)
					go func() {
						_, e := f.pool.Exec(ctx, `UPDATE mail_template_versions SET revoked_at=clock_timestamp() WHERE id=$1`, v.ID)
						changed <- e
					}()
					var early error
					ordered, early = r5SnapshotWaitOrDone(t, f, ctx, pid, changed)
					must(t, early)
				}
				must(t, hold.Rollback(ctx))
				must(t, r5ConcurrentResult(t, ctx, done))
				if changed != nil && ordered {
					must(t, r5ConcurrentResult(t, ctx, changed))
				}
				stored, e := f.st.GetOutboundJob(ctx, j.ID)
				must(t, e)
				if mode == "template-live" && *status != 200 {
					t.Fatalf("valid template retry status %d", *status)
				}
				if mode == "template-revoke" && !ordered && (*status == 200 || stored.State != models.OutboundDead) {
					t.Fatal("revoked template was retried")
				}
				return
			}
			// Ownerless integration keeps verified exact-address fallback outside a
			// company mailbox. It must not acquire employee/administrator authority.
			k, _ := r5LegacyKey(t, f)
			_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=NULL,scopes='["send:write"]'::jsonb WHERE id=$1`, k.ID)
			must(t, e)
			address := "integration@company.test"
			must(t, f.st.CreateSendIdentity(ctx, &models.SendIdentity{TenantID: f.tenant.ID, ZoneID: f.zone.ID, Address: address, IdentityType: models.SendIdentityExact, Verified: true}))
			j = &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, APIKeyID: &k.ID, SenderKeyID: &k.ID, MailFrom: address, To: []string{"to@retry.test"}, RcptTo: []string{"to@retry.test"}, State: models.OutboundDead}
			must(t, f.st.CreateOutboundJob(ctx, j))
			var release func()
			if mode == "legacy-busy-gap" {
				hold, e := f.pool.Begin(ctx)
				must(t, e)
				defer hold.Rollback(context.Background())
				_, e = hold.Exec(ctx, `LOCK TABLE mailboxes IN ROW EXCLUSIVE MODE`)
				must(t, e)
				release = func() { must(t, hold.Rollback(ctx)) }
			}
			done, status := r5RetryRequest(t, f, j, "tm_content_"+k.ID.String(), true, ctx)
			must(t, r5ConcurrentResult(t, ctx, done))
			if release != nil {
				release()
			}
			stored, e := f.st.GetOutboundJob(ctx, j.ID)
			must(t, e)
			if mode == "legacy-exact" && (*status != 200 || stored.State != models.OutboundPending) {
				t.Fatalf("valid integration retry status %d state %s", *status, stored.State)
			}
			if mode == "legacy-busy-gap" && (*status != 409 || stored.State != models.OutboundDead) {
				t.Fatal("unfenced negative mailbox lookup retried")
			}
		})
	}
}

func TestR5AtomicRetryOutboxFailureAndAcceptedRecipients(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			f, j, token := r5RetryFixture(t, false)
			ctx := context.Background()
			_, e := f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='accepted',attempts=1 WHERE job_id=$1 AND address=$2`, j.ID, j.To[0])
			must(t, e)
			before, e := f.st.ListOutboundRecipients(ctx, f.tenant.ID, j.ID)
			must(t, e)
			if failure {
				_, e = f.pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_retry_event CHECK(event_type<>'company.admin.changed') NOT VALID`)
				must(t, e)
			}
			done, status := r5RetryRequest(t, f, j, token, false, ctx)
			must(t, <-done)
			stored, e := f.st.GetOutboundJob(ctx, j.ID)
			must(t, e)
			after, e := f.st.ListOutboundRecipients(ctx, f.tenant.ID, j.ID)
			must(t, e)
			if company.Digest(before) != company.Digest(after) {
				t.Fatal("retry rewrote recipient outcomes")
			}
			var n int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.retry' AND resource_id=$1`, j.ID).Scan(&n))
			if failure {
				if *status != 500 || stored.State != models.OutboundDead || n != 0 {
					t.Fatal("outbox failure did not roll back job and audit")
				}
			} else {
				if *status != 200 || stored.State != models.OutboundPending || stored.Attempts != 2 || n != 1 {
					t.Fatal("valid retry did not commit once")
				}
			}
		})
	}
}

func TestR5AtomicRetryConcurrentCommands(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			f, j, token := r5RetryFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			other := j
			if !same {
				other = r5LegacyJob(t, f)
				_, e := f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, other.ID)
				must(t, e)
			}
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM outbound_jobs WHERE id=ANY($1) ORDER BY id FOR UPDATE`, []uuid.UUID{j.ID, other.ID})
			must(t, e)
			d1, s1 := r5RetryRequest(t, f, j, token, false, ctx)
			d2, s2 := r5RetryRequest(t, f, other, token, false, ctx)
			// PostgreSQL can queue the second tuple waiter behind the first,
			// not directly behind the controller. Observe the actual wait chain.
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var n int
				must(t, f.pool.QueryRow(ctx, `WITH RECURSIVE waiting(pid) AS (SELECT $1::int UNION SELECT a.pid FROM pg_stat_activity a JOIN waiting w ON w.pid=ANY(pg_blocking_pids(a.pid)) WHERE a.datname=current_database() AND a.state='active') SELECT count(*) FROM waiting WHERE pid<>$1::int`, int32(hold.Conn().PgConn().PID())).Scan(&n))
				if n >= 2 {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("two retry commands did not independently reach job locks")
				case <-ticker.C:
				}
			}
			must(t, hold.Rollback(ctx))
			must(t, r5ConcurrentResult(t, ctx, d1))
			must(t, r5ConcurrentResult(t, ctx, d2))
			var n int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.retry' AND resource_id=ANY($1)`, []uuid.UUID{j.ID, other.ID}).Scan(&n))
			if same {
				if !(*s1 == 200 && *s2 == 409 || *s1 == 409 && *s2 == 200) || n != 1 {
					t.Fatalf("duplicate retry status %d/%d audits %d", *s1, *s2, n)
				}
			} else {
				if *s1 != 200 || *s2 != 200 || n != 2 {
					t.Fatalf("independent retries status %d/%d audits %d", *s1, *s2, n)
				}
			}
		})
	}
}

func TestR5AtomicRetryRequiredAuditRollsBack(t *testing.T) {
	f, j, token := r5RetryFixture(t, false)
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT reject_retry_audit CHECK(action<>'outbound.retry') NOT VALID`)
	must(t, e)
	done, status := r5RetryRequest(t, f, j, token, false, ctx)
	must(t, <-done)
	stored, e := f.st.GetOutboundJob(ctx, j.ID)
	must(t, e)
	if *status != 500 || stored.State != models.OutboundDead || stored.Attempts != 2 || stored.LastError != "retained failure" {
		t.Fatalf("failed required audit did not roll back: status=%d state=%s", *status, stored.State)
	}
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.retry' AND resource_id=$1`, j.ID).Scan(&n))
	if n != 0 {
		t.Fatal(fmt.Sprintf("unexpected retry audits %d", n))
	}
}
