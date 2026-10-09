package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app/submissions"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func TestR5OutboundReceiptRejectsStalePrincipal(t *testing.T) {
	for _, mode := range []string{"live-user", "frozen-user", "missing-user", "restricted-user", "demoted-admin", "frozen-admin", "live-key", "deleted-key", "expired-key", "write-only-key", "key-owner-changed", "key-owner-frozen", "key-zone", "owner-zone", "unknown-admin"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			j := r5LegacyJob(t, f)
			a := f.u
			var k *models.TenantAPIKey
			if strings.Contains(mode, "key") || mode == "owner-zone" {
				k, a = r5LegacyKey(t, f)
				j = r5LegacyJob(t, f, &k.ID)
			}
			switch mode {
			case "frozen-user":
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				must(t, e)
			case "missing-user": // Keep a historical receipt without relying on FK deletion policy.
				a.ID = uuid.New()
				_, e := f.pool.Exec(ctx, `UPDATE outbound_jobs SET user_id=NULL WHERE id=$1`, j.ID)
				must(t, e)
				a.IsAdmin = true
			case "restricted-user", "owner-zone":
				p := r5SnapshotProfile(t, f, true, false)
				p.AllowedZoneIDs = []uuid.UUID{uuid.New()}
				must(t, f.st.UpdatePermissionProfile(ctx, p))
			case "demoted-admin":
				a = f.a
				_, e := f.pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.admin.ID)
				must(t, e)
			case "frozen-admin":
				a = f.a
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
				must(t, e)
			case "deleted-key":
				must(t, f.st.DeleteAPIKey(ctx, k.ID))
			case "expired-key":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp() WHERE id=$1`, k.ID)
				must(t, e)
			case "write-only-key":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, k.ID)
				must(t, e)
			case "key-owner-changed":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=$2 WHERE id=$1`, k.ID, f.other.ID)
				must(t, e)
			case "key-owner-frozen":
				_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				must(t, e)
			case "key-zone":
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET allowed_zone_ids=$2 WHERE id=$1`, k.ID, []uuid.UUID{uuid.New()})
				must(t, e)
			case "unknown-admin":
				a.Type = "unknown"
				a.IsAdmin = true
			}
			v, e := submissions.NewService(nil, f.st, nil, zerolog.Nop()).AccessibleOutboundJob(ctx, f.tenant, a, j.ID)
			if mode == "live-user" || mode == "live-key" {
				must(t, e)
				if v == nil {
					t.Fatal("live principal lost receipt")
				}
				return
			}
			if e == nil || v != nil {
				t.Fatalf("%s: stale principal returned receipt, err=%v", mode, e)
			}
		})
	}
}

// Run the real router in a goroutine without calling testing.Fatal there.
func r5ReceiptRequest(ctx context.Context, h http.Handler, path, token, key string) (<-chan error, *httptest.ResponseRecorder) {
	req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	rr := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() { h.ServeHTTP(rr, req); done <- nil }()
	return done, rr
}

// These boundary-only helpers do not change the already accepted key10 fixture.
// Inspect raw JSON before decoding the DTO, so a future DTO field cannot silently
// widen the ordinary receipt contract, including list items and nested objects.
func r5ReceiptBoundaryView(t *testing.T, w *httptest.ResponseRecorder, j *models.OutboundJob, list, viewContent bool, retryReason string) company.OutboundReceipt {
	t.Helper()
	raw := w.Body.Bytes()
	for _, private := range append([]string{"PRIVATE_", j.Subject, j.MailFrom}, j.RcptTo...) {
		if private != "" && strings.Contains(string(raw), private) {
			t.Fatal("ordinary receipt leaked content, subject, address or protocol diagnostics")
		}
	}
	fieldsAllowed, fieldsRequired := "data", "data"
	if list {
		fieldsAllowed, fieldsRequired = "data meta", "data meta"
	}
	env := r5LegacyReceiptObject(t, raw, fieldsAllowed, fieldsRequired)
	data := env["data"]
	if list {
		r5LegacyReceiptObject(t, env["meta"], "total page per_page", "total page per_page")
		var meta struct {
			Total   int `json:"total"`
			Page    int `json:"page"`
			PerPage int `json:"per_page"`
		}
		must(t, json.Unmarshal(env["meta"], &meta))
		if meta.Total != 1 || meta.Page != 1 || meta.PerPage < 1 {
			t.Fatal("receipt list lost exact fixture total or pagination metadata")
		}
		var rows []json.RawMessage
		must(t, json.Unmarshal(data, &rows))
		if len(rows) != 1 {
			t.Fatalf("expected exactly the fixture receipt, got %d rows", len(rows))
		}
		data = rows[0]
	}
	fields := r5LegacyReceiptObject(t, data,
		"id tenant_id state status progress created_at updated_at attempt_count next_retry delivery_uncertain capabilities",
		"id tenant_id state status progress created_at updated_at attempt_count delivery_uncertain capabilities")
	progress := r5LegacyReceiptObject(t, fields["progress"], "completeness counts", "completeness counts")
	r5LegacyReceiptObject(t, progress["counts"], "total accepted pending temporary permanent uncertain", "total accepted pending temporary permanent uncertain")
	r5LegacyReceiptObject(t, fields["capabilities"], "view_content retry retry_block_reason", "view_content retry retry_block_reason")
	var v company.OutboundReceipt
	must(t, json.Unmarshal(data, &v))
	status := company.SubmissionNeedsAttention
	if j.State == models.OutboundPending {
		status = company.SubmissionSubmitted
	}
	if v.ID != j.ID || v.TenantID == nil || *v.TenantID != j.TenantID || v.State != j.State || v.Status != status {
		t.Fatal("receipt lost exact identity, tenant, state or conservative ledger status")
	}
	counts := company.OutboundReceiptCounts{Total: len(j.RcptTo), Pending: len(j.RcptTo)}
	if v.Progress.Completeness != "known" || v.Progress.Counts == nil || *v.Progress.Counts != counts || v.DeliveryUncertain {
		t.Fatal("receipt lost the complete pending recipient aggregate")
	}
	if v.CreatedAt == nil || v.CreatedAt.IsZero() || v.UpdatedAt == nil || v.UpdatedAt.IsZero() || v.AttemptCount == nil || *v.AttemptCount != j.Attempts || v.NextRetry != nil {
		t.Fatal("receipt lost durable operation metadata")
	}
	if v.Capabilities == nil || v.Capabilities.ViewContent != viewContent || v.Capabilities.Retry || v.Capabilities.RetryBlockReason != retryReason {
		t.Fatal("receipt confused current content, retry and operation-read authority")
	}
	return v
}

func r5ReceiptBoundarySameMetadata(t *testing.T, before, after company.OutboundReceipt) {
	t.Helper()
	before.Capabilities, after.Capabilities = nil, nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("content authority change altered durable safe receipt metadata")
	}
}

// Retention is configured through the real mailbox API before enqueue. Verify
// the real archive trigger created an already-finite item; never invent an
// expiry for the permanently retained personal mailbox used by other tests.
func r5ReceiptBoundaryFiniteJob(t *testing.T, f *companyFixture) *models.OutboundJob {
	t.Helper()
	ctx := context.Background()
	hours := 1
	mailbox, err := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "receipt-boundary", Kind: "shared", RetentionHours: &hours})
	must(t, err)
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mailbox.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	copyFixture := *f
	copyFixture.personal = mailbox
	j := r5LegacyJob(t, &copyFixture)
	var finite bool
	must(t, f.pool.QueryRow(ctx, `SELECT i.expires_at IS NOT NULL AND i.expires_at=a.created_at+make_interval(hours=>m.retention_hours_override)
 AND i.expires_at>clock_timestamp() AND m.mailbox_kind='shared' AND m.retention_hours_override=1
 FROM sent_mail_items i JOIN sent_mail_assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id
 JOIN mailboxes m ON m.tenant_id=i.tenant_id AND m.id=i.mailbox_id
 WHERE i.tenant_id=$1 AND i.asset_id=$2 AND i.mailbox_id=$3`, j.TenantID, j.ID, mailbox.ID).Scan(&finite))
	if !finite {
		t.Fatal("normal enqueue did not create the configured finite archive")
	}
	return j
}

func r5ReceiptBoundaryExpire(t *testing.T, f *companyFixture, ctx context.Context, j *models.OutboundJob) {
	t.Helper()
	tag, err := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp()-interval '1 second'
 WHERE tenant_id=$1 AND asset_id=$2 AND mailbox_id=$3 AND expires_at IS NOT NULL`, j.TenantID, j.ID, j.SenderMailboxID)
	must(t, err)
	if tag.RowsAffected() != 1 {
		t.Fatal("expiry did not shorten exactly the original finite item")
	}
}

func r5ReceiptBoundaryStillWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("receipt request completed before its exact blocker was released: %v", err)
	default:
	}
}

func TestR5OutboundReceiptKeyExpiryAfterQueryWait(t *testing.T) {
	for _, pathMode := range []string{"detail", "list", "attempts"} {
		t.Run(pathMode, func(t *testing.T) {
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			k, _ := r5LegacyKey(t, f)
			j := r5LegacyJob(t, f, &k.ID)
			must(t, f.st.CreateOutboundAttempt(ctx, &models.OutboundAttempt{TenantID: j.TenantID, JobID: j.ID, Attempt: 1, SMTPCode: 451, SMTPResponse: "PRIVATE_ATTEMPT_SMTP", RemoteHost: "PRIVATE_ATTEMPT_HOST", Error: "PRIVATE_ATTEMPT_ERROR"}))
			_, e := f.pool.Exec(ctx, `UPDATE outbound_jobs SET attempts=1 WHERE id=$1 AND tenant_id=$2`, j.ID, j.TenantID)
			must(t, e)
			j.Attempts = 1
			svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			path := "/api/v1/outbound"
			if pathMode != "list" {
				path += "/" + j.ID.String()
			}
			if pathMode == "attempts" {
				path += "/attempts"
			}
			key := "tm_content_" + k.ID.String()
			reason := "state_not_retryable"
			if pathMode == "list" {
				reason = "unknown"
			}
			// Fresh-key positive control uses the same real endpoint. Attempts is a
			// receipt with attempt_count, not a per-attempt protocol/host array.
			r5ReceiptBoundaryView(t, r5LegacyKeyHTTP(t, h, key, path, 200), j, pathMode == "list", true, reason)
			var deadline time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, k.ID).Scan(&deadline))
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			// All three shipping endpoints read the ordinary receipt query. The
			// attempts endpoint no longer reads/locks outbound_attempts.
			_, e = hold.Exec(ctx, `LOCK TABLE outbound_jobs IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			done, rr := r5ReceiptRequest(ctx, h, path, "", key)
			pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "WITH visible AS MATERIALIZED")
			t.Logf("%s receipt waiter=%d exact blocker=%d key deadline=%s", pathMode, pid, hold.Conn().PgConn().PID(), deadline.UTC().Format(time.RFC3339Nano))
			r5ReceiptBoundaryStillWaiting(t, done)
			r5AwaitSentDeadline(t, f, ctx, pid, deadline)
			t.Log("database clock reached key deadline before receipt blocker release")
			r5ReceiptBoundaryStillWaiting(t, done)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
			if rr.Code != 403 && rr.Code != 404 {
				t.Fatalf("key expired across real receipt wait must fail closed, got %d", rr.Code)
			}
		})
	}
}

func TestR5OutboundReceiptListOrdersMemberFreeze(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	j := r5LegacyJob(t, f)
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE outbound_jobs IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	done, rr := r5ReceiptRequest(ctx, h, "/api/v1/outbound", token, "")
	reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "WITH visible AS MATERIALIZED")
	changed := make(chan error, 1)
	go func() {
		_, err := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
		changed <- err
	}()
	waits, early := r5SnapshotWaitOrDone(t, f, ctx, reader, changed)
	must(t, early)
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	if waits {
		r5AwaitOperation(t, ctx, changed)
	}
	if !waits {
		// Subject is never in this DTO. Any 200 (even an empty list) after
		// the freeze won the ordering race would accept the stale principal.
		if rr.Code != 401 && rr.Code != 403 && rr.Code != 404 {
			t.Fatalf("freeze committed before receipt query, stale actor returned status %d", rr.Code)
		}
	} else {
		if rr.Code != 200 {
			t.Fatalf("reader ordered before freeze unexpectedly failed: %d", rr.Code)
		}
		r5ReceiptBoundaryView(t, rr, j, true, true, "unknown")
	}
	r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 401)
}

func TestR5OutboundReceiptListMatchesDetailScope(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := r5ReceiptBoundaryFiniteJob(t, f)
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: *j.SenderMailboxID, UserID: f.other.ID, CanRead: true}))
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token, owner := r3Token(t, f.other), r3Token(t, f.employee)
	path := "/api/v1/outbound/" + j.ID.String()
	contentPath := "/api/v1/company/submissions/" + j.ID.String() + "/content"
	before := r5ReceiptBoundaryView(t, r3HTTP(t, h, token, "GET", path, nil, 200), j, false, true, "state_not_retryable")
	r5ReceiptBoundaryView(t, r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 200), j, true, true, "unknown")
	content := r3Data[company.SubmissionContent](t, r3HTTP(t, h, owner, "GET", contentPath, nil, 200))
	if content.ID != j.ID || content.TextBody != j.TextBody || content.ContentRedacted {
		t.Fatal("real owner JWT lost the live canonical archive")
	}
	r5ReceiptBoundaryExpire(t, f, ctx, j)
	r3HTTP(t, h, token, "GET", path, nil, 404)
	rr := r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 200)
	if rows := r3Data[[]json.RawMessage](t, rr); len(rows) != 0 || strings.Contains(rr.Body.String(), j.ID.String()) {
		t.Fatal("list revealed a shared receipt denied by the detail endpoint")
	}
	// Operation ownership preserves only metadata, never expired content.
	after := r5ReceiptBoundaryView(t, r3HTTP(t, h, owner, "GET", path, nil, 200), j, false, false, "state_not_retryable")
	r5ReceiptBoundarySameMetadata(t, before, after)
	listed := r5ReceiptBoundaryView(t, r3HTTP(t, h, owner, "GET", "/api/v1/outbound", nil, 200), j, true, false, "unknown")
	r5ReceiptBoundarySameMetadata(t, after, listed)
	r3HTTP(t, h, owner, "GET", contentPath, nil, 404)
	r3HTTP(t, h, token, "GET", contentPath, nil, 404)
}

func TestR5OutboundReceiptOwnerlessKeyAndRetryScope(t *testing.T) {
	for _, mode := range []string{"ownerless", "write-only-retry"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			k, _ := r5LegacyKey(t, f)
			var j *models.OutboundJob
			if mode == "ownerless" {
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET owner_user_id=NULL WHERE id=$1`, k.ID)
				must(t, e)
				j = &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, APIKeyID: &k.ID, SenderKeyID: &k.ID, MailFrom: f.personal.FullAddress, To: []string{"fixture@receiver.test"}, RcptTo: []string{"fixture@receiver.test"}, Subject: "integration receipt", TextBody: "PRIVATE_OWNERLESS", State: models.OutboundDead}
				must(t, f.st.CreateOutboundJob(ctx, j))
			} else {
				j = r5LegacyJob(t, f, &k.ID)
				_, e := f.pool.Exec(ctx, `UPDATE outbound_jobs SET state='dead' WHERE id=$1`, j.ID)
				must(t, e)
			}
			svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			call := func(method, path string, status int) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("X-API-Key", "tm_content_"+k.ID.String())
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				if rr.Code != status {
					t.Fatalf("%s status %d wanted %d: %s", method, rr.Code, status, rr.Body.String())
				}
				return rr
			}
			path := "/api/v1/outbound/" + j.ID.String()
			if mode == "ownerless" {
				rr := call("GET", path, 200)
				detail := r5ReceiptBoundaryView(t, rr, j, false, false, "sender_authority")
				listed := r5ReceiptBoundaryView(t, call("GET", "/api/v1/outbound", 200), j, true, false, "unknown")
				r5ReceiptBoundarySameMetadata(t, detail, listed)
				call("GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", 403)
				call("POST", path+"/retry", 403)
				for _, foreignTenant := range []bool{false, true} {
					tenantID := f.tenant.ID
					if foreignTenant {
						tenant := &models.Tenant{Name: "Foreign ownerless receipt", PlanID: f.tenant.PlanID}
						must(t, f.st.CreateTenant(ctx, tenant))
						tenantID = tenant.ID
					}
					otherKey := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenantID, KeyPrefix: "fixture", Label: "other ownerless key", Scopes: []string{"send:read"}}
					otherToken := "tm_content_" + otherKey.ID.String()
					otherKey.KeyHash = company.Hash(otherToken)
					must(t, f.st.CreateAPIKey(ctx, otherKey))
					otherList := r5LegacyKeyHTTP(t, h, otherToken, "/api/v1/outbound", 200)
					if rows := r3Data[[]json.RawMessage](t, otherList); len(rows) != 0 {
						t.Fatal("another key or tenant listed the ownerless operation")
					}
					r5LegacyKeyHTTP(t, h, otherToken, path, 404)
				}
			} else {
				call("POST", path+"/retry", 403)
				_, e := f.pool.Exec(ctx, `UPDATE tenant_api_keys SET scopes='["send:write"]'::jsonb WHERE id=$1`, k.ID)
				must(t, e)
				call("GET", path, 403)
				// Make the recorded address deterministic; normal API authentication
				// also records last-use metadata asynchronously.
				must(t, f.st.TouchAPIKey(ctx, k.ID, "192.0.2.10"))
				// This positive control requires a legitimately retryable persisted
				// job; expose sender validation failures instead of a generic HTTP500.
				currentJob, e := f.st.GetOutboundJob(ctx, j.ID)
				must(t, e)
				if e = svc.ValidateJobAuthorization(ctx, currentJob); e != nil {
					t.Fatalf("positive retry sender validation: %v", e)
				}
				retried := call("POST", path+"/retry", 200)
				updated, e := f.st.GetOutboundJob(ctx, j.ID)
				must(t, e)
				if updated == nil || updated.State != models.OutboundPending {
					t.Fatal("write-only retry did not reach existing queue transition")
				}
				r5ReceiptBoundaryView(t, retried, updated, false, true, "state_not_retryable")
			}
		})
	}
}

func TestR5OutboundReceiptStorageErrorAndTenantIsolation(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := r5LegacyJob(t, f)
	tenant := &models.Tenant{Name: "Foreign receipt company", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "foreign-receipt.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, zone))
	foreign := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, MailFrom: "sender@foreign-receipt.test", To: []string{"client@fixture.test"}, RcptTo: []string{"client@fixture.test"}, Subject: "FOREIGN_RECEIPT_MARKER", State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(ctx, foreign))
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.admin)
	r3HTTP(t, h, token, "GET", "/api/v1/outbound/"+foreign.ID.String(), nil, 404)
	rr := r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 200)
	if strings.Contains(rr.Body.String(), "FOREIGN_RECEIPT_MARKER") {
		t.Fatal("administrator crossed tenant scope")
	}
	// Fail the source query inside this disposable fixture, not the identity gate.
	_, e := f.pool.Exec(ctx, `ALTER TABLE outbound_jobs RENAME TO unavailable_receipt_jobs`)
	must(t, e)
	for _, path := range []string{"/api/v1/outbound", "/api/v1/outbound/" + j.ID.String()} {
		rr := r3HTTP(t, h, token, "GET", path, nil, 500)
		if strings.Contains(rr.Body.String(), "safe receipt") || strings.Contains(rr.Body.String(), "unavailable_receipt_jobs") || strings.Contains(rr.Body.String(), "PRIVATE_LEGACY") {
			t.Fatal("failed page leaked partial records or storage details")
		}
	}
}

func TestR5APIKeyLastUsedIPRoundTrips(t *testing.T) {
	for _, address := range []string{"", "192.0.2.10", "2001:db8::42"} {
		t.Run(address, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			k, _ := r5LegacyKey(t, f)
			if address != "" {
				must(t, f.st.TouchAPIKey(ctx, k.ID, address))
			}
			verify := func(label string, got *models.TenantAPIKey, err error) {
				t.Helper()
				if err != nil {
					t.Errorf("%s metadata failed after use: %v", label, err)
					return
				}
				if got == nil {
					t.Errorf("%s key disappeared", label)
					return
				}
				if address == "" {
					if got.LastUsedIP != nil {
						t.Errorf("%s unset IP not nil", label)
					}
				} else if got.LastUsedIP == nil || *got.LastUsedIP != address || got.LastUsedAt == nil {
					t.Errorf("%s incorrect last-used metadata", label)
				}
				if got.KeyHash != "" {
					t.Errorf("%s exposed key hash", label)
				}
			}
			got, e := f.st.GetAPIKey(ctx, k.ID)
			verify("get", got, e)
			for _, ownerOnly := range []bool{false, true} {
				label := "tenant-list"
				var rows []*models.TenantAPIKey
				if ownerOnly {
					label = "owner-list"
					rows, e = f.st.ListAPIKeysByOwner(ctx, f.tenant.ID, f.employee.ID)
				} else {
					rows, e = f.st.ListAPIKeys(ctx, f.tenant.ID)
				}
				var match *models.TenantAPIKey
				for _, row := range rows {
					if row.ID == k.ID {
						match = row
					}
				}
				verify(label, match, e)
			}
		})
	}
}

func TestR5OutboundReceiptStablePaginationAndEmptyPage(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	ids := []uuid.UUID{}
	for i := 0; i < 3; i++ {
		j := r5LegacyJob(t, f)
		ids = append(ids, j.ID)
	}
	_, e := f.pool.Exec(ctx, `UPDATE outbound_jobs SET created_at='2026-01-01T00:00:00Z' WHERE id=ANY($1)`, ids)
	must(t, e)
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	seen := map[uuid.UUID]bool{}
	for page := 1; page <= 4; page++ {
		rr := r3HTTP(t, h, token, "GET", fmt.Sprintf("/api/v1/outbound?page=%d&per_page=1", page), nil, 200)
		var envelope struct {
			Data []models.OutboundJob `json:"data"`
			Meta struct {
				Total int `json:"total"`
			} `json:"meta"`
		}
		must(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
		if envelope.Meta.Total != 3 {
			t.Fatalf("page %d: total=%d body=%s", page, envelope.Meta.Total, rr.Body.String())
		}
		if page == 4 {
			if len(envelope.Data) != 0 {
				t.Fatal("beyond-end page was not empty")
			}
			continue
		}
		if len(envelope.Data) != 1 || seen[envelope.Data[0].ID] {
			t.Fatal("pagination repeated or dropped an item")
		}
		seen[envelope.Data[0].ID] = true
	}
}
