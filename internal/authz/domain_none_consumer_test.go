package authz

import (
	"encoding/json"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"testing"
)

func domainConsumerPermission(t *testing.T, mode string) *models.EffectivePermission {
	t.Helper()
	var p models.EffectivePermission
	if err := json.Unmarshal([]byte(`{"can_send":true,"domain_access_mode":"`+mode+`","allowed_zone_ids":[]}`), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestDomainNoneConsumerMailboxAndZoneScope(t *testing.T) {
	tenant, user, zone := uuid.New(), uuid.New(), uuid.New()
	for _, mode := range []string{"", "all", "none", "list"} {
		for _, admin := range []bool{false, true} {
			a := Actor{Type: PrincipalUser, ID: user, TenantID: tenant, Permission: domainConsumerPermission(t, mode), IsAdmin: admin}
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, OwnerUserID: &user}
			d := EvaluateMailboxAccess(a, mb, nil)
			want := mode == "" || mode == "all"
			if d.CanRead != want || d.CanOrganize != want || d.CanSend != want || d.CanManage != admin {
				t.Errorf("mode=%q admin=%v decision=%+v", mode, admin, d)
			}
			f := ZoneListScope(a, tenant)
			if f.TenantID != tenant || f.AllZones != want || len(f.ZoneIDs) != 0 {
				t.Errorf("mode=%q admin=%v scope=%+v", mode, admin, f)
			}
		}
	}
}

func TestDomainNoneConsumerOwnerScopeAndManagementBoundary(t *testing.T) {
	tenant, user, zone := uuid.New(), uuid.New(), uuid.New()
	for _, mode := range []string{"", "all", "none", "list", "unknown"} {
		for _, ids := range [][]uuid.UUID{nil, {}, {zone}} {
			p := domainConsumerPermission(t, mode)
			p.AllowedZoneIDs = ids
			a := Actor{Type: PrincipalUser, ID: user, TenantID: tenant, Permission: p}
			wantRestricted := mode != "all" && (mode != "" || len(ids) > 0)
			f := OwnerListScope(a, tenant)
			if f.RestrictZones != wantRestricted || f.TenantID != tenant || f.UserID == nil || *f.UserID != user {
				t.Errorf("mode=%q ids=%v scope=%+v", mode, ids, f)
			}
			wantZone := mode == "all" || (mode == "" && (len(ids) == 0 || ids[0] == zone)) || (mode == "list" && len(ids) > 0)
			if ZoneAllowed(a, zone) != wantZone {
				t.Errorf("member mode=%q ids=%v zone=%v", mode, ids, ZoneAllowed(a, zone))
			}
			a.IsAdmin = true
			if !ZoneAllowed(a, zone) {
				t.Errorf("existing management bypass lost mode=%q", mode)
			}
			adminScope := OwnerListScope(a, tenant)
			if !adminScope.AllInTenant || adminScope.RestrictZones != wantRestricted {
				t.Errorf("admin scope=%+v", adminScope)
			}
			if (mode == "none" || mode == "unknown") && len(f.AllowedZoneIDs) != 0 {
				t.Errorf("denied scope retained stale ids: %+v", f)
			}
		}
	}
}

func TestDomainNoneConsumerMailboxGrantCannotWidenProfile(t *testing.T) {
	tenant, user, zone, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone}
	grant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: user, CanRead: true, CanOrganize: true, CanSend: true, TemplateOnly: true}
	for _, mode := range []string{"", "all", "none", "list", "unknown"} {
		for _, ids := range [][]uuid.UUID{nil, {}, {zone}, {other}} {
			for _, admin := range []bool{false, true} {
				p := domainConsumerPermission(t, mode)
				p.AllowedZoneIDs = ids
				a := Actor{Type: PrincipalUser, ID: user, TenantID: tenant, Permission: p, IsAdmin: admin}
				d := EvaluateMailboxAccess(a, mb, grant)
				want := mode == "all" || (mode == "" && (len(ids) == 0 || ids[0] == zone)) || (mode == "list" && len(ids) > 0 && ids[0] == zone)
				if d.CanRead != want || d.CanOrganize != want || d.CanSend != want || d.TemplateOnly != want || d.CanManage != admin {
					t.Errorf("mode=%q ids=%v admin=%v decision=%+v", mode, ids, admin, d)
				}
			}
		}
	}
}
