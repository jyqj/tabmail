package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// ---------------------------------------------------------------------------
// Mock store (empty after grant removal)
// ---------------------------------------------------------------------------

type mockStore struct{}

func newMockStore() *mockStore {
	return &mockStore{}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	tenantA     = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	tenantB     = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	userID      = uuid.MustParse("20000000-0000-0000-0000-000000000001")
	otherUserID = uuid.MustParse("20000000-0000-0000-0000-000000000002")
	zoneID      = uuid.MustParse("30000000-0000-0000-0000-000000000001")
	mboxID      = uuid.MustParse("40000000-0000-0000-0000-000000000001")
	keyID       = uuid.MustParse("50000000-0000-0000-0000-000000000001")
)

func superAdmin() Actor {
	return Actor{
		Type:         PrincipalUser,
		ID:           userID,
		TenantID:     tenantA,
		Role:         models.RoleSuperAdmin,
		IsSuperAdmin: true,
	}
}

func admin() Actor {
	return Actor{
		Type:     PrincipalUser,
		ID:       userID,
		TenantID: tenantA,
		Role:     models.RoleAdmin,
		IsAdmin:  true,
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

func ownedZoneResource(owner uuid.UUID) Resource {
	res := zoneResource()
	id := owner
	res.OwnerUserID = &id
	return res
}

func testZone(tenant uuid.UUID, owner *uuid.UUID) *models.DomainZone {
	return &models.DomainZone{ID: zoneID, TenantID: tenant, OwnerUserID: owner}
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

func TestSuperAdminCanDoAnything(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := superAdmin()
	res := zoneResource()

	actions := []Action{
		ActionTenantManage, ActionTenantUsersManage,
		ActionZoneRead, ActionZoneManage, ActionZoneCreate, ActionZoneDelete,
		ActionRouteRead, ActionRouteManage, ActionRouteDelete,
		ActionMailboxRead, ActionMailboxWrite, ActionMailboxCreate, ActionMailboxDelete,
		ActionMessageList, ActionMessageRead, ActionMessageSource, ActionMessageWrite, ActionMessageDelete,
		ActionSendFrom, ActionOutboundRead,
		ActionAPIKeyCreate, ActionAPIKeyManage,
	}

	for _, action := range actions {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("super admin should be allowed %s, got: %v", action, err)
		}
	}
}

func TestAdminCanDoMostThings(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := admin()
	res := zoneResource()

	allowed := []Action{
		ActionTenantManage,
		ActionZoneRead, ActionZoneManage, ActionZoneCreate, ActionZoneDelete,
		ActionRouteRead, ActionRouteManage, ActionRouteDelete,
		ActionMailboxRead, ActionMailboxWrite, ActionMailboxCreate, ActionMailboxDelete,
		ActionMessageList, ActionMessageRead, ActionMessageSource, ActionMessageWrite, ActionMessageDelete,
		ActionSendFrom, ActionOutboundRead,
		ActionAPIKeyCreate, ActionAPIKeyManage,
	}

	for _, action := range allowed {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("admin should be allowed %s, got: %v", action, err)
		}
	}
}

func TestAdminCannotManageUsers(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := admin()
	res := Resource{TenantID: tenantA}

	if err := az.Authorize(ctx, actor, ActionTenantUsersManage, res); err == nil {
		t.Fatal("admin should NOT be able to ActionTenantUsersManage")
	}
}

func TestAdminAllowedMessageList(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := admin()
	res := mailboxResource()

	if err := az.Authorize(ctx, actor, ActionMessageList, res); err != nil {
		t.Fatalf("admin should be allowed message.list, got: %v", err)
	}
}

func TestAdminCanManageZones(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := admin()
	res := zoneResource()

	for _, action := range []Action{ActionZoneRead, ActionZoneManage, ActionZoneCreate, ActionZoneDelete} {
		if err := az.Authorize(ctx, actor, action, res); err != nil {
			t.Errorf("admin should be allowed %s, got: %v", action, err)
		}
	}
}

func TestTenantIsolation(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := admin()
	res := Resource{TenantID: tenantB, ZoneID: zoneID} // different tenant

	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err == nil {
		t.Fatal("should be denied access to another tenant's resource")
	}
}

func TestDenialKinds(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	// A cross-tenant denial is classified as tenant isolation.
	err := az.Authorize(ctx, admin(), ActionZoneRead, Resource{TenantID: tenantB, ZoneID: zoneID})
	if KindOf(err) != KindTenantIsolation {
		t.Fatalf("tenant isolation denial: KindOf = %q, want %q", KindOf(err), KindTenantIsolation)
	}

	// Constructors carry their Kind; the default constructor is generic.
	if KindOf(forbidden(KindOwnership, "x")) != KindOwnership {
		t.Fatalf("classified constructor lost its Kind")
	}
	if KindOf(ErrForbidden("x")) != KindForbidden {
		t.Fatalf("default denial should be KindForbidden")
	}
}

func TestRegularUserZoneAccess(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	// User with no zone restrictions
	perm := &models.EffectivePermission{
		CanSend:          true,
		CanCreateDomains: true,
		CanCreateRoutes:  true,
		CanCreateAPIKeys: true,
	}
	actor := regularUser(perm)
	res := ownedZoneResource(userID) // ownership now enforced at the seam

	// User can access an owned zone (AllowedZoneIDs empty = all zones allowed)
	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err != nil {
		t.Fatalf("user with no zone restrictions should be allowed ActionZoneRead on owned zone, got: %v", err)
	}

	// ownership now enforced at the seam: same loaded zone without an owner
	// match is denied for a regular user.
	if err := az.Authorize(ctx, actor, ActionZoneRead, zoneResource()); err == nil {
		t.Fatal("user should be denied ActionZoneRead on a loaded zone they do not own")
	}
}

func TestRegularUserZoneRestricted(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")
	perm := &models.EffectivePermission{
		AllowedZoneIDs: []uuid.UUID{otherZone}, // only otherZone allowed
	}
	actor := regularUser(perm)
	res := zoneResource()

	if err := az.Authorize(ctx, actor, ActionZoneRead, res); err == nil {
		t.Fatal("user should be denied when zone not in AllowedZoneIDs")
	}
}

func TestAPIKeyTenantWideBypass(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	actor := apiKeyActor()
	res := zoneResource()

	for _, action := range []Action{ActionZoneRead, ActionZoneManage, ActionMailboxRead, ActionMailboxWrite, ActionMessageRead, ActionMessageSource, ActionMessageList} {
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

func TestMailboxCreateZoneNotAllowed(t *testing.T) {
	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")
	az := New(newMockStore())
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

	// ownership now enforced at the seam: mailbox create authorizes against the
	// loaded zone, so non-owners are denied while zone owners are allowed.
	err := az.Authorize(ctx, regularUser(nil), ActionMailboxCreate, ownedZoneResource(otherUserID))
	if err == nil || err.Error() != "not your domain" {
		t.Fatalf("expected ownership denial, got: %v", err)
	}
	if err := az.Authorize(ctx, regularUser(nil), ActionMailboxCreate, ownedZoneResource(userID)); err != nil {
		t.Fatalf("zone owner should be allowed mailbox create, got: %v", err)
	}
}

func TestRouteManageNeedsPermission(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	perm := &models.EffectivePermission{CanCreateRoutes: false}
	actor := regularUser(perm)
	res := zoneResource()

	if err := az.Authorize(ctx, actor, ActionRouteManage, res); err == nil {
		t.Fatal("should be denied route manage when CanCreateRoutes=false")
	}
}

func TestRouteDeleteIgnoresCanCreateRoutesFlag(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	// Owner with CanCreateRoutes=false may still delete routes in their zone.
	perm := &models.EffectivePermission{CanCreateRoutes: false}
	actor := regularUser(perm)
	if err := az.Authorize(ctx, actor, ActionRouteDelete, ownedZoneResource(userID)); err != nil {
		t.Fatalf("route delete should not require CanCreateRoutes, got: %v", err)
	}

	// Allowlist still applies.
	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")
	restricted := regularUser(&models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{otherZone}})
	err := az.Authorize(ctx, restricted, ActionRouteDelete, ownedZoneResource(userID))
	if err == nil || err.Error() != "zone not in allowed list" {
		t.Fatalf("expected allowlist denial, got: %v", err)
	}

	// Ownership still applies.
	err = az.Authorize(ctx, regularUser(nil), ActionRouteDelete, ownedZoneResource(otherUserID))
	if err == nil || err.Error() != "not your domain" {
		t.Fatalf("expected ownership denial, got: %v", err)
	}
}

func TestSendFromAppliesZoneAllowlistWithoutOwnership(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()
	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")

	// CanSend=true with zone in allowlist: allowed even without zone ownership.
	perm := &models.EffectivePermission{CanSend: true, AllowedZoneIDs: []uuid.UUID{zoneID}}
	actor := regularUser(perm)
	if err := az.Authorize(ctx, actor, ActionSendFrom, ownedZoneResource(otherUserID)); err != nil {
		t.Fatalf("send.from should not require zone ownership, got: %v", err)
	}

	// CanSend=true but zone outside allowlist: denied.
	restricted := regularUser(&models.EffectivePermission{CanSend: true, AllowedZoneIDs: []uuid.UUID{otherZone}})
	err := az.Authorize(ctx, restricted, ActionSendFrom, zoneResource())
	if err == nil || err.Error() != "zone not in allowed list" {
		t.Fatalf("expected allowlist denial, got: %v", err)
	}

	// CanSend=false still denied first.
	noSend := regularUser(&models.EffectivePermission{CanSend: false, AllowedZoneIDs: []uuid.UUID{zoneID}})
	err = az.Authorize(ctx, noSend, ActionSendFrom, zoneResource())
	if err == nil || err.Error() != "sending not allowed" {
		t.Fatalf("expected CanSend denial, got: %v", err)
	}

	// Tenant-wide key with zone limits: allowlist applies via checkZoneAccess.
	restrictedKey := apiKeyActor()
	restrictedKey.Permission = &models.EffectivePermission{CanSend: true, AllowedZoneIDs: []uuid.UUID{otherZone}}
	err = az.Authorize(ctx, restrictedKey, ActionSendFrom, zoneResource())
	if err == nil || err.Error() != "zone not in allowed list" {
		t.Fatalf("expected allowlist denial for restricted key, got: %v", err)
	}

	// No zone in the resource: flag check only.
	if err := az.Authorize(ctx, actor, ActionSendFrom, Resource{TenantID: tenantA}); err != nil {
		t.Fatalf("send.from without zone should pass flag-only check, got: %v", err)
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

// ---------------------------------------------------------------------------
// Ownership-aware Authorize decision table
// ---------------------------------------------------------------------------

func TestAuthorizeZoneOwnership(t *testing.T) {
	az := New(newMockStore())
	ctx := context.Background()

	crossTenantOwnedZone := ownedZoneResource(otherUserID)
	crossTenantOwnedZone.TenantID = tenantB

	userOwnedKey := Actor{
		Type:        PrincipalAPIKey,
		ID:          keyID,
		TenantID:    tenantA,
		OwnerUserID: &userID,
	}

	restrictedTenantWideKey := apiKeyActor()
	restrictedTenantWideKey.Permission = &models.EffectivePermission{
		AllowedZoneIDs: []uuid.UUID{uuid.MustParse("30000000-0000-0000-0000-000000000099")},
	}

	zeroTenantAdmin := admin()
	zeroTenantAdmin.TenantID = uuid.Nil

	cases := []struct {
		name    string
		actor   Actor
		action  Action
		res     Resource
		allow   bool
		errText string
	}{
		{
			name:   "super admin cross-tenant allow",
			actor:  superAdmin(),
			action: ActionZoneManage,
			res:    crossTenantOwnedZone,
			allow:  true,
		},
		{
			name:   "admin same-tenant allow without ownership",
			actor:  admin(),
			action: ActionZoneManage,
			res:    ownedZoneResource(otherUserID),
			allow:  true,
		},
		{
			name:    "admin cross-tenant deny",
			actor:   admin(),
			action:  ActionZoneManage,
			res:     crossTenantOwnedZone,
			allow:   false,
			errText: "access denied",
		},
		{
			name:    "admin with zero TenantID deny (non-super)",
			actor:   zeroTenantAdmin,
			action:  ActionZoneManage,
			res:     ownedZoneResource(userID),
			allow:   false,
			errText: "access denied",
		},
		{
			name:   "tenant-wide key allow without ownership",
			actor:  apiKeyActor(),
			action: ActionZoneManage,
			res:    ownedZoneResource(otherUserID),
			allow:  true,
		},
		{
			name:    "tenant-wide key deny outside allowlist",
			actor:   restrictedTenantWideKey,
			action:  ActionZoneManage,
			res:     ownedZoneResource(otherUserID),
			allow:   false,
			errText: "zone not in allowed list",
		},
		{
			name:   "user owner allow",
			actor:  regularUser(nil),
			action: ActionZoneManage,
			res:    ownedZoneResource(userID),
			allow:  true,
		},
		{
			// ownership now enforced at the seam
			name:    "user non-owner deny",
			actor:   regularUser(nil),
			action:  ActionZoneManage,
			res:     ownedZoneResource(otherUserID),
			allow:   false,
			errText: "not your domain",
		},
		{
			// ownership now enforced at the seam
			name:    "user on owner-less loaded zone deny",
			actor:   regularUser(nil),
			action:  ActionZoneRead,
			res:     zoneResource(), // ID set, OwnerUserID nil
			allow:   false,
			errText: "not your domain",
		},
		{
			name:   "create-time pre-load check stays allowlist-only",
			actor:  regularUser(nil),
			action: ActionRouteRead,
			res:    Resource{Type: "zone", TenantID: tenantA, ZoneID: zoneID}, // no ID, no owner
			allow:  true,
		},
		{
			name:  "create-time pre-load check still applies allowlist",
			actor: regularUser(&models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{uuid.MustParse("30000000-0000-0000-0000-000000000099")}}),

			action:  ActionRouteRead,
			res:     Resource{Type: "zone", TenantID: tenantA, ZoneID: zoneID},
			allow:   false,
			errText: "zone not in allowed list",
		},
		{
			name:   "user-owned API key allow when owner matches",
			actor:  userOwnedKey,
			action: ActionZoneManage,
			res:    ownedZoneResource(userID),
			allow:  true,
		},
		{
			// ownership now enforced at the seam
			name:    "user-owned API key deny when owner differs",
			actor:   userOwnedKey,
			action:  ActionZoneManage,
			res:     ownedZoneResource(otherUserID),
			allow:   false,
			errText: "not your domain",
		},
		{
			// ownership now enforced at the seam: non-zone resources require
			// ownership when the resource carries an owner.
			name:    "user non-owner deny on owned mailbox resource",
			actor:   regularUser(nil),
			action:  ActionMailboxWrite,
			res:     Resource{Type: "mailbox", ID: mboxID, TenantID: tenantA, ZoneID: zoneID, OwnerUserID: &otherUserID},
			allow:   false,
			errText: "not your domain",
		},
		{
			name:   "mailbox resource without owner stays allowlist-only",
			actor:  regularUser(nil),
			action: ActionMailboxWrite,
			res:    mailboxResource(),
			allow:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := az.Authorize(ctx, tc.actor, tc.action, tc.res)
			if tc.allow {
				if err != nil {
					t.Fatalf("expected allow, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected deny, got allow")
			}
			if tc.errText != "" && err.Error() != tc.errText {
				t.Fatalf("expected error %q, got %q", tc.errText, err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CanManageZone predicate (mirrors app.CanManageZone via domainActorParams)
// ---------------------------------------------------------------------------

func TestCanManageZonePredicate(t *testing.T) {
	userOwnedKey := Actor{
		Type:        PrincipalAPIKey,
		ID:          keyID,
		TenantID:    tenantA,
		OwnerUserID: &userID,
	}
	zeroTenantAdmin := admin()
	zeroTenantAdmin.TenantID = uuid.Nil

	cases := []struct {
		name  string
		actor Actor
		zone  *models.DomainZone
		want  bool
	}{
		{"nil zone deny", admin(), nil, false},
		{"super admin cross-tenant allow", superAdmin(), testZone(tenantB, &otherUserID), true},
		{"super admin without tenant allow", Actor{Type: PrincipalUser, ID: userID, IsSuperAdmin: true}, testZone(tenantB, nil), true},
		{"admin with zero TenantID deny", zeroTenantAdmin, testZone(tenantA, &userID), false},
		{"admin cross-tenant deny", admin(), testZone(tenantB, &userID), false},
		{"admin same-tenant allow", admin(), testZone(tenantA, &otherUserID), true},
		{"tenant-wide key same-tenant allow", apiKeyActor(), testZone(tenantA, nil), true},
		{"tenant-wide key cross-tenant deny", apiKeyActor(), testZone(tenantB, nil), false},
		{"user owner allow", regularUser(nil), testZone(tenantA, &userID), true},
		{"user non-owner deny", regularUser(nil), testZone(tenantA, &otherUserID), false},
		{"user owner-less zone deny", regularUser(nil), testZone(tenantA, nil), false},
		{"user-owned API key owner allow", userOwnedKey, testZone(tenantA, &userID), true},
		{"user-owned API key non-owner deny", userOwnedKey, testZone(tenantA, &otherUserID), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageZone(tc.actor, tc.zone); got != tc.want {
				t.Fatalf("CanManageZone = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ZoneAllowed predicate (mirrors handlers' ensureZoneAllowedForActor)
// ---------------------------------------------------------------------------

func TestZoneAllowedPredicate(t *testing.T) {
	otherZone := uuid.MustParse("30000000-0000-0000-0000-000000000099")

	cases := []struct {
		name  string
		actor Actor
		zone  uuid.UUID
		want  bool
	}{
		{"super admin always allowed", superAdmin(), zoneID, true},
		{"admin always allowed", admin(), zoneID, true},
		{"nil permission allowed", regularUser(nil), zoneID, true},
		{"empty allowlist allowed", regularUser(&models.EffectivePermission{}), zoneID, true},
		{"member allowed", regularUser(&models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{zoneID}}), zoneID, true},
		{"non-member denied", regularUser(&models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{otherZone}}), zoneID, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ZoneAllowed(tc.actor, tc.zone); got != tc.want {
				t.Fatalf("ZoneAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Actor helpers and resource constructor
// ---------------------------------------------------------------------------

func TestZoneResourceConstructor(t *testing.T) {
	zone := testZone(tenantA, &userID)
	res := ZoneResource(zone)
	if res.Type != "zone" || res.ID != zone.ID || res.TenantID != zone.TenantID || res.ZoneID != zone.ID {
		t.Fatalf("unexpected resource: %+v", res)
	}
	if res.OwnerUserID == nil || *res.OwnerUserID != userID {
		t.Fatalf("OwnerUserID not propagated: %+v", res.OwnerUserID)
	}
}

func TestActorEffectiveUserID(t *testing.T) {
	if uid := regularUser(nil).EffectiveUserID(); uid == nil || *uid != userID {
		t.Fatalf("user EffectiveUserID = %v, want %s", uid, userID)
	}
	ownedKey := Actor{Type: PrincipalAPIKey, ID: keyID, OwnerUserID: &userID}
	if uid := ownedKey.EffectiveUserID(); uid == nil || *uid != userID {
		t.Fatalf("owned key EffectiveUserID = %v, want %s", uid, userID)
	}
	if uid := apiKeyActor().EffectiveUserID(); uid != nil {
		t.Fatalf("ownerless key EffectiveUserID = %v, want nil", uid)
	}
	if uid := (Actor{}).EffectiveUserID(); uid != nil {
		t.Fatalf("zero actor EffectiveUserID = %v, want nil", uid)
	}
}

func TestActorAuditLabel(t *testing.T) {
	if got := regularUser(nil).AuditLabel(); got != "user:"+userID.String() {
		t.Fatalf("user AuditLabel = %q", got)
	}
	if got := apiKeyActor().AuditLabel(); got != "api_key:"+keyID.String() {
		t.Fatalf("api key AuditLabel = %q", got)
	}
	if got := (Actor{TenantID: tenantA}).AuditLabel(); got != tenantA.String() {
		t.Fatalf("tenant-only AuditLabel = %q", got)
	}
	if got := (Actor{}).AuditLabel(); got != "public" {
		t.Fatalf("anonymous AuditLabel = %q", got)
	}
}
