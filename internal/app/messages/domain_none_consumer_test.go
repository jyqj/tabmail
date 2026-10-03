package messageapp

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"testing"
)

func TestDomainNoneConsumerMessageResolver(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"", "all", "none", "list", "unknown"} {
		for _, ownKey := range []bool{false, true} {
			owner := uuid.New()
			_, svc, tenant, mb := seededMessageService(t, models.AccessAPIKey, &owner)
			for _, ids := range [][]uuid.UUID{nil, {}, {mb.ZoneID}, {uuid.New()}} {
				p := &models.EffectivePermission{DomainAccessMode: mode, AllowedZoneIDs: ids}
				viewer := Viewer{Tenant: tenant, AuthMode: AuthModeUser, UserID: &owner, Permission: p}
				if ownKey {
					viewer.AuthMode = AuthModeAPIKey
					viewer.UserID = nil
					viewer.OwnerUserID = &owner
				}
				want := mode == "all" || (mode == "" && (len(ids) == 0 || ids[0] == mb.ZoneID)) || (mode == "list" && len(ids) > 0 && ids[0] == mb.ZoneID)
				for _, write := range []bool{false, true} {
					var err error
					if write {
						_, err = svc.ResolveMailboxForWrite(ctx, mb.FullAddress, viewer)
					} else {
						_, err = svc.ResolveMailbox(ctx, mb.FullAddress, viewer)
					}
					if want && err != nil {
						t.Errorf("mode=%q ids=%v ownedKey=%v write=%v want permit got %v", mode, ids, ownKey, write, err)
					}
					if !want {
						if e, ok := app.As(err); !ok || e.Kind != app.KindForbidden {
							t.Errorf("mode=%q ids=%v ownedKey=%v write=%v want forbid got %v", mode, ids, ownKey, write, err)
						}
					}
				}
			}
		}
	}
}

func TestDomainNoneConsumerMessageLegacyAndAdminBoundary(t *testing.T) {
	owner := uuid.New()
	_, svc, tenant, mb := seededMessageService(t, models.AccessAPIKey, &owner)
	for _, ids := range [][]uuid.UUID{nil, {}, {mb.ZoneID}, {uuid.New()}} {
		v := Viewer{Tenant: tenant, AuthMode: AuthModeUser, UserID: &owner, AllowedZoneIDs: ids}
		want := len(ids) == 0 || ids[0] == mb.ZoneID
		if viewerZoneAllowed(v, mb.ZoneID) != want {
			t.Errorf("legacy fallback changed ids=%v", ids)
		}
	}
	// Existing administrator metadata qualification remains, but its content
	// check still uses the explicit profile and does not acquire body authority.
	v := Viewer{Tenant: tenant, AuthMode: AuthModeUser, UserID: &owner, IsAdmin: true, Permission: &models.EffectivePermission{DomainAccessMode: "none"}}
	if _, err := svc.ResolveMailbox(context.Background(), mb.FullAddress, v); err != nil {
		t.Errorf("admin metadata qualification changed: %v", err)
	}
	allowed, err := svc.canReadMessageContent(context.Background(), mb, v)
	if allowed || err == nil {
		t.Errorf("admin none profile acquired content: allowed=%v err=%v", allowed, err)
	}
}
