//go:build r5audit

package postgres_test

// Pending R5 secure-behavior regressions: deliberately opt-in during P0 so
// known baseline defects do not redefine the ordinary suite's success criteria.
// Every assertion expects the SECURE result. The baseline driver accepts only
// the exact named failures and marker, never a compilation/setup failure.
// Move each case into the ordinary suite when the corresponding fix lands.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func r5AuditServer(t *testing.T, f *companyFixture) *httptest.Server {
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj) // Worker is never started; no email leaves this test.
	s := httptest.NewServer(companyRouter(t, f, obj, svc))
	t.Cleanup(s.Close)
	return s
}
func r5AuditHTTP(t *testing.T, s *httptest.Server, token, method, path string, body any) (int, []byte) {
	t.Helper()
	raw, e := json.Marshal(body)
	must(t, e)
	req, e := http.NewRequest(method, s.URL+path, bytes.NewReader(raw))
	must(t, e)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := s.Client()
	client.Timeout = 5 * time.Second
	res, e := client.Do(req)
	must(t, e)
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	must(t, e)
	if res.StatusCode >= 500 {
		t.Fatalf("unexpected server error: %d", res.StatusCode)
	}
	return res.StatusCode, b
}
func r5ExpectStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("setup status wanted %d got %d", want, got)
	}
}

func TestR5AuditA01OverridePreservesUneditedRestriction(t *testing.T) {
	f := seedCompany(t)
	s := r5AuditServer(t, f)
	token := r3Token(t, f.admin)
	p := "/api/v1/admin/users/" + f.employee.ID.String() + "/permissions"
	status, _ := r5AuditHTTP(t, s, token, "PUT", p, map[string]any{"can_send": false, "allowed_zone_ids": []uuid.UUID{f.zone.ID}})
	r5ExpectStatus(t, status, 200)
	before, e := f.st.EffectivePermission(context.Background(), f.employee.ID)
	must(t, e)
	if before.CanSend {
		t.Fatal("fixture was not restricted")
	}
	status, _ = r5AuditHTTP(t, s, token, "PUT", p, map[string]any{"daily_send_quota": 25})
	if status != 200 && status != 400 && status != 409 {
		t.Fatalf("unexpected patch response %d", status)
	}
	after, e := f.st.EffectivePermission(context.Background(), f.employee.ID)
	must(t, e)
	if after.CanSend || len(after.AllowedZoneIDs) != 1 {
		t.Fatal("R5_BASELINE_DEFECT_A01: quota-only request erased existing restriction")
	}
}
func TestR5AuditA02StaleProfileCannotRestoreRevocation(t *testing.T) {
	f := seedCompany(t)
	s := r5AuditServer(t, f)
	token := r3Token(t, f.admin)
	status, b := r5AuditHTTP(t, s, token, "POST", "/api/v1/admin/permissions", map[string]any{"name": "R5 audit", "can_send": true})
	r5ExpectStatus(t, status, 201)
	var data struct {
		Data models.PermissionProfile `json:"data"`
	}
	must(t, json.Unmarshal(b, &data))
	p := "/api/v1/admin/permissions/" + data.Data.ID.String()
	status, _ = r5AuditHTTP(t, s, token, "PATCH", p, map[string]any{"can_send": false})
	r5ExpectStatus(t, status, 200)
	revoked, e := f.st.GetPermissionProfile(context.Background(), data.Data.ID)
	must(t, e)
	if revoked.CanSend {
		t.Fatal("fixture revocation failed")
	}
	status, _ = r5AuditHTTP(t, s, token, "PATCH", p, map[string]any{"description": "unrelated edit from stale form", "can_send": true})
	if status != 200 && status != 400 && status != 409 {
		t.Fatalf("unexpected stale-write response %d", status)
	}
	current, e := f.st.GetPermissionProfile(context.Background(), data.Data.ID)
	must(t, e)
	if current.CanSend {
		t.Fatal("R5_BASELINE_DEFECT_A02: stale profile write restored revoked capability")
	}
}
func TestR5AuditA03ExpiredContentDeniedAcrossEntrypoints(t *testing.T) {
	f := seedCompany(t)
	s := r5AuditServer(t, f)
	j := archJob(t, f, models.OutboundSent)
	_, e := f.pool.Exec(context.Background(), `UPDATE sent_mail_items SET expires_at=clock_timestamp()-interval '1 second' WHERE asset_id=$1`, j.ID)
	must(t, e)
	token := r3Token(t, f.employee)
	status, _ := r5AuditHTTP(t, s, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil)
	r5ExpectStatus(t, status, 404)
	status, b := r5AuditHTTP(t, s, token, "GET", "/api/v1/outbound/"+j.ID.String(), nil)
	if status == 403 || status == 404 {
		return
	}
	r5ExpectStatus(t, status, 200)
	var data struct {
		Data models.OutboundJob `json:"data"`
	}
	must(t, json.Unmarshal(b, &data))
	if data.Data.TextBody != "" || data.Data.HTMLBody != "" {
		t.Fatal("R5_BASELINE_DEFECT_A03: legacy receipt returned expired body")
	}
}
func TestR5AuditA04FrozenEmployeeCanBeHandedOver(t *testing.T) {
	f := seedCompany(t)
	s := r5AuditServer(t, f)
	oldToken := r3Token(t, f.employee)
	admin := r3Token(t, f.admin)
	status, _ := r5AuditHTTP(t, s, admin, "PATCH", "/api/v1/admin/users/"+f.employee.ID.String(), map[string]any{"is_active": false})
	r5ExpectStatus(t, status, 200)
	status, _ = r5AuditHTTP(t, s, oldToken, "GET", "/api/v1/company/mailboxes", nil)
	if status != 401 && status != 403 {
		t.Fatal("fixture did not revoke frozen user")
	}
	status, b := r5AuditHTTP(t, s, admin, "POST", "/api/v1/company/employees/"+f.employee.ID.String()+"/offboard/preview", map[string]any{"successor_user_id": f.other.ID, "options": map[string]string{"drafts": "seal"}, "reason": "R5 isolated offboarding audit"})
	if status == 409 && strings.Contains(string(b), "already inactive") {
		t.Fatal("R5_BASELINE_DEFECT_A04: frozen target cannot enter handover")
	}
	r5ExpectStatus(t, status, 200)
}
func TestR5AuditA05RestorePreservesHardExpiry(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	s := r5AuditServer(t, f)
	_, e := f.pool.Exec(ctx, `INSERT INTO mailbox_grants(tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only) VALUES($1,$2,$3,true,true,false,false)`, f.tenant.ID, f.shared.ID, f.employee.ID)
	must(t, e)
	expiry := time.Now().UTC().Add(time.Hour)
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "audit@sender.test", Recipients: []string{f.shared.FullAddress}, Subject: "finite audit", RawObjectKey: "r5-audit-not-opened", ExpiresAt: &expiry}
	must(t, f.st.CreateMessage(ctx, m))
	var original time.Time
	must(t, f.pool.QueryRow(ctx, `SELECT expires_at FROM messages WHERE id=$1`, m.ID).Scan(&original))
	p := "/api/v1/company/mailboxes/" + f.shared.ID.String() + "/messages/" + m.ID.String() + "/actions"
	token := r3Token(t, f.employee)
	for _, action := range []string{"trash", "restore"} {
		status, _ := r5AuditHTTP(t, s, token, "POST", p, map[string]string{"action": action})
		r5ExpectStatus(t, status, 200)
	}
	var after *time.Time
	must(t, f.pool.QueryRow(ctx, `SELECT expires_at FROM messages WHERE id=$1`, m.ID).Scan(&after))
	if after == nil || !after.Equal(original) {
		t.Fatal("R5_BASELINE_DEFECT_A05: restore removed or extended hard expiry")
	}
}
func TestR5AuditA06ProtectedPrefixDoesNotStarveGC(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	ids := []uuid.UUID{}
	// Ten normal-sized drafts pin 100 attachments; the 101st is a true orphan.
	// Setup uses SQL only for deterministic IDs and time, not for sweep behavior.
	for i := 1; i <= 101; i++ {
		id := uuid.MustParse(fmt.Sprintf("10000000-0000-0000-0000-%012x", i))
		ids = append(ids, id)
		_, e := f.pool.Exec(ctx, `INSERT INTO mail_attachments(id,tenant_id,mailbox_id,user_id,object_key,filename,content_type,size,sha256,state) VALUES($1,$2,$3,$4,$5,'audit.txt','text/plain',1,$6,'ready')`, id, f.tenant.ID, f.personal.ID, f.employee.ID, "r5-audit-"+id.String(), company.Hash("x"))
		must(t, e)
	}
	for i := 0; i < 10; i++ {
		_, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{AttachmentIDs: ids[i*10 : (i+1)*10]}})
		must(t, e)
	}
	_, e := f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=$1`, f.tenant.ID)
	must(t, e)
	for i := 0; i < 3; i++ {
		must(t, f.st.SweepCompanyMetadata(ctx))
	}
	var protected, orphan int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE id<>$2),count(*) FILTER (WHERE id=$2) FROM mail_attachments WHERE tenant_id=$1`, f.tenant.ID, ids[100]).Scan(&protected, &orphan))
	if protected != 100 {
		t.Fatal("unexpected loss of referenced attachments")
	}
	if orphan != 0 {
		t.Fatal("R5_BASELINE_DEFECT_A06: protected prefix starves orphan 101 across sweeps")
	}
}
func TestR5AuditA07SentAssetKeepsBCCAfterQueueCleanup(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	bcc := "hidden@recipient.test"
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"visible@recipient.test"}, BCC: []string{bcc}, RcptTo: []string{"visible@recipient.test", bcc}, Subject: "R5 BCC audit", TextBody: "synthetic body", State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(ctx, j))
	before, e := f.st.GetOutboundJob(ctx, j.ID)
	must(t, e)
	if len(before.BCC) != 1 {
		t.Fatal("fixture BCC missing before cleanup")
	}
	_, e = f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
	must(t, e)
	var jobs int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE id=$1`, j.ID).Scan(&jobs))
	if jobs != 0 {
		t.Fatal("queue cleanup failed")
	}
	c, e := f.st.GetSubmissionContent(ctx, f.u, j.ID)
	must(t, e)
	if c.TextBody != "synthetic body" {
		t.Fatal("unrelated asset loss")
	}
	var raw []byte
	must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&raw))
	if !strings.Contains(string(raw), bcc) {
		t.Fatal("R5_BASELINE_DEFECT_A07: durable sent asset lost BCC after queue cleanup")
	}
}
