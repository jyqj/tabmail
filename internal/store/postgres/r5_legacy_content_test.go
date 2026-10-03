package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
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

// Keep the wire allowlist independent of the product DTO: adding a field to the
// DTO must not silently expand the receipt contract, including nested objects.
func r5LegacyReceiptObject(t *testing.T, raw json.RawMessage, allowed, required string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	must(t, json.Unmarshal(raw, &fields))
	allow := make(map[string]bool)
	for _, name := range strings.Fields(allowed) {
		allow[name] = true
	}
	for name := range fields {
		if !allow[name] {
			t.Fatalf("receipt field is not allowlisted: %s", name)
		}
	}
	for _, name := range strings.Fields(required) {
		if value, ok := fields[name]; !ok || string(value) == "null" {
			t.Fatalf("receipt required field is absent or null: %s", name)
		}
	}
	return fields
}

func r5LegacyKeyReceipt(t *testing.T, w *httptest.ResponseRecorder, j *models.OutboundJob, viewContent bool) company.OutboundReceipt {
	t.Helper()
	raw := w.Body.Bytes()
	for _, private := range append([]string{"PRIVATE_LEGACY", j.Subject, j.MailFrom}, j.RcptTo...) {
		if private != "" && strings.Contains(string(raw), private) {
			t.Fatal("ordinary key receipt exposed subject, content, address or diagnostics")
		}
	}
	env := r5LegacyReceiptObject(t, raw, "data", "data")
	fields := r5LegacyReceiptObject(t, env["data"],
		"id tenant_id state status progress created_at updated_at attempt_count next_retry delivery_uncertain capabilities",
		"id tenant_id state status progress created_at updated_at attempt_count delivery_uncertain capabilities")
	progress := r5LegacyReceiptObject(t, fields["progress"], "completeness counts", "completeness counts")
	r5LegacyReceiptObject(t, progress["counts"], "total accepted pending temporary permanent uncertain", "total accepted pending temporary permanent uncertain")
	r5LegacyReceiptObject(t, fields["capabilities"], "view_content retry retry_block_reason", "view_content retry retry_block_reason")
	var receipt company.OutboundReceipt
	must(t, json.Unmarshal(env["data"], &receipt))
	if receipt.ID != j.ID || receipt.TenantID == nil || *receipt.TenantID != j.TenantID || receipt.State != models.OutboundSent || receipt.Status != company.SubmissionNeedsAttention {
		t.Fatal("ordinary key receipt lost its exact job, tenant or conservative ledger status")
	}
	// CreateOutboundJob records three pending recipients. A sent job alone is
	// not proof of next-hop acceptance; the complete ledger remains authoritative.
	wantCounts := company.OutboundReceiptCounts{Total: 3, Pending: 3}
	if receipt.Progress.Completeness != "known" || receipt.Progress.Counts == nil || *receipt.Progress.Counts != wantCounts || receipt.DeliveryUncertain {
		t.Fatal("ordinary key receipt changed or filtered the full recipient aggregate")
	}
	if receipt.CreatedAt == nil || receipt.CreatedAt.IsZero() || receipt.UpdatedAt == nil || receipt.UpdatedAt.IsZero() || receipt.AttemptCount == nil || *receipt.AttemptCount != 0 || receipt.NextRetry != nil {
		t.Fatal("ordinary key receipt lost safe operation metadata")
	}
	if receipt.Capabilities == nil || receipt.Capabilities.ViewContent != viewContent || receipt.Capabilities.Retry || receipt.Capabilities.RetryBlockReason != "state_not_retryable" {
		t.Fatal("ordinary key receipt advertised incorrect current-content or retry authority")
	}
	return receipt
}

func r5LegacyKeyHTTP(t *testing.T, h http.Handler, token, path string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-API-Key", token) // Never attach a JWT or synthesize a user actor.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("key GET %s: wanted %d got %d", path, status, w.Code)
	}
	return w
}

func TestR5LegacyContentKeyHTTPPreservesSafeReceipt(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("legacy key HTTP contract requires owned TABMAIL_TEST_DB_DSN")
	}
	// Each boundary owns its fixture and runs even if another leaf fails.
	for _, mode := range []string{"receipt-live", "receipt-expired", "send-write-only", "deleted-key", "expired-key", "cross-tenant-key", "changed-owner", "owner-jwt-content-live", "owner-jwt-content-expired", "api-key-content-forbidden"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			k, _ := r5LegacyKey(t, f)
			// Use a real finite-retention mailbox and current owner read grant.
			// The normal enqueue transaction/trigger creates the archive and TTL;
			// no permanent personal item is given an invented expiry.
			hours := 1
			mailbox, err := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "legacy-key-receipt", Kind: "shared", RetentionHours: &hours})
			must(t, err)
			must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mailbox.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			jobFixture := *f
			jobFixture.personal = mailbox // Local copy; do not mutate the shared fixture.
			j := r5LegacyJob(t, &jobFixture, &k.ID)
			var finite bool
			must(t, f.pool.QueryRow(ctx, `SELECT m.mailbox_kind='shared' AND m.retention_hours_override=1
 AND i.expires_at IS NOT NULL AND i.expires_at=a.created_at+make_interval(hours=>m.retention_hours_override)
 AND i.expires_at>clock_timestamp()
 FROM sent_mail_items i JOIN sent_mail_assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id
 JOIN mailboxes m ON m.tenant_id=i.tenant_id AND m.id=i.mailbox_id
 WHERE i.tenant_id=$1 AND i.asset_id=$2 AND i.mailbox_id=$3`, f.tenant.ID, j.ID, mailbox.ID).Scan(&finite))
			if !finite {
				t.Fatal("enqueue trigger did not produce the configured live finite archive")
			}
			svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			token := "tm_content_" + k.ID.String()
			path := "/api/v1/outbound/" + j.ID.String()
			contentPath := "/api/v1/company/submissions/" + j.ID.String() + "/content"
			keyReceipt := func(viewContent bool) company.OutboundReceipt {
				return r5LegacyKeyReceipt(t, r5LegacyKeyHTTP(t, h, token, path, http.StatusOK), j, viewContent)
			}
			expireContent := func() {
				t.Helper()
				tag, err := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp()-interval '1 second'
 WHERE tenant_id=$1 AND asset_id=$2 AND mailbox_id=$3 AND expires_at IS NOT NULL`, f.tenant.ID, j.ID, mailbox.ID)
				must(t, err)
				if tag.RowsAffected() != 1 {
					t.Fatal("expiry did not advance exactly the fixture's original finite item")
				}
			}
			ownerContent := func() {
				t.Helper()
				content := r3Data[company.SubmissionContent](t, r3HTTP(t, h, r3Token(t, f.employee), http.MethodGet, contentPath, nil, http.StatusOK))
				if content.ID != j.ID || content.Subject != j.Subject || content.MailFrom != j.MailFrom || content.TextBody != j.TextBody || content.HTMLBody != j.HTMLBody || content.ContentRedacted {
					t.Fatal("real owner JWT did not receive the live archive on the independent content route")
				}
			}
			switch mode {
			case "receipt-live":
				keyReceipt(true)
			case "receipt-expired":
				before := keyReceipt(true)
				expireContent()
				after := keyReceipt(false)
				before.Capabilities, after.Capabilities = nil, nil
				if !reflect.DeepEqual(before, after) {
					t.Fatal("content expiry changed durable safe receipt metadata")
				}
			case "send-write-only":
				tag, err := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1 AND tenant_id=$2`, k.ID, f.tenant.ID)
				must(t, err)
				if tag.RowsAffected() != 1 {
					t.Fatal("fixture key scope was not changed")
				}
				r5LegacyKeyHTTP(t, h, token, path, http.StatusForbidden)
			case "deleted-key":
				must(t, f.st.DeleteAPIKey(ctx, k.ID))
				r5LegacyKeyHTTP(t, h, token, path, http.StatusUnauthorized)
			case "expired-key":
				var deadline time.Time
				must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1 AND tenant_id=$2 RETURNING expires_at`, k.ID, f.tenant.ID).Scan(&deadline))
				keyReceipt(true)
				// Shorten this credential's actual TTL, never a process-clock sleep.
				must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1 AND tenant_id=$2 AND expires_at=$3 RETURNING expires_at`, k.ID, f.tenant.ID, deadline).Scan(&deadline))
				r5LegacyKeyHTTP(t, h, token, path, http.StatusUnauthorized)
			case "cross-tenant-key":
				foreign := &models.Tenant{Name: "Foreign receipt tenant", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreign))
				foreignKey := &models.TenantAPIKey{ID: uuid.New(), TenantID: foreign.ID, KeyPrefix: "fixture", Label: "foreign receipt key", Scopes: []string{"send:read"}}
				foreignToken := "tm_content_" + foreignKey.ID.String()
				foreignKey.KeyHash = company.Hash(foreignToken)
				must(t, f.st.CreateAPIKey(ctx, foreignKey))
				// Prove authentication succeeds, rather than accepting a 401 as isolation.
				foreignList := r5LegacyKeyHTTP(t, h, foreignToken, "/api/v1/outbound", http.StatusOK)
				if rows := r3Data[[]json.RawMessage](t, foreignList); len(rows) != 0 {
					t.Fatal("foreign tenant key listed another tenant's operation")
				}
				r5LegacyKeyHTTP(t, h, foreignToken, path, http.StatusNotFound)
			case "changed-owner":
				before := keyReceipt(true)
				tag, err := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$3 WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$4`, k.ID, f.tenant.ID, f.other.ID, f.employee.ID)
				must(t, err)
				if tag.RowsAffected() != 1 {
					t.Fatal("fixture key owner was not changed")
				}
				// The key still owns the operation; its new owner cannot read the mailbox.
				after := keyReceipt(false)
				before.Capabilities, after.Capabilities = nil, nil
				if !reflect.DeepEqual(before, after) {
					t.Fatal("key-owner change altered safe operation identity or metadata")
				}
				r3HTTP(t, h, r3Token(t, f.other), http.MethodGet, contentPath, nil, http.StatusNotFound)
				ownerContent()
			case "owner-jwt-content-live":
				ownerContent()
			case "owner-jwt-content-expired":
				ownerContent()
				expireContent()
				r3HTTP(t, h, r3Token(t, f.employee), http.MethodGet, contentPath, nil, http.StatusNotFound)
			case "api-key-content-forbidden":
				// A true receipt hint must not turn the key into an interactive JWT user.
				r5LegacyKeyHTTP(t, h, token, contentPath, http.StatusForbidden)
			}
		})
	}
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
	j := r5ReceiptBoundaryFiniteJob(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	path := "/api/v1/company/outbound/" + j.ID.String() + "/recipients"
	contentPath := "/api/v1/company/submissions/" + j.ID.String() + "/content"
	before := r5ReceiptBoundaryView(t, r3HTTP(t, h, token, "GET", path, nil, 200), j, false, true, "state_not_retryable")
	content := r3Data[company.SubmissionContent](t, r3HTTP(t, h, token, "GET", contentPath, nil, 200))
	if content.ID != j.ID || content.TextBody != j.TextBody || content.ContentRedacted {
		t.Fatal("owner JWT did not read the live canonical archive before the wait")
	}
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE outbound_recipients IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	done, rr := r5ReceiptRequest(ctx, h, path, token, "")
	// The table name occurs beyond pg_stat_activity's default query prefix.
	// Keep the real ledger lock and exact blocker PID; observe the SQL prefix
	// instead of widening PostgreSQL configuration or weakening the barrier.
	reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "WITH visible AS MATERIALIZED")
	t.Logf("ledger-lock receipt waiter=%d exact blocker=%d", reader, hold.Conn().PgConn().PID())
	r5ReceiptBoundaryStillWaiting(t, done)
	r5ReceiptBoundaryExpire(t, f, ctx, j)
	t.Log("finite item expiry committed before ledger blocker release")
	r5ReceiptBoundaryStillWaiting(t, done)
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	if rr.Code != 200 {
		t.Fatalf("legitimate operation receipt disappeared after ledger wait: %d", rr.Code)
	}
	after := r5ReceiptBoundaryView(t, rr, j, false, false, "state_not_retryable")
	r5ReceiptBoundarySameMetadata(t, before, after)
	r3HTTP(t, h, token, "GET", contentPath, nil, 404)
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
