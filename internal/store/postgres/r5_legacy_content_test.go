package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func r5LegacyJob(t *testing.T, f *companyFixture, key ...*uuid.UUID) *models.OutboundJob {
	t.Helper()
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"to@fixture.test"}, CC: []string{"cc@fixture.test"}, BCC: []string{"hidden@fixture.test"}, RcptTo: []string{"to@fixture.test", "cc@fixture.test", "hidden@fixture.test"}, Subject: "safe receipt", TextBody: "PRIVATE_LEGACY_BODY", HTMLBody: "<b>PRIVATE_LEGACY_BODY</b>", HeadersJSON: json.RawMessage(`{"X-Private":"PRIVATE_LEGACY_HEADER"}`), LastError: "PRIVATE_LEGACY_DIAGNOSTIC", SMTPResponse: "PRIVATE_LEGACY_DIAGNOSTIC", State: models.OutboundSent}
	if len(key) > 0 {
		j.APIKeyID = key[0]
		j.SenderKeyID = key[0]
	}
	must(t, f.st.CreateOutboundJob(context.Background(), j))
	return j
}

func r5LegacyKey(t *testing.T, f *companyFixture) (*models.TenantAPIKey, authz.Actor) {
	t.Helper()
	k := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, KeyPrefix: "fixture", Label: "legacy content fixture", OwnerUserID: &f.employee.ID, Scopes: []string{"send:read"}}
	k.KeyHash = company.Hash("tm_content_" + k.ID.String())
	must(t, f.st.CreateAPIKey(context.Background(), k))
	return k, authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: f.tenant.ID, OwnerUserID: &f.employee.ID, Permission: &models.EffectivePermission{CanSend: true}}
}

func TestR5LegacyContentLifecycleMatchesArchive(t *testing.T) {
	for _, mode := range []string{"live", "trash-live", "expires", "purge", "missing-item", "missing-asset"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			j := r5LegacyJob(t, f)
			expected := mode == "live" || mode == "trash-live"
			queries := map[string]string{
				"trash-live":    `UPDATE sent_mail_items SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '1 day' WHERE asset_id=$1`,
				"expires":       `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`,
				"purge":         `UPDATE sent_mail_items SET deleted_at=clock_timestamp(),purge_after=clock_timestamp() WHERE asset_id=$1`,
				"missing-item":  `DELETE FROM sent_mail_items WHERE asset_id=$1`,
				"missing-asset": `DELETE FROM sent_mail_assets WHERE id=$1`,
			}
			if mode == "missing-asset" {
				_, e := f.pool.Exec(ctx, `DELETE FROM sent_mail_items WHERE asset_id=$1`, j.ID)
				must(t, e)
			}
			if q := queries[mode]; q != "" {
				_, e := f.pool.Exec(ctx, q, j.ID)
				must(t, e)
			}
			content, e := f.st.GetSubmissionContent(ctx, f.u, j.ID)
			if expected {
				must(t, e)
				if content == nil {
					t.Fatal("live archive absent")
				}
			} else if content != nil || e == nil {
				t.Fatal("fixture did not make canonical content unavailable")
			}
			svc := submissions.NewService(nil, f.st, nil, zerolog.Nop())
			allowed, e := svc.ContentAllowed(ctx, f.u, j)
			must(t, e)
			if allowed != expected {
				t.Errorf("legacy content allowed=%v while archive allowed=%v", allowed, expected)
			}
			view, e := svc.RedactOutboundJob(ctx, f.u, j)
			must(t, e)
			if view.ContentRedacted == expected {
				t.Error("legacy redaction disagrees with content lifetime")
			}
			if !expected {
				raw, e := json.Marshal(view)
				must(t, e)
				if strings.Contains(string(raw), "PRIVATE_LEGACY") || strings.Contains(string(raw), "hidden@fixture.test") {
					t.Error("expired legacy response exposed private content or diagnostics")
				}
				attempts, e := svc.RedactOutboundAttempts(ctx, f.u, j, []*models.OutboundAttempt{{Error: "PRIVATE_LEGACY_DIAGNOSTIC", SMTPResponse: "PRIVATE_LEGACY_DIAGNOSTIC"}})
				must(t, e)
				if attempts[0].Error != submissions.RestrictedAttemptError {
					t.Error("expired attempt diagnostics remained readable")
				}
			}
			receipt, e := svc.AccessibleOutboundJob(ctx, f.tenant, f.u, j.ID)
			must(t, e)
			if receipt == nil || view.Subject != "safe receipt" {
				t.Fatal("legitimate operation receipt was removed")
			}
		})
	}
}

func TestR5LegacyContentRechecksKeyAndOwner(t *testing.T) {
	for _, mode := range []string{"live", "expired-key", "deleted-key", "changed-owner", "scope-revoked", "key-zone", "owner-zone", "owner-frozen", "ownerless", "unknown-principal"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			j := r5LegacyJob(t, f)
			k, a := r5LegacyKey(t, f)
			switch mode {
			case "expired-key":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp() WHERE id=$1`, k.ID)
				must(t, e)
			case "deleted-key":
				must(t, f.st.DeleteAPIKey(ctx, k.ID))
			case "changed-owner":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$2 WHERE id=$1`, k.ID, f.other.ID)
				must(t, e)
			case "scope-revoked":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["mailboxes:read"]'::jsonb WHERE id=$1`, k.ID)
				must(t, e)
			case "key-zone":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, k.ID, []uuid.UUID{uuid.New()})
				must(t, e)
			case "owner-zone":
				p := r5SnapshotProfile(t, f, true, false)
				p.AllowedZoneIDs = []uuid.UUID{uuid.New()}
				must(t, f.st.UpdatePermissionProfile(ctx, p))
			case "owner-frozen":
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				must(t, e)
			case "ownerless":
				a.OwnerUserID = nil
			case "unknown-principal":
				a.Type = "unrecognized"
			}
			allowed, e := submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, a, j)
			must(t, e)
			if allowed != (mode == "live") {
				t.Fatalf("%s: stale credential allowed=%v", mode, allowed)
			}
		})
	}
}

func TestR5LegacyContentReloadsPermissionAfterIdentityWait(t *testing.T) {
	for _, key := range []bool{false, true} {
		name := "user"
		if key {
			name = "key"
		}
		t.Run(name, func(t *testing.T) {
			f := seedCompany(t)
			j := r5LegacyJob(t, f)
			p := r5SnapshotProfile(t, f, true, false)
			a := f.u
			if key {
				_, a = r5LegacyKey(t, f)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
			must(t, e)
			allowed := false
			done := make(chan error, 1)
			go func() {
				var err error
				allowed, err = submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, a, j)
				done <- err
			}()
			waits, early := r5SnapshotWaitOrDone(t, f, ctx, hold.Conn().PgConn().PID(), done)
			must(t, early)
			_, e = hold.Exec(ctx, `UPDATE permission_profiles SET allowed_zone_ids=$2 WHERE id=$1`, p.ID, []uuid.UUID{uuid.New()})
			must(t, e)
			must(t, hold.Commit(ctx))
			if waits {
				r5AwaitOperation(t, ctx, done)
			}
			if !waits || allowed {
				t.Fatalf("content crossed changed identity/permission: waited=%v allowed=%v", waits, allowed)
			}
		})
	}
}

func TestR5LegacyOutboundHTTPDoesNotResurrectExpiredContent(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := r5LegacyJob(t, f)
	must(t, f.st.CreateOutboundAttempt(ctx, &models.OutboundAttempt{TenantID: f.tenant.ID, JobID: j.ID, Attempt: 1, Error: "PRIVATE_LEGACY_DIAGNOSTIC", SMTPResponse: "PRIVATE_LEGACY_DIAGNOSTIC"}))
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
	must(t, e)
	r3HTTP(t, h, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, 404)
	for _, path := range []string{"/api/v1/outbound/" + j.ID.String(), "/api/v1/outbound", "/api/v1/outbound/" + j.ID.String() + "/attempts"} {
		rr := r3HTTP(t, h, token, "GET", path, nil, 200)
		if strings.Contains(rr.Body.String(), "PRIVATE_LEGACY") || strings.Contains(rr.Body.String(), "hidden@fixture.test") {
			t.Errorf("%s exposed expired content", path)
		}
	}
}

func TestR5LegacyContentKeyHTTPPreservesSafeReceipt(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	k, _ := r5LegacyKey(t, f)
	j := r5LegacyJob(t, f, &k.ID)
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := "tm_content_" + k.ID.String()
	path := "/api/v1/outbound/" + j.ID.String()
	call := func(status int) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-API-Key", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("key request: wanted %d got %d", status, w.Code)
		}
		return w
	}
	live := call(200)
	if !strings.Contains(live.Body.String(), "PRIVATE_LEGACY_BODY") {
		t.Fatal("valid owned key lost live content")
	}
	_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
	must(t, e)
	rr := call(200)
	if strings.Contains(rr.Body.String(), "PRIVATE_LEGACY") || strings.Contains(rr.Body.String(), "hidden@fixture.test") {
		t.Fatal("key receipt exposed expired content")
	}
	if !strings.Contains(rr.Body.String(), "safe receipt") {
		t.Fatal("owned key lost safe receipt")
	}
	_, e = f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, k.ID)
	must(t, e)
	call(403) // send:write must never grant this GET endpoint.
	must(t, f.st.DeleteAPIKey(ctx, k.ID))
	call(401)
}

func TestR5LegacyContentRejectsBusyKeyWithoutDeadlock(t *testing.T) {
	f := seedCompany(t)
	j := r5LegacyJob(t, f)
	k, a := r5LegacyKey(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM tenant_api_keys WHERE id=$1 FOR UPDATE`, k.ID)
	must(t, e)
	allowed, e := submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, a, j)
	v, ok := app.As(e)
	if allowed || !ok || v.Kind != app.KindConflict {
		t.Fatalf("busy key did not fail closed: allowed=%v err=%v", allowed, e)
	}
	must(t, hold.Rollback(ctx))
	allowed, e = submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, a, j)
	must(t, e)
	if !allowed {
		t.Fatal("failed decision leaked locks or blocked recovery")
	}
}

func TestR5LegacyContentKeyExpiryAfterMailboxWait(t *testing.T) {
	f := seedCompany(t)
	j := r5LegacyJob(t, f)
	k, a := r5LegacyKey(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, k.ID).Scan(&deadline))
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.personal.ID)
	must(t, e)
	allowed := false
	done := make(chan error, 1)
	go func() {
		var err error
		allowed, err = submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, a, j)
		done <- err
	}()
	waits, early := r5SnapshotWaitOrDone(t, f, ctx, hold.Conn().PgConn().PID(), done)
	must(t, early)
	if waits {
		pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
		r5AwaitSentDeadline(t, f, ctx, pid, deadline)
	}
	must(t, hold.Rollback(ctx))
	if waits {
		r5AwaitOperation(t, ctx, done)
	}
	if !waits || allowed {
		t.Fatalf("key expiry was not checked after mailbox wait: waited=%v allowed=%v", waits, allowed)
	}
}

func TestR5LegacyRecipientProjectionRechecksAfterLedgerWait(t *testing.T) {
	f := seedCompany(t)
	j := r5LegacyJob(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE outbound_recipients IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	done := make(chan error, 1)
	var body string
	req := httptest.NewRequest("GET", "/api/v1/company/outbound/"+j.ID.String()+"/recipients", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	go func() {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			done <- fmt.Errorf("recipient response status %d", rr.Code)
			return
		}
		body = rr.Body.String()
		done <- nil
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_recipients")
	_, e = f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
	must(t, e)
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	if strings.Contains(body, "hidden@fixture.test") {
		t.Fatal("ledger wait reused old BCC visibility")
	}
}

func TestR5LegacyContentPropagatesStorageFailure(t *testing.T) {
	f := seedCompany(t)
	j := r5LegacyJob(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	allowed, e := submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(ctx, f.u, j)
	if allowed || !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled decision did not fail closed")
	}
	// Removing the authoritative table is a fixture-local database failure, not
	// proof that an absent archive is readable through the legacy job.
	_, e = f.pool.Exec(context.Background(), `ALTER TABLE sent_mail_items RENAME TO unavailable_sent_items`)
	must(t, e)
	allowed, e = submissions.NewService(nil, f.st, nil, zerolog.Nop()).ContentAllowed(context.Background(), f.u, j)
	if allowed || e == nil {
		t.Fatal("authority failure fell back to job content")
	}
	if v, ok := app.As(e); ok && v.Kind == app.KindNotFound {
		t.Fatal("database failure was disguised as absence")
	}
}
