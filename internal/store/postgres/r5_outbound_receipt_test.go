package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app/submissions"
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

func TestR5OutboundReceiptKeyExpiryAfterQueryWait(t *testing.T) {
	for _, pathMode := range []string{"detail", "list", "attempts"} {
		t.Run(pathMode, func(t *testing.T) {
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			k, _ := r5LegacyKey(t, f)
			j := r5LegacyJob(t, f, &k.ID)
			svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
			h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
			var deadline time.Time
			must(t, f.pool.QueryRow(ctx, `UPDATE tenant_api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, k.ID).Scan(&deadline))
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			table := "outbound_jobs"
			if pathMode == "attempts" {
				table = "outbound_attempts"
			}
			_, e = hold.Exec(ctx, `LOCK TABLE `+table+` IN ACCESS EXCLUSIVE MODE`)
			must(t, e)
			path := "/api/v1/outbound"
			if pathMode != "list" {
				path += "/" + j.ID.String()
			}
			if pathMode == "attempts" {
				path += "/attempts"
			}
			done, rr := r5ReceiptRequest(ctx, h, path, "", "tm_content_"+k.ID.String())
			pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), table)
			r5AwaitSentDeadline(t, f, ctx, pid, deadline)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
			if rr.Code == 200 && strings.Contains(rr.Body.String(), "safe receipt") {
				t.Fatal("expired key received receipt metadata after database wait")
			}
			if pathMode == "attempts" && rr.Code == 200 {
				t.Fatal("expired key still received attempts after wait")
			}
			if rr.Code != 403 && rr.Code != 404 {
				t.Fatalf("invalid key should fail closed, got %d", rr.Code)
			}
		})
	}
}

func TestR5OutboundReceiptListOrdersMemberFreeze(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	r5LegacyJob(t, f)
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.employee)
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `LOCK TABLE outbound_jobs IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	done, rr := r5ReceiptRequest(ctx, h, "/api/v1/outbound", token, "")
	reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "outbound_jobs")
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
	if !waits && rr.Code == 200 && strings.Contains(rr.Body.String(), "safe receipt") {
		t.Fatal("freeze committed before receipt query, stale actor still returned metadata")
	}
	if waits && rr.Code != 200 {
		t.Fatalf("reader ordered before freeze unexpectedly failed: %d", rr.Code)
	}
	r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 401)
}

func TestR5OutboundReceiptListMatchesDetailScope(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := r5LegacyJob(t, f)
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true}))
	svc := outbound.NewService(config.Outbound{Enabled: true}, f.st, f.st, zerolog.Nop())
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), svc)
	token := r3Token(t, f.other)
	r3HTTP(t, h, token, "GET", "/api/v1/outbound/"+j.ID.String(), nil, 200)
	_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
	must(t, e)
	r3HTTP(t, h, token, "GET", "/api/v1/outbound/"+j.ID.String(), nil, 404)
	rr := r3HTTP(t, h, token, "GET", "/api/v1/outbound", nil, 200)
	if strings.Contains(rr.Body.String(), j.ID.String()) {
		t.Fatal("list revealed a shared receipt denied by the detail endpoint")
	}
	// The historical submitter's own receipt must remain independently visible.
	rr = r3HTTP(t, h, r3Token(t, f.employee), "GET", "/api/v1/outbound/"+j.ID.String(), nil, 200)
	if !strings.Contains(rr.Body.String(), "safe receipt") || strings.Contains(rr.Body.String(), "PRIVATE_LEGACY") {
		t.Fatal("owner receipt lifetime/content split regressed")
	}
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
				if strings.Contains(rr.Body.String(), "PRIVATE_OWNERLESS") || !strings.Contains(rr.Body.String(), "integration receipt") {
					t.Fatal("ownerless key content/receipt distinction changed")
				}
				call("GET", "/api/v1/outbound", 200)
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
				call("POST", path+"/retry", 200)
				updated, e := f.st.GetOutboundJob(ctx, j.ID)
				must(t, e)
				if updated.State != models.OutboundPending {
					t.Fatal("write-only retry did not reach existing queue transition")
				}
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
