package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// mustExecPool runs a raw policy-setting statement against the fixture pool.
func mustExecPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	_, e := pool.Exec(ctx, sql, args...)
	must(t, e)
}

func sendPolicyPtr(s string) *string { return &s }

// configureSendPolicy flips the tenant default through the real
// ConfigureCompany path, honoring the settings revision CAS each time.
func configureSendPolicy(t *testing.T, f *companyFixture, policy string) *company.Settings {
	t.Helper()
	ctx := context.Background()
	cur, e := f.st.GetCompanySettings(ctx, f.tenant.ID)
	must(t, e)
	s, e := f.st.ConfigureCompany(ctx, f.a, company.Settings{Name: cur.Name, PrimaryZoneID: cur.PrimaryZoneID, Revision: cur.Revision, MailSendPolicy: policy})
	must(t, e)
	if s.MailSendPolicy != policy {
		t.Fatalf("configured mail_send_policy = %q, want %q", s.MailSendPolicy, policy)
	}
	return s
}

func wantAppKind(t *testing.T, err error, kind app.ErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("wanted %v error, got nil", kind)
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != kind {
		t.Fatalf("wanted %v error, got %T: %v", kind, err, err)
	}
}

// newPolicyService wires an outbound service against the real store; the relay
// host is unreachable on purpose — these tests never run delivery, only
// enqueue-time and per-attempt authorization.
func newPolicyService(t *testing.T, f *companyFixture) *outbound.Service {
	t.Helper()
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1, RelayTLS: "none", MaxRetries: 3}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj)
	return svc
}

// TestSendPolicyDefaultsAndEffectiveResolution pins the migration contract
// (tenant default 'free', mailbox override NULL) and that the store delivers
// the COALESCE'd effective policy on every mailbox row.
func TestSendPolicyDefaultsAndEffectiveResolution(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()

	var tenantPolicy string
	must(t, f.pool.QueryRow(ctx, `SELECT mail_send_policy FROM tenants WHERE id=$1`, f.tenant.ID).Scan(&tenantPolicy))
	if tenantPolicy != "free" {
		t.Fatalf("tenant default after migration = %q, want free", tenantPolicy)
	}
	var noOverride bool
	must(t, f.pool.QueryRow(ctx, `SELECT send_policy IS NULL FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&noOverride))
	if !noOverride {
		t.Fatal("mailbox send_policy must default to NULL (inherit tenant)")
	}

	mb, e := f.st.GetMailboxByAddress(ctx, f.personal.FullAddress)
	must(t, e)
	if mb.SendPolicy == nil || *mb.SendPolicy != "free" {
		t.Fatalf("effective policy on mailbox row = %v, want free", mb.SendPolicy)
	}

	// A mailbox-level override wins over the tenant default in the same read.
	mustExecPool(t, ctx, f.pool, `UPDATE mailboxes SET send_policy='template_required' WHERE id=$1`, f.personal.ID)
	mustExecPool(t, ctx, f.pool, `UPDATE tenants SET mail_send_policy='disabled' WHERE id=$1`, f.tenant.ID)
	mb, e = f.st.GetMailboxByAddress(ctx, f.personal.FullAddress)
	must(t, e)
	if mb.SendPolicy == nil || *mb.SendPolicy != "template_required" {
		t.Fatalf("mailbox override must beat tenant default, got %v", mb.SendPolicy)
	}
}

// TestSendPolicyGatesEnqueuedJobAtNextAttempt is the worker-inheritance
// integration proof: a free-form job enqueued under the 'free' default is
// refused at its next delivery attempt once the company flips the tenant
// policy — ValidateJobAuthorization re-runs the shared decision tree and
// maps the refusal onto the MailboxSenderErr channel (WorkerFailure priority
// unchanged, no worker-side code touched).
func TestSendPolicyGatesEnqueuedJobAtNextAttempt(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	svc := newPolicyService(t, f)

	job, e := svc.Submit(ctx, outbound.SendRequest{
		TenantID:        f.tenant.ID,
		SenderMailboxID: &f.personal.ID,
		UserID:          &f.employee.ID,
		ZoneID:          f.zone.ID,
		From:            f.personal.FullAddress,
		To:              []string{"client@recipient.test"},
		Subject:         "free under default policy",
		TextBody:        "body",
		IdempotencyKey:  "send-policy-owner-1",
	})
	must(t, e)

	// Company tightens the tenant policy after enqueue.
	mustExecPool(t, ctx, f.pool, `UPDATE tenants SET mail_send_policy='template_required' WHERE id=$1`, f.tenant.ID)
	err := svc.ValidateJobAuthorization(ctx, job)
	if err == nil || err.Error() != "a granted published template version is required" {
		t.Fatalf("next attempt after template_required flip = %v, want template refusal", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("policy refusal must be an authz error, got %T: %v", err, err)
	}

	// The mailbox-level override exempts this one mailbox again.
	mustExecPool(t, ctx, f.pool, `UPDATE mailboxes SET send_policy='free' WHERE id=$1`, f.personal.ID)
	if err = svc.ValidateJobAuthorization(ctx, job); err != nil {
		t.Fatalf("mailbox override must win over the tenant default: %v", err)
	}

	// Company shuts mailbox sends down entirely; owner included.
	mustExecPool(t, ctx, f.pool, `UPDATE mailboxes SET send_policy=NULL WHERE id=$1`, f.personal.ID)
	mustExecPool(t, ctx, f.pool, `UPDATE tenants SET mail_send_policy='disabled' WHERE id=$1`, f.tenant.ID)
	err = svc.ValidateJobAuthorization(ctx, job)
	if err == nil || err.Error() != "mailbox sending disabled by company policy" {
		t.Fatalf("next attempt after disabled flip = %v, want policy refusal", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("policy refusal must be an authz error, got %T: %v", err, err)
	}
}

// TestSendPolicyBindsAdminGrant proves the policy binds an administrator's
// granted send right too: the grant works under the 'free' default and is
// refused outright once the tenant flips to 'disabled'.
func TestSendPolicyBindsAdminGrant(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	svc := newPolicyService(t, f)
	must(t, f.st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.admin.ID, CanSend: true}))

	submit := func(key string) error {
		_, err := svc.Submit(ctx, outbound.SendRequest{
			TenantID:        f.tenant.ID,
			SenderMailboxID: &f.shared.ID,
			UserID:          &f.admin.ID,
			ZoneID:          f.zone.ID,
			From:            f.shared.FullAddress,
			To:              []string{"client@recipient.test"},
			Subject:         "admin grant send",
			TextBody:        "body",
			IdempotencyKey:  key,
		})
		return err
	}
	if err := submit("send-policy-admin-free"); err != nil {
		t.Fatalf("free policy must let the admin grant send: %v", err)
	}

	mustExecPool(t, ctx, f.pool, `UPDATE tenants SET mail_send_policy='disabled' WHERE id=$1`, f.tenant.ID)
	err := submit("send-policy-admin-disabled")
	if err == nil || err.Error() != "mailbox sending disabled by company policy" {
		t.Fatalf("admin grant under disabled policy = %v, want policy refusal", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("policy refusal must be an authz error, got %T: %v", err, err)
	}
}

// TestSendPolicyAdminCompanyDefaultLifecycle drives the company-default policy
// through the real ConfigureCompany path and proves the mailbox-owner send
// authorization follows it: free allows the free-form submit, flipping to
// template_required refuses it at the next authorization, disabled refuses
// outright — and every change lands in the audit log.
func TestSendPolicyAdminCompanyDefaultLifecycle(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	svc := newPolicyService(t, f)

	s, e := f.st.GetCompanySettings(ctx, f.tenant.ID)
	must(t, e)
	if s.MailSendPolicy != "free" {
		t.Fatalf("initial settings mail_send_policy = %q, want free", s.MailSendPolicy)
	}
	job, e := svc.Submit(ctx, outbound.SendRequest{
		TenantID:        f.tenant.ID,
		SenderMailboxID: &f.personal.ID,
		UserID:          &f.employee.ID,
		ZoneID:          f.zone.ID,
		From:            f.personal.FullAddress,
		To:              []string{"client@recipient.test"},
		Subject:         "free under default company policy",
		TextBody:        "body",
		IdempotencyKey:  "send-policy-admin-default-1",
	})
	must(t, e)

	configureSendPolicy(t, f, "template_required")
	err := svc.ValidateJobAuthorization(ctx, job)
	if err == nil || err.Error() != "a granted published template version is required" {
		t.Fatalf("owner send after template_required = %v, want template refusal", err)
	}
	configureSendPolicy(t, f, "disabled")
	err = svc.ValidateJobAuthorization(ctx, job)
	if err == nil || err.Error() != "mailbox sending disabled by company policy" {
		t.Fatalf("owner send after disabled = %v, want policy refusal", err)
	}

	var audits int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='company.configure' AND resource_id=$1 AND details->>'mail_send_policy'='disabled'`, f.tenant.ID).Scan(&audits))
	if audits != 1 {
		t.Fatalf("company.configure audit rows with disabled policy = %d, want 1", audits)
	}
}

// TestSendPolicyMailboxOverrideAdminLifecycle proves the administrative
// override API: the override beats the company default, clearing it inherits
// again, and the effective value reaches MailboxAccess views.
func TestSendPolicyMailboxOverrideAdminLifecycle(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()

	configureSendPolicy(t, f, "template_required")
	effective := func() string {
		mb, e := f.st.GetMailboxByAddress(ctx, f.personal.FullAddress)
		must(t, e)
		if mb.SendPolicy == nil {
			t.Fatal("effective send policy missing on mailbox row")
		}
		return *mb.SendPolicy
	}
	if got := effective(); got != "template_required" {
		t.Fatalf("effective policy before override = %q, want template_required", got)
	}

	must(t, policyCurrent(f.st, ctx, f.a, f.personal.ID, sendPolicyPtr("free")))
	if got := effective(); got != "free" {
		t.Fatalf("override must beat company default, got %q", got)
	}

	// nil clears the override; the mailbox inherits the tenant default again.
	must(t, policyCurrent(f.st, ctx, f.a, f.personal.ID, nil))
	if got := effective(); got != "template_required" {
		t.Fatalf("cleared override must inherit the company default, got %q", got)
	}

	access, e := f.st.ListWorkMailboxes(ctx, f.a)
	must(t, e)
	for _, v := range access {
		if v.Mailbox.ID == f.personal.ID && (v.Mailbox.SendPolicy == nil || *v.Mailbox.SendPolicy != "template_required") {
			t.Fatalf("MailboxAccess must carry the effective policy, got %v", v.Mailbox.SendPolicy)
		}
	}

	var cleared int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='mailbox.send_policy' AND resource_id=$1 AND (details->>'send_policy')=''`, f.personal.ID).Scan(&cleared))
	if cleared != 1 {
		t.Fatalf("clear-override audit rows = %d, want 1", cleared)
	}
	var overrides int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='mailbox.send_policy' AND resource_id=$1 AND details->>'send_policy'='free'`, f.personal.ID).Scan(&overrides))
	if overrides != 1 {
		t.Fatalf("override audit rows = %d, want 1", overrides)
	}
}

// TestSendPolicyAdminValidationAndGuards pins the admin-only and validation
// boundaries of both new configuration paths.
func TestSendPolicyAdminValidationAndGuards(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()

	// Non-admin cannot configure the company default.
	_, e := f.st.ConfigureCompany(ctx, f.u, company.Settings{Name: "Example Company", PrimaryZoneID: f.zone.ID, MailSendPolicy: "free"})
	wantAppKind(t, e, app.KindForbidden)
	// Invalid company default is a 400 naming the three legal values.
	_, e = f.st.ConfigureCompany(ctx, f.a, company.Settings{Name: "Example Company", PrimaryZoneID: f.zone.ID, MailSendPolicy: "relaxed"})
	wantAppKind(t, e, app.KindBadRequest)
	if !strings.Contains(e.Error(), "free") || !strings.Contains(e.Error(), "template_required") || !strings.Contains(e.Error(), "disabled") {
		t.Fatalf("invalid policy error must name all three values, got: %v", e)
	}
	// Non-admin cannot set a mailbox override.
	e = policyCurrent(f.st, ctx, f.u, f.personal.ID, sendPolicyPtr("free"))
	wantAppKind(t, e, app.KindForbidden)
	// Invalid override is a 400; unknown mailbox is a 404.
	e = policyCurrent(f.st, ctx, f.a, f.personal.ID, sendPolicyPtr("relaxed"))
	wantAppKind(t, e, app.KindBadRequest)
	e = policyCurrent(f.st, ctx, f.a, uuid.New(), sendPolicyPtr("free"))
	wantAppKind(t, e, app.KindNotFound)
	// An invalid default must not have leaked into storage.
	s, e := f.st.GetCompanySettings(ctx, f.tenant.ID)
	must(t, e)
	if s.MailSendPolicy != "free" {
		t.Fatalf("rejected policy change must not persist, got %q", s.MailSendPolicy)
	}
}

// TestSendPolicyAdminHTTPJourney covers the route wiring: the RequireAdmin
// middleware plus in-transaction admin check both refuse an employee, an
// invalid body is a 400, and the mailbox views carry the effective policy.
func TestSendPolicyAdminHTTPJourney(t *testing.T) {
	f := seedCompany(t)
	obj := testutil.NewMemoryObjectStore()
	svc := newPolicyService(t, f)
	h := companyRouter(t, f, obj, svc)
	admin, employee := r3Token(t, f.admin), r3Token(t, f.employee)
	policyPath := "/api/v1/company/mailboxes/" + f.personal.ID.String() + "/send-policy"

	r3HTTP(t, h, employee, "PUT", policyPath, map[string]any{"send_policy": "free"}, 403)
	r3HTTP(t, h, admin, "PUT", policyPath, map[string]any{"send_policy": "relaxed"}, 400)
	r3HTTP(t, h, admin, "PUT", policyPath, map[string]any{"send_policy": "template_required", "revision": 1}, 200)

	effective := func() string {
		boxes := r3Data[[]company.MailboxAccess](t, r3HTTP(t, h, admin, "GET", "/api/v1/company/mailboxes", nil, 200))
		for _, v := range boxes {
			if v.Mailbox.ID == f.personal.ID {
				if v.Mailbox.SendPolicy == nil {
					t.Fatal("mailbox view missing effective send policy")
				}
				return *v.Mailbox.SendPolicy
			}
		}
		t.Fatal("personal mailbox absent from admin listing")
		return ""
	}
	if got := effective(); got != "template_required" {
		t.Fatalf("effective policy after override = %q, want template_required", got)
	}

	// null clears the override back to the company default.
	r3HTTP(t, h, admin, "PUT", policyPath, map[string]any{"send_policy": nil, "revision": 2}, 200)
	if got := effective(); got != "free" {
		t.Fatalf("effective policy after clearing override = %q, want free", got)
	}

	// Company default through the settings endpoint: invalid 400, valid flips.
	cur := r3Data[company.Settings](t, r3HTTP(t, h, admin, "GET", "/api/v1/company/settings", nil, 200))
	body := map[string]any{"name": cur.Name, "primary_zone_id": cur.PrimaryZoneID, "revision": cur.Revision, "mail_send_policy": "relaxed"}
	r3HTTP(t, h, admin, "PUT", "/api/v1/company/settings", body, 400)
	body["mail_send_policy"] = "disabled"
	got := r3Data[company.Settings](t, r3HTTP(t, h, admin, "PUT", "/api/v1/company/settings", body, 200))
	if got.MailSendPolicy != "disabled" {
		t.Fatalf("settings PUT mail_send_policy = %q, want disabled", got.MailSendPolicy)
	}
}
