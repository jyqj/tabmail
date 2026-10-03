package permissions

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func profileCreateServiceActor(tenant uuid.UUID) authz.Actor {
	version := int64(0)
	return authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant, Role: models.RoleAdmin, IsAdmin: true, SessionVersion: &version}
}

func TestProfileCreateServiceMissingPort(t *testing.T) {
	tenant := uuid.New()
	st := newStub()
	// Hide optional ports behind the exact legacy consumer interface. Presence
	// of CreatePermissionProfile must not authorize a guarded-create fallback.
	legacy := struct{ Store }{Store: st}
	out, err := New(legacy).CreateProfile(context.Background(), profileCreateServiceActor(tenant), &tenant, CreateInput{Name: "valid"})
	assertKind(t, err, app.KindInternal, "")
	if out != nil || st.created != nil || st.guardedCreateCalls != 0 {
		t.Fatal("missing guarded port used a legacy writer or returned a profile")
	}
}

func TestProfileCreateServiceNilSuccessFailsClosed(t *testing.T) {
	tenant := uuid.New()
	actor := profileCreateServiceActor(tenant)
	st := newStub()
	st.guardedCreateNilSuccess = true
	out, err := New(st).CreateProfile(context.Background(), actor, &tenant, CreateInput{Name: "valid"})
	assertKind(t, err, app.KindInternal, "")
	if out != nil || st.created != nil || st.guardedCreateCalls != 1 {
		t.Fatal("nil guarded success returned a profile, skipped the guarded port or called legacy create")
	}
	if !reflect.DeepEqual(st.guardedCreateActor, actor) || st.guardedCreateSelected != &tenant || st.guardedCreateRequested == nil || st.guardedCreateRequested.Revision != "" {
		t.Fatal("nil fault fixture changed actor/selection or manufactured a persistent revision")
	}
}

func TestProfileCreateServiceScopeForwarding(t *testing.T) {
	home, selected, requested := uuid.New(), uuid.New(), uuid.New()
	admin := profileCreateServiceActor(home)
	super := admin
	super.Role = models.RoleSuperAdmin
	super.IsAdmin = false
	super.IsSuperAdmin = true
	for _, tc := range []struct {
		name                string
		actor               authz.Actor
		selected, requested *uuid.UUID
	}{
		{"super-selected-vs-requested", super, &selected, &requested},
		{"super-global-request", super, &selected, nil},
		{"super-no-selected-anchor", super, nil, &requested},
		{"admin-raw-foreign-request", admin, &home, &requested},
		{"admin-raw-global-request", admin, &home, nil},
		// These are untrusted hints, not a fake current role. The transaction alone
		// decides whether its stored current actor may create in the requested scope.
		{"cached-super-hint", super, &home, &requested},
		{"cached-reader-hint", authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: home, Role: models.RoleUser, SessionVersion: admin.SessionVersion}, &selected, &requested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newStub()
			result := &models.PermissionProfile{ID: uuid.New(), Name: "explicit port result"}
			st.guardedCreateResult = result
			zones := []uuid.UUID{uuid.New()}
			if tc.requested == nil {
				zones = nil
			}
			in := CreateInput{Name: "requested", Description: "description", TenantID: tc.requested, CanSend: true, DailySendQuota: 1, DailyReceiveQuota: 2, MaxMailboxes: 3, MaxDomains: 4, AllowedZoneIDs: zones, CanCreateDomains: true, CanCreateRoutes: true, CanCreateAPIKeys: true}
			out, err := New(st).CreateProfile(context.Background(), tc.actor, tc.selected, in)
			if err != nil {
				t.Fatal(err)
			}
			p := st.guardedCreateRequested
			if st.guardedCreateCalls != 1 || st.created != nil || out != result || !reflect.DeepEqual(st.guardedCreateActor, tc.actor) || st.guardedCreateSelected != tc.selected || p == nil || p.TenantID != tc.requested {
				t.Fatal("service changed actor/selection/raw requested target or did not consume exact guarded result")
			}
			if p.Name != in.Name || p.Description != in.Description || p.CanSend != in.CanSend || p.DailySendQuota != 1 || p.DailyReceiveQuota != 2 || p.MaxMailboxes != 3 || p.MaxDomains != 4 || !reflect.DeepEqual(p.AllowedZoneIDs, zones) || !p.CanCreateDomains || !p.CanCreateRoutes || !p.CanCreateAPIKeys || p.IsSystem || p.Revision != "" || p.ID == uuid.Nil || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
				t.Fatal("raw creation fields/identity/timestamps changed or persisted revision was fabricated")
			}
		})
	}
}

func TestProfileCreateServiceQuotaValidation(t *testing.T) {
	tenant := uuid.New()
	actor := profileCreateServiceActor(tenant)
	fields := []struct {
		name string
		set  func(*CreateInput, int)
	}{
		{"daily_send_quota", func(in *CreateInput, v int) { in.DailySendQuota = v }},
		{"daily_receive_quota", func(in *CreateInput, v int) { in.DailyReceiveQuota = v }},
		{"max_mailboxes", func(in *CreateInput, v int) { in.MaxMailboxes = v }},
		{"max_domains", func(in *CreateInput, v int) { in.MaxDomains = v }},
	}
	invalid := []int{-1}
	tooLarge := int64(2147483648)
	if strconv.IntSize == 64 {
		invalid = append(invalid, int(tooLarge))
	}
	for _, field := range fields {
		for _, value := range invalid {
			t.Run(field.name+"/"+strconv.Itoa(value), func(t *testing.T) {
				st := newStub()
				in := CreateInput{Name: "valid"}
				field.set(&in, value)
				out, err := New(st).CreateProfile(context.Background(), actor, &tenant, in)
				assertKind(t, err, app.KindBadRequest, "quota must be a nonnegative database integer")
				if out != nil || st.guardedCreateCalls != 0 || st.created != nil {
					t.Fatal("invalid body quota reached a creation writer")
				}
			})
		}
	}
	for _, value := range []int{0, 2147483647} {
		t.Run("valid/"+strconv.Itoa(value), func(t *testing.T) {
			st := newStub()
			in := CreateInput{Name: "valid"}
			for _, field := range fields {
				field.set(&in, value)
			}
			out, err := New(st).CreateProfile(context.Background(), actor, &tenant, in)
			if err != nil || st.guardedCreateCalls != 1 || out != st.guardedCreateRequested || st.created != nil {
				t.Fatalf("valid quota did not reach guarded creation: %v", err)
			}
		})
	}
}

func TestProfileCreateServiceZoneBoundary(t *testing.T) {
	tenant := uuid.New()
	actor := profileCreateServiceActor(tenant)
	zone := uuid.New()
	for _, tc := range []struct {
		name    string
		zones   []uuid.UUID
		portErr error
	}{
		{"nil-inherit", nil, nil},
		{"explicit-empty", []uuid.UUID{}, nil},
		{"explicit-list", []uuid.UUID{zone}, nil},
		{"unknown-zone-port-denial", []uuid.UUID{uuid.New()}, app.BadRequest("domain zone is unavailable in the target company")},
		{"zero-zone-port-denial", []uuid.UUID{uuid.Nil}, app.BadRequest("domain zone identities must be nonzero and distinct")},
		{"duplicate-zone-port-denial", []uuid.UUID{zone, zone}, app.BadRequest("domain zone identities must be nonzero and distinct")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newStub()
			st.getZoneErr = app.Internal(nilEditorPortError{})
			st.guardedCreateErr = tc.portErr
			out, err := New(st).CreateProfile(context.Background(), actor, &tenant, CreateInput{Name: "valid", TenantID: &tenant, AllowedZoneIDs: tc.zones})
			if st.guardedCreateCalls != 1 || st.guardedCreateRequested == nil || !reflect.DeepEqual(st.guardedCreateRequested.AllowedZoneIDs, tc.zones) || st.created != nil {
				t.Fatal("raw nil/empty/list zone intent changed or guarded port was bypassed")
			}
			if tc.portErr != nil {
				assertKind(t, err, app.KindBadRequest, "")
				if out != nil {
					t.Fatal("guarded zone denial returned profile")
				}
			} else if err != nil || out != st.guardedCreateRequested {
				t.Fatalf("service performed legacy zone authorization before guarded transaction: %v", err)
			}
		})
	}
}

func TestProfileCreateServiceAuthorityErrorMapping(t *testing.T) {
	tenant := uuid.New()
	owner := uuid.New()
	for _, actor := range []authz.Actor{
		profileCreateServiceActor(tenant),
		{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, OwnerUserID: &owner, IsSuperAdmin: true},
		{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, TenantWide: true, IsAdmin: true},
	} {
		st := newStub()
		st.guardedCreateErr = authz.ErrForbidden("current authority denied")
		out, err := New(st).CreateProfile(context.Background(), actor, &tenant, CreateInput{Name: "valid"})
		assertKind(t, err, app.KindForbidden, "")
		if out != nil || st.created != nil || st.guardedCreateCalls != 1 || !reflect.DeepEqual(st.guardedCreateActor, actor) {
			t.Fatal("guarded authority denial was replaced by stale-hint authorization or legacy create")
		}
	}
}
