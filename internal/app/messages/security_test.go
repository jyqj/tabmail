package messageapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestProtectedMailboxIsHiddenAcrossTenantBoundaries(t *testing.T) {
	ctx := context.Background()
	st, svc, targetTenant, mailbox := newMessageIsolationFixture(t, models.AccessToken)
	otherTenant := &models.Tenant{ID: uuid.New(), Name: "other"}
	st.SeedTenant(otherTenant)

	viewer := Viewer{Tenant: otherTenant, AuthMode: AuthModeUser, IsAdmin: true}
	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	requireMessageSecurityKind(t, err, app.KindNotFound)
	_, err = svc.ResolveMailboxForWrite(ctx, mailbox.FullAddress, viewer)
	requireMessageSecurityKind(t, err, app.KindNotFound)

	viewer.Tenant = targetTenant
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("selected-tenant admin should resolve mailbox metadata: %v", err)
	}
	if svc.canReadMessageContent(mailbox, viewer) {
		t.Fatal("selected-tenant admin content access must use audited break-glass")
	}
}

func TestPlatformAdminMustSelectTargetTenant(t *testing.T) {
	ctx := context.Background()
	st, svc, targetTenant, mailbox := newMessageIsolationFixture(t, models.AccessAPIKey)
	homeTenant := &models.Tenant{ID: uuid.New(), Name: "platform-home"}
	st.SeedTenant(homeTenant)

	viewer := Viewer{Tenant: homeTenant, AuthMode: AuthModeUser, IsAdmin: true, IsSuperAdmin: true}
	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	requireMessageSecurityKind(t, err, app.KindNotFound)

	viewer.Tenant = targetTenant
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("platform admin should resolve after explicit tenant selection: %v", err)
	}
}

func TestMailboxCapabilityStillResolvesItsTarget(t *testing.T) {
	ctx := context.Background()
	st, svc, _, mailbox := newMessageIsolationFixture(t, models.AccessToken)
	publicContext := &models.Tenant{ID: uuid.New(), Name: "public-context"}
	st.SeedTenant(publicContext)
	token, err := mailtoken.Issue("mailbox-secret", mailbox.ID.String(), mailbox.FullAddress, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewer := Viewer{Tenant: publicContext, AuthMode: AuthModePublic, BearerToken: token}
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("mailbox token should resolve its protected mailbox: %v", err)
	}
	viewer.BearerToken = "invalid"
	_, err = svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	requireMessageSecurityKind(t, err, app.KindNotFound)
}

func TestForeignJWTIsNotTreatedAsMailboxCapability(t *testing.T) {
	ctx := context.Background()
	st, svc, _, mailbox := newMessageIsolationFixture(t, models.AccessToken)
	otherTenant := &models.Tenant{ID: uuid.New(), Name: "other"}
	st.SeedTenant(otherTenant)
	viewer := Viewer{
		Tenant:      otherTenant,
		AuthMode:    AuthModeUser,
		BearerToken: "a-jwt-is-not-a-mailbox-capability",
	}
	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	requireMessageSecurityKind(t, err, app.KindNotFound)
}

func TestCrossTenantPublicMailboxDoesNotInheritAdminBreakGlass(t *testing.T) {
	ctx := context.Background()
	st, svc, _, mailbox := newMessageIsolationFixture(t, models.AccessPublic)
	otherTenant := &models.Tenant{ID: uuid.New(), Name: "other"}
	st.SeedTenant(otherTenant)
	viewer := Viewer{Tenant: otherTenant, AuthMode: AuthModeUser, IsAdmin: true}
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("public mailbox should remain public: %v", err)
	}
	if !svc.canReadMessageContent(mailbox, viewer) {
		t.Fatal("unrelated admin role must not redact public mailbox content")
	}
}

func newMessageIsolationFixture(t *testing.T, mode models.AccessMode) (*testutil.FakeStore, *Service, *models.Tenant, *models.Mailbox) {
	t.Helper()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	st.SeedTenant(tenant)
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: uuid.NewString() + ".test", IsVerified: true, MXVerified: true}
	st.SeedZone(zone)
	mailbox := &models.Mailbox{ID: uuid.New(), TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "inbox", ResolvedDomain: zone.Domain, FullAddress: "inbox@" + zone.Domain, AccessMode: mode, CreatedAt: time.Now()}
	st.SeedMailbox(mailbox)
	svc := NewService(st, testutil.NewMemoryObjectStore(), nil, nil, policy.NamingFull, true, "mailbox-secret", zerolog.Nop())
	return st, svc, tenant, mailbox
}

func requireMessageSecurityKind(t *testing.T, err error, want app.ErrorKind) {
	t.Helper()
	appErr, ok := app.As(err)
	if !ok || appErr.Kind != want {
		t.Fatalf("expected %s app error, got %T %v", want, err, err)
	}
}
