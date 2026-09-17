package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
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
