package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// ---------------------------------------------------------------------------
// Mock store
// ---------------------------------------------------------------------------

type mockStore struct {
	zoneRoles     map[string]models.ZoneGrantRole // key: "zoneID:principalType:principalID"
	mailboxGrants map[string]*models.MailboxGrant  // key: "mailboxID:principalType:principalID"
	sendAsGrants  map[string]bool                  // key: "tenantID:address:principalType:principalID"
}

func newMockStore() *mockStore {
	return &mockStore{
		zoneRoles:     make(map[string]models.ZoneGrantRole),
		mailboxGrants: make(map[string]*models.MailboxGrant),
		sendAsGrants:  make(map[string]bool),
	}
}

func grantKey(a, b, c uuid.UUID, pt string) string {
	return a.String() + ":" + pt + ":" + c.String()
}

func mbGrantKey(mailboxID uuid.UUID, pt string, pid uuid.UUID) string {
	return mailboxID.String() + ":" + pt + ":" + pid.String()
}

func (m *mockStore) GetHighestZoneRole(_ context.Context, zoneID uuid.UUID, principalType string, principalID uuid.UUID) (models.ZoneGrantRole, error) {
	key := grantKey(zoneID, uuid.Nil, principalID, principalType)
	return m.zoneRoles[key], nil
}

func (m *mockStore) GetMailboxGrant(_ context.Context, mailboxID uuid.UUID, principalType string, principalID uuid.UUID) (*models.MailboxGrant, error) {
	key := mbGrantKey(mailboxID, principalType, principalID)
	return m.mailboxGrants[key], nil
}

func (m *mockStore) HasSendAsGrant(_ context.Context, tenantID uuid.UUID, address string, principalType string, principalID uuid.UUID) (bool, error) {
	key := tenantID.String() + ":" + address + ":" + principalType + ":" + principalID.String()
	return m.sendAsGrants[key], nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	tenantA = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	tenantB = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	userID  = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	zoneID  = uuid.MustParse("30000000-0000-0000-0000-000000000001")
	mboxID  = uuid.MustParse("40000000-0000-0000-0000-000000000001")
	keyID   = uuid.MustParse("50000000-0000-0000-0000-000000000001")
)

func platformAdmin() Actor {
	return Actor{
		Type:            PrincipalUser,
		ID:              userID,
		TenantID:        tenantA,
		Role:            models.RolePlatformAdmin,
		IsPlatformAdmin: true,
	}
}

func tenantAdmin() Actor {
	return Actor{
		Type:          PrincipalUser,
		ID:            userID,
		TenantID:      tenantA,
		Role:          models.RoleTenantAdmin,
		IsTenantAdmin: true,
	}
}

func regularUser(perm *models.EffectivePermission) Actor {
	return Actor{
		Type:       PrincipalUser,
		ID:         userID,
		TenantID:   tenantA,
		Role:       models.RoleUser,
		Permission: perm,
	}
}

func apiKeyActor() Actor {
	return Actor{
		Type:       PrincipalAPIKey,
		ID:         keyID,
		TenantID:   tenantA,
		TenantWide: true,
	}
}

func zoneResource() Resource {
	return Resource{
		Type:     "zone",
		ID:       zoneID,
		TenantID: tenantA,
		ZoneID:   zoneID,
	}
}

func mailboxResource() Resource {
	return Resource{
		Type:     "mailbox",
		ID:       mboxID,
		TenantID: tenantA,
		ZoneID:   zoneID,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPlatformAdminCanDoAnything(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := platformAdmin()
	res := zoneResource()

	actions := []Action{
		ActionTenantManage, ActionTenantUsersManage,
		ActionZoneRead, ActionZoneManage, ActionZoneCreate, ActionZoneDelete,
		ActionRouteRead, ActionRouteManage,
		ActionMailboxRead, ActionMailboxWrite, ActionMailboxCreate, ActionMailboxDelete,
		ActionMessageRead, ActionMessageWrite, ActionMessageDelete,
		ActionSendFrom, ActionOutboundRead,
		ActionAPIKeyCreate, ActionAPIKeyManage,
	}

	for _, action := range actions {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("platform admin should be allowed %s, got: %v", action, err)
		}
	}
}

func TestTenantAdminCannotManageTenants(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := tenantAdmin()
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionTenantManage, res); err == nil {
		t.Fatal("tenant admin should NOT be able to ActionTenantManage")
	}
}

func TestTenantAdminCanManageZones(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := tenantAdmin()
	res := zoneResource()

	for _, action := range []Action{ActionZoneRead, ActionZoneManage, ActionZoneCreate, ActionZoneDelete} {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("tenant admin should be allowed %s, got: %v", action, err)
		}
	}
}

func TestTenantIsolation(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := tenantAdmin()
	res := Resource{TenantID: tenantB, ZoneID: zoneID} // different tenant

	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err == nil {
		t.Fatal("should be denied access to another tenant's resource")
	}
}

func TestRegularUserNoGrant(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	perm := &models.EffectivePermission{
		CanSend:          true,
		CanCreateDomains: true,
		CanCreateRoutes:  true,
		CanCreateAPIKeys: true,
	}
	actor := regularUser(perm)
	res := zoneResource()

	// No zone grant — should fail
	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err == nil {
		t.Fatal("user without zone grant should be denied ActionZoneRead")
	}
	if err := az.Authorize(ctx, actor, ActionZoneManage, res); err == nil {
		t.Fatal("user without zone grant should be denied ActionZoneManage")
	}
}

func TestRegularUserWithViewerGrant(t *testing.T) {
	st := newMockStore()
	st.zoneRoles[grantKey(zoneID, uuid.Nil, userID, "user")] = models.ZoneRoleViewer

	az := New(st)
	ctx := context.Background()
	actor := regularUser(nil)
	res := zoneResource()

	// Viewer can read
	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err != nil {
		t.Fatalf("viewer should be allowed ActionZoneRead, got: %v", err)
	}

	// Viewer cannot manage
	if err := az.Authorize(ctx, actor, ActionZoneManage, res); err == nil {
		t.Fatal("viewer should NOT be allowed ActionZoneManage")
	}
}

func TestRegularUserWithAdminGrant(t *testing.T) {
	st := newMockStore()
	st.zoneRoles[grantKey(zoneID, uuid.Nil, userID, "user")] = models.ZoneRoleAdmin

	az := New(st)
	ctx := context.Background()
	actor := regularUser(nil)
	res := zoneResource()

	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err != nil {
		t.Fatalf("zone admin should be allowed ActionZoneRead, got: %v", err)
	}
	if err := az.Authorize(ctx, actor, ActionZoneManage, res); err != nil {
		t.Fatalf("zone admin should be allowed ActionZoneManage, got: %v", err)
	}
}

func TestAPIKeyTenantWideBypass(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := apiKeyActor()
	res := zoneResource()

	for _, action := range []Action{ActionZoneRead, ActionZoneManage, ActionMailboxRead, ActionMailboxWrite} {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("API key (tenant-wide) should be allowed %s, got: %v", action, err)
		}
	}
}

func TestAPIKeyDeniedAdminActions(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := apiKeyActor()
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionTenantManage, res); err == nil {
		t.Fatal("API key should NOT be allowed ActionTenantManage")
	}
	if err := az.Authorize(ctx, actor, ActionTenantUsersManage, res); err == nil {
		t.Fatal("API key should NOT be allowed ActionTenantUsersManage")
	}
}

func TestPermissionCanSendDenied(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	perm := &models.EffectivePermission{CanSend: false}
	actor := regularUser(perm)
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionSendFrom, res); err == nil {
		t.Fatal("user with CanSend=false should be denied ActionSendFrom")
	}
}

func TestPermissionCanCreateDomainsDenied(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	perm := &models.EffectivePermission{CanCreateDomains: false}
	actor := regularUser(perm)
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionZoneCreate, res); err == nil {
		t.Fatal("user with CanCreateDomains=false should be denied ActionZoneCreate")
	}
}

func TestPermissionCanCreateAPIKeysDenied(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	perm := &models.EffectivePermission{CanCreateAPIKeys: false}
	actor := regularUser(perm)
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionAPIKeyCreate, res); err == nil {
		t.Fatal("user with CanCreateAPIKeys=false should be denied ActionAPIKeyCreate")
	}
}

func TestMailboxGrantReader(t *testing.T) {
	st := newMockStore()
	st.mailboxGrants[mbGrantKey(mboxID, "user", userID)] = &models.MailboxGrant{
		Role: models.MailboxRoleReader,
	}

	az := New(st)
	ctx := context.Background()
	actor := regularUser(nil)
	res := mailboxResource()

	// Reader can read
	if err := az.Authorize(ctx, actor, ActionMailboxRead, res); err != nil {
		t.Fatalf("mailbox reader should be allowed ActionMailboxRead, got: %v", err)
	}

	// Reader cannot write
	if err := az.Authorize(ctx, actor, ActionMailboxWrite, res); err == nil {
		t.Fatal("mailbox reader should NOT be allowed ActionMailboxWrite")
	}
}

func TestMailboxGrantWriter(t *testing.T) {
	st := newMockStore()
	st.mailboxGrants[mbGrantKey(mboxID, "user", userID)] = &models.MailboxGrant{
		Role: models.MailboxRoleWriter,
	}

	az := New(st)
	ctx := context.Background()
	actor := regularUser(nil)
	res := mailboxResource()

	if err := az.Authorize(ctx, actor, ActionMailboxRead, res); err != nil {
		t.Fatalf("mailbox writer should be allowed ActionMailboxRead, got: %v", err)
	}
	if err := az.Authorize(ctx, actor, ActionMailboxWrite, res); err != nil {
		t.Fatalf("mailbox writer should be allowed ActionMailboxWrite, got: %v", err)
	}
}

func TestMailboxFallbackToZoneGrant(t *testing.T) {
	st := newMockStore()
	// No mailbox grant, but zone admin grant exists
	st.zoneRoles[grantKey(zoneID, uuid.Nil, userID, "user")] = models.ZoneRoleAdmin

	az := New(st)
	ctx := context.Background()
	actor := regularUser(nil)
	res := mailboxResource()

	if err := az.Authorize(ctx, actor, ActionMailboxRead, res); err != nil {
		t.Fatalf("should fall back to zone grant for mailbox access, got: %v", err)
	}
}

func TestMailboxCreateZoneNotAllowed(t *testing.T) {
	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")
	st := newMockStore()
	st.zoneRoles[grantKey(zoneID, uuid.Nil, userID, "user")] = models.ZoneRoleAdmin

	az := New(st)
	ctx := context.Background()

	perm := &models.EffectivePermission{
		AllowedZoneIDs: []uuid.UUID{otherZone}, // only otherZone allowed
	}
	actor := regularUser(perm)
	res := Resource{
		Type:     "mailbox",
		TenantID: tenantA,
		ZoneID:   zoneID, // not in AllowedZoneIDs
	}

	if err := az.Authorize(ctx, actor, ActionMailboxCreate, res); err == nil {
		t.Fatal("should be denied when zone not in AllowedZoneIDs")
	}
}

func TestRouteManageNeedsPermission(t *testing.T) {
	st := newMockStore()
	st.zoneRoles[grantKey(zoneID, uuid.Nil, userID, "user")] = models.ZoneRoleAdmin

	az := New(st)
	ctx := context.Background()

	perm := &models.EffectivePermission{CanCreateRoutes: false}
	actor := regularUser(perm)
	res := zoneResource()

	if err := az.Authorize(ctx, actor, ActionRouteManage, res); err == nil {
		t.Fatal("should be denied route manage when CanCreateRoutes=false")
	}
}

func TestIsAuthzError(t *testing.T) {
	if IsAuthzError(nil) {
		t.Fatal("nil should not be authz error")
	}
	if !IsAuthzError(ErrForbidden("test")) {
		t.Fatal("ErrForbidden result should be authz error")
	}
}
