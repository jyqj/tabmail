package permissions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type stubStore struct {
	zones    map[uuid.UUID]*models.DomainZone
	users    map[uuid.UUID]*models.User
	profiles map[uuid.UUID]*models.PermissionProfile

	// recorded calls
	listArg         *uuid.UUID
	created         *models.PermissionProfile
	updated         *models.PermissionProfile
	deletedID       uuid.UUID
	deletedTenant   *uuid.UUID
	upserted        *models.UserPermissionOverride
	deletedOverride uuid.UUID

	getZoneErr error
	listErr    error
	createErr  error

	// Guarded creation is an explicitly observed port, never a legacy writer
	// fallback or a fake current-authority/revision engine.
	guardedCreateActor     authz.Actor
	guardedCreateSelected  *uuid.UUID
	guardedCreateRequested *models.PermissionProfile
	guardedCreateCalls     int
	guardedCreateResult    *models.PermissionProfile
	guardedCreateErr       error
	// Explicit fault injection; false retains every existing fixture outcome.
	guardedCreateNilSuccess bool
}

func (s *stubStore) ListPermissionProfiles(_ context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error) {
	s.listArg = tenantID
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := []*models.PermissionProfile{}
	for _, p := range s.profiles {
		if tenantID == nil {
			out = append(out, p)
			continue
		}
		if p.TenantID != nil && *p.TenantID == *tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

// This is the legacy list-use-case fake, not a model of transactional identity
// refresh. PostgreSQL boundary tests cover the current stored administrator.
func (s *stubStore) ListVisiblePermissionProfiles(ctx context.Context, actor authz.Actor, selected *uuid.UUID) ([]*models.PermissionProfile, error) {
	if actor.Type != authz.PrincipalUser || actor.ID == uuid.Nil {
		return nil, app.Forbidden("interactive administrator required")
	}
	if actor.IsSuperAdmin {
		return s.ListPermissionProfiles(ctx, nil)
	}
	if selected == nil {
		return nil, app.Forbidden("no tenant context")
	}
	if !actor.IsAdmin || actor.TenantID != *selected {
		return nil, app.Forbidden("company administrator required")
	}
	return s.ListPermissionProfiles(ctx, selected)
}

func (s *stubStore) GetPermissionProfile(_ context.Context, id uuid.UUID) (*models.PermissionProfile, error) {
	if p := s.profiles[id]; p != nil {
		cp := *p
		return &cp, nil
	}
	return nil, nil
}

func (s *stubStore) CreatePermissionProfile(_ context.Context, p *models.PermissionProfile) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = p
	return nil
}

func (s *stubStore) CreatePermissionProfileGuarded(_ context.Context, actor authz.Actor, selected *uuid.UUID, requested *models.PermissionProfile) (*models.PermissionProfile, error) {
	s.guardedCreateCalls++
	s.guardedCreateActor, s.guardedCreateSelected, s.guardedCreateRequested = actor, selected, requested
	if s.guardedCreateErr != nil {
		return nil, s.guardedCreateErr
	}
	if s.guardedCreateNilSuccess {
		return nil, nil
	}
	if s.guardedCreateResult != nil {
		return s.guardedCreateResult, nil
	}
	// Echoing the service request is a pure consumer fixture. No persisted
	// revision, role refresh, zone qualification or tenant policy is synthesized.
	return requested, nil
}

func (s *stubStore) UpdatePermissionProfile(_ context.Context, p *models.PermissionProfile) error {
	s.updated = p
	return nil
}

func (s *stubStore) DeletePermissionProfile(_ context.Context, id uuid.UUID, tenantID *uuid.UUID) error {
	s.deletedID = id
	s.deletedTenant = tenantID
	return nil
}

func (s *stubStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	if s.getZoneErr != nil {
		return nil, s.getZoneErr
	}
	if z := s.zones[id]; z != nil {
		cp := *z
		return &cp, nil
	}
	return nil, nil
}

func (s *stubStore) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	if u := s.users[id]; u != nil {
		cp := *u
		return &cp, nil
	}
	return nil, nil
}

func (s *stubStore) UpsertUserPermissionOverride(_ context.Context, o *models.UserPermissionOverride) error {
	s.upserted = o
	return nil
}

func (s *stubStore) DeleteUserPermissionOverride(_ context.Context, userID uuid.UUID) error {
	s.deletedOverride = userID
	return nil
}

func (s *stubStore) EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error) {
	return &models.EffectivePermission{CanSend: true}, nil
}

func newStub() *stubStore {
	return &stubStore{
		zones:    map[uuid.UUID]*models.DomainZone{},
		users:    map[uuid.UUID]*models.User{},
		profiles: map[uuid.UUID]*models.PermissionProfile{},
	}
}

func superAdmin() authz.Actor  { return authz.Actor{ID: uuid.New(), IsSuperAdmin: true} }
func tenantAdmin() authz.Actor { return authz.Actor{ID: uuid.New(), IsAdmin: true} }

func ptr[T any](v T) *T { return &v }

func assertKind(t *testing.T, err error, want app.ErrorKind, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error kind %s, got nil", want)
	}
	got, ok := app.As(err)
	if !ok {
		t.Fatalf("expected app.Error, got %T: %v", err, err)
	}
	if got.Kind != want {
		t.Fatalf("expected kind %s, got %s (%v)", want, got.Kind, err)
	}
	if wantMsg != "" && got.Message != wantMsg {
		t.Fatalf("expected message %q, got %q", wantMsg, got.Message)
	}
}

func TestListProfiles(t *testing.T) {
	tenantID := uuid.New()
	otherID := uuid.New()
	version := int64(0)
	super := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, SessionVersion: &version}
	admin := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleAdmin, IsAdmin: true, SessionVersion: &version}

	t.Run("super admin lists all profiles", func(t *testing.T) {
		st := newStub()
		svc := New(st)
		items, err := svc.ListProfiles(context.Background(), super, &tenantID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.listArg != nil {
			t.Fatalf("super admin must list across tenants, got tenant filter %v", *st.listArg)
		}
		if items == nil {
			t.Fatal("expected non-nil items")
		}
	})

	t.Run("tenant admin lists own tenant", func(t *testing.T) {
		st := newStub()
		st.profiles[uuid.New()] = &models.PermissionProfile{ID: uuid.New(), TenantID: &tenantID}
		st.profiles[uuid.New()] = &models.PermissionProfile{ID: uuid.New(), TenantID: &otherID}
		svc := New(st)
		items, err := svc.ListProfiles(context.Background(), admin, &tenantID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 own-tenant profile, got %d", len(items))
		}
		if st.listArg == nil || *st.listArg != tenantID {
			t.Fatal("expected store scoped to caller tenant")
		}
	})

	t.Run("tenant admin without tenant context is forbidden", func(t *testing.T) {
		svc := New(newStub())
		_, err := svc.ListProfiles(context.Background(), admin, nil)
		assertKind(t, err, app.KindForbidden, "no tenant context")
	})
}

func TestCreateProfile(t *testing.T) {
	tenantID, foreignTenant, zoneID := uuid.New(), uuid.New(), uuid.New()
	version := int64(0)
	admin := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleAdmin, IsAdmin: true, SessionVersion: &version}
	super := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, SessionVersion: &version}
	tests := []struct {
		name     string
		actor    authz.Actor
		selected *uuid.UUID
		in       CreateInput
		portErr  error
		wantKind app.ErrorKind
		wantMsg  string
		calls    int
	}{
		{name: "missing name is bad request", actor: admin, in: CreateInput{}, wantKind: app.KindBadRequest, wantMsg: "name is required"},
		// Scope/domain checks now belong to the guarded transaction. These rows
		// exercise error mapping and request forwarding, not fake DB enforcement.
		{name: "guarded port rejects admin without selected company", actor: admin, in: CreateInput{Name: "p"}, portErr: app.Forbidden("selected tenant is required for company administrators"), wantKind: app.KindForbidden, calls: 1},
		{name: "guarded port rejects global profile zone ids", actor: super, in: CreateInput{Name: "p", AllowedZoneIDs: []uuid.UUID{zoneID}}, portErr: app.BadRequest("global permission profiles cannot carry tenant-local domains"), wantKind: app.KindBadRequest, calls: 1},
		{name: "guarded port rejects foreign tenant zone", actor: admin, selected: &tenantID, in: CreateInput{Name: "p", AllowedZoneIDs: []uuid.UUID{zoneID}}, portErr: app.BadRequest("domain zone is unavailable in the target company"), wantKind: app.KindBadRequest, calls: 1},
		{name: "guarded port rejects unknown zone", actor: super, selected: &tenantID, in: CreateInput{Name: "p", TenantID: &tenantID, AllowedZoneIDs: []uuid.UUID{uuid.New()}}, portErr: app.BadRequest("domain zone is unavailable in the target company"), wantKind: app.KindBadRequest, calls: 1},
		{name: "guarded port zone-store failure is internal", actor: super, selected: &tenantID, in: CreateInput{Name: "p", TenantID: &tenantID, AllowedZoneIDs: []uuid.UUID{zoneID}}, portErr: errors.New("zone DB down"), wantKind: app.KindInternal, calls: 1},
		{name: "guarded port create-store failure is internal", actor: admin, selected: &tenantID, in: CreateInput{Name: "p"}, portErr: errors.New("create DB down"), wantKind: app.KindInternal, calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStub()
			st.guardedCreateErr = tt.portErr
			out, err := New(st).CreateProfile(context.Background(), tt.actor, tt.selected, tt.in)
			assertKind(t, err, tt.wantKind, tt.wantMsg)
			if out != nil || st.created != nil || st.guardedCreateCalls != tt.calls {
				t.Fatal("failed create returned profile, used legacy writer or bypassed expected port")
			}
			if tt.calls == 1 && (st.guardedCreateActor.ID != tt.actor.ID || st.guardedCreateSelected != tt.selected || st.guardedCreateRequested.Name != tt.in.Name || st.guardedCreateRequested.TenantID != tt.in.TenantID) {
				t.Fatal("guarded create input/scope was rewritten before current authority")
			}
		})
	}
	t.Run("tenant admin forwards raw body scope and consumes guarded result", func(t *testing.T) {
		st := newStub()
		// The configured response expresses the store's result. The fake does not
		// derive scope from IsAdmin/IsSuperAdmin hints or emulate persisted revision.
		result := &models.PermissionProfile{TenantID: &tenantID, Name: "p"}
		st.guardedCreateResult = result
		in := CreateInput{Name: "p", TenantID: &foreignTenant, AllowedZoneIDs: []uuid.UUID{zoneID}}
		out, err := New(st).CreateProfile(context.Background(), admin, &tenantID, in)
		if err != nil {
			t.Fatal(err)
		}
		if out != result || st.guardedCreateRequested.TenantID != in.TenantID || st.guardedCreateSelected != &tenantID || st.guardedCreateActor.ID != admin.ID || st.created != nil {
			t.Fatal("raw requested tenant/selected company or guarded response was not preserved")
		}
		if st.guardedCreateRequested.IsSystem || st.guardedCreateRequested.ID == uuid.Nil || st.guardedCreateRequested.CreatedAt.IsZero() || st.guardedCreateRequested.Revision != "" {
			t.Fatal("service request identity/timestamps/system/revision changed")
		}
	})
	t.Run("super admin forwards requested tenant independently of selected company", func(t *testing.T) {
		st := newStub()
		in := CreateInput{Name: "p", TenantID: &foreignTenant, AllowedZoneIDs: []uuid.UUID{zoneID}}
		out, err := New(st).CreateProfile(context.Background(), super, &tenantID, in)
		if err != nil {
			t.Fatal(err)
		}
		if out != st.guardedCreateRequested || out.TenantID != in.TenantID || st.guardedCreateSelected != &tenantID || st.created != nil {
			t.Fatal("super raw target was replaced with selected company or legacy create used")
		}
	})
}

func TestUpdateProfile(t *testing.T) {
	tenantID := uuid.New()
	foreignZoneID := uuid.New()
	otherID := uuid.New()
	zoneInTenant := &models.DomainZone{ID: uuid.New(), TenantID: tenantID}

	tests := []struct {
		name      string
		actor     authz.Actor
		tenantCtx *uuid.UUID
		existing  *models.PermissionProfile
		in        UpdateInput
		wantKind  app.ErrorKind
		wantMsg   string
	}{
		{
			name:     "unknown profile is not found",
			actor:    superAdmin(),
			existing: nil,
			in:       UpdateInput{},
			wantKind: app.KindNotFound,
			wantMsg:  "permission profile not found",
		},
		{
			name:     "system profile cannot be modified",
			actor:    superAdmin(),
			existing: &models.PermissionProfile{ID: uuid.New(), IsSystem: true},
			in:       UpdateInput{},
			wantKind: app.KindForbidden,
			wantMsg:  "cannot modify system profile",
		},
		{
			name:      "global profile is out of reach for tenant admin",
			actor:     tenantAdmin(),
			tenantCtx: &tenantID,
			existing:  &models.PermissionProfile{ID: uuid.New()},
			in:        UpdateInput{},
			wantKind:  app.KindNotFound,
			wantMsg:   "permission profile not found",
		},
		{
			name:      "other-tenant profile is not found",
			actor:     tenantAdmin(),
			tenantCtx: &tenantID,
			existing:  &models.PermissionProfile{ID: uuid.New(), TenantID: &otherID},
			in:        UpdateInput{},
			wantKind:  app.KindNotFound,
			wantMsg:   "permission profile not found",
		},
		{
			name:     "tenant admin without tenant context is forbidden",
			actor:    tenantAdmin(),
			existing: &models.PermissionProfile{ID: uuid.New(), TenantID: &tenantID},
			in:       UpdateInput{},
			wantKind: app.KindForbidden,
			wantMsg:  "no tenant context",
		},
		{
			name:     "zones on global profile are rejected",
			actor:    superAdmin(),
			existing: &models.PermissionProfile{ID: uuid.New()},
			in:       UpdateInput{AllowedZoneIDs: []uuid.UUID{uuid.New()}},
			wantKind: app.KindBadRequest,
			wantMsg:  "allowed_zone_ids require a tenant-scoped permission profile",
		},
		{
			name:     "foreign zone is rejected",
			actor:    superAdmin(),
			existing: &models.PermissionProfile{ID: uuid.New(), TenantID: &tenantID},
			in:       UpdateInput{AllowedZoneIDs: []uuid.UUID{foreignZoneID}},
			wantKind: app.KindBadRequest,
			wantMsg:  "zone " + foreignZoneID.String() + " not found or does not belong to target tenant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStub()
			if tt.existing != nil {
				st.profiles[tt.existing.ID] = tt.existing
			}
			st.zones[zoneInTenant.ID] = zoneInTenant
			var id uuid.UUID
			if tt.existing != nil {
				id = tt.existing.ID
			}
			_, err := New(st).UpdateProfile(context.Background(), tt.actor, tt.tenantCtx, id, tt.in)
			assertKind(t, err, tt.wantKind, tt.wantMsg)
			if st.updated != nil {
				t.Fatal("store must not be called on validation failure")
			}
		})
	}

	t.Run("patch applies only provided fields", func(t *testing.T) {
		st := newStub()
		existing := &models.PermissionProfile{
			ID:             uuid.New(),
			TenantID:       &tenantID,
			Name:           "old",
			CanSend:        false,
			DailySendQuota: 10,
		}
		st.profiles[existing.ID] = existing
		st.zones[zoneInTenant.ID] = zoneInTenant

		newName := "new"
		in := UpdateInput{Name: &newName, AllowedZoneIDs: []uuid.UUID{zoneInTenant.ID}}
		profile, err := New(st).UpdateProfile(context.Background(), tenantAdmin(), &tenantID, existing.ID, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile.Name != "new" {
			t.Fatalf("expected name updated, got %q", profile.Name)
		}
		if profile.CanSend {
			t.Fatal("unspecified field must stay unchanged")
		}
		if profile.DailySendQuota != 10 {
			t.Fatal("unspecified quota must stay unchanged")
		}
		if len(profile.AllowedZoneIDs) != 1 || profile.AllowedZoneIDs[0] != zoneInTenant.ID {
			t.Fatalf("expected zones replaced, got %v", profile.AllowedZoneIDs)
		}
	})

	t.Run("empty zone slice clears zones without validation", func(t *testing.T) {
		st := newStub()
		existing := &models.PermissionProfile{ID: uuid.New(), TenantID: &tenantID, AllowedZoneIDs: []uuid.UUID{uuid.New()}}
		st.profiles[existing.ID] = existing

		in := UpdateInput{AllowedZoneIDs: []uuid.UUID{}}
		profile, err := New(st).UpdateProfile(context.Background(), tenantAdmin(), &tenantID, existing.ID, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(profile.AllowedZoneIDs) != 0 {
			t.Fatalf("expected zones cleared, got %v", profile.AllowedZoneIDs)
		}
	})
}

func TestDeleteProfile(t *testing.T) {
	tenantID := uuid.New()

	t.Run("system profile cannot be deleted", func(t *testing.T) {
		st := newStub()
		id := uuid.New()
		st.profiles[id] = &models.PermissionProfile{ID: id, IsSystem: true}
		err := New(st).DeleteProfile(context.Background(), superAdmin(), nil, id)
		assertKind(t, err, app.KindForbidden, "cannot delete system profile")
		if st.deletedID != uuid.Nil {
			t.Fatal("store must not be called on guard failure")
		}
	})

	t.Run("other-tenant profile is not found", func(t *testing.T) {
		st := newStub()
		other := uuid.New()
		id := uuid.New()
		st.profiles[id] = &models.PermissionProfile{ID: id, TenantID: &other}
		err := New(st).DeleteProfile(context.Background(), tenantAdmin(), &tenantID, id)
		assertKind(t, err, app.KindNotFound, "permission profile not found")
	})

	t.Run("tenant admin delete is tenant-scoped at the store", func(t *testing.T) {
		st := newStub()
		id := uuid.New()
		st.profiles[id] = &models.PermissionProfile{ID: id, TenantID: &tenantID}
		if err := New(st).DeleteProfile(context.Background(), tenantAdmin(), &tenantID, id); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.deletedID != id {
			t.Fatalf("expected deleted id %s, got %s", id, st.deletedID)
		}
		if st.deletedTenant == nil || *st.deletedTenant != tenantID {
			t.Fatal("expected store delete scoped to caller tenant")
		}
	})

	t.Run("super admin delete is unscoped", func(t *testing.T) {
		st := newStub()
		id := uuid.New()
		st.profiles[id] = &models.PermissionProfile{ID: id, TenantID: &tenantID}
		if err := New(st).DeleteProfile(context.Background(), superAdmin(), nil, id); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.deletedTenant != nil {
			t.Fatal("expected unscoped delete for super admin")
		}
	})
}

func TestUserOverridePaths(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	foreignUserID := uuid.New()

	stub := func() *stubStore {
		st := newStub()
		st.users[userID] = &models.User{ID: userID, TenantID: tenantID}
		st.users[foreignUserID] = &models.User{ID: foreignUserID, TenantID: uuid.New()}
		return st
	}

	t.Run("nil tenant context is forbidden", func(t *testing.T) {
		svc := New(stub())
		_, err := svc.GetUserPermission(context.Background(), nil, userID)
		assertKind(t, err, app.KindForbidden, "no tenant context")
		err = svc.DeleteUserPermissionOverride(context.Background(), nil, userID)
		assertKind(t, err, app.KindForbidden, "no tenant context")
		_, err = svc.SetUserPermissionOverride(context.Background(), nil, userID, &models.UserPermissionOverride{})
		assertKind(t, err, app.KindForbidden, "no tenant context")
	})

	t.Run("foreign user is not found", func(t *testing.T) {
		svc := New(stub())
		_, err := svc.GetUserPermission(context.Background(), &tenantID, foreignUserID)
		assertKind(t, err, app.KindNotFound, "user not found")
		err = svc.DeleteUserPermissionOverride(context.Background(), &tenantID, foreignUserID)
		assertKind(t, err, app.KindNotFound, "user not found")
		_, err = svc.SetUserPermissionOverride(context.Background(), &tenantID, foreignUserID, &models.UserPermissionOverride{})
		assertKind(t, err, app.KindNotFound, "user not found")
	})

	t.Run("override zone of another tenant is rejected", func(t *testing.T) {
		st := stub()
		foreignZone := uuid.New()
		st.zones[foreignZone] = &models.DomainZone{ID: foreignZone, TenantID: uuid.New()}
		_, err := New(st).SetUserPermissionOverride(context.Background(), &tenantID, userID,
			&models.UserPermissionOverride{AllowedZoneIDs: []uuid.UUID{foreignZone}})
		assertKind(t, err, app.KindBadRequest,
			"zone "+foreignZone.String()+" not found or does not belong to tenant")
		if st.upserted != nil {
			t.Fatal("store must not be called on validation failure")
		}
	})

	t.Run("valid override binds user id and upserts", func(t *testing.T) {
		st := stub()
		zone := uuid.New()
		st.zones[zone] = &models.DomainZone{ID: zone, TenantID: tenantID}
		canSend := true
		out, err := New(st).SetUserPermissionOverride(context.Background(), &tenantID, userID,
			&models.UserPermissionOverride{CanSend: &canSend, AllowedZoneIDs: []uuid.UUID{zone}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.UserID != userID {
			t.Fatalf("expected user id bound, got %s", out.UserID)
		}
		if st.upserted == nil || st.upserted.UserID != userID {
			t.Fatal("expected upsert with bound user id")
		}
	})

	t.Run("delete override reaches store", func(t *testing.T) {
		st := stub()
		if err := New(st).DeleteUserPermissionOverride(context.Background(), &tenantID, userID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st.deletedOverride != userID {
			t.Fatalf("expected deleted override for %s, got %s", userID, st.deletedOverride)
		}
	})

	t.Run("get user permission returns effective permission", func(t *testing.T) {
		perm, err := New(stub()).GetUserPermission(context.Background(), &tenantID, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if perm == nil || !perm.CanSend {
			t.Fatal("expected effective permission from store")
		}
	})
}

func TestMyPermissions(t *testing.T) {
	st := newStub()
	perm, err := New(st).MyPermissions(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perm == nil || !perm.CanSend {
		t.Fatal("expected own effective permission")
	}
}
