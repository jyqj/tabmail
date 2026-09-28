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

	t.Run("super admin lists all profiles", func(t *testing.T) {
		st := newStub()
		svc := New(st)
		items, err := svc.ListProfiles(context.Background(), superAdmin(), &tenantID)
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
		items, err := svc.ListProfiles(context.Background(), tenantAdmin(), &tenantID)
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
		_, err := svc.ListProfiles(context.Background(), tenantAdmin(), nil)
		assertKind(t, err, app.KindForbidden, "no tenant context")
	})
}

func TestCreateProfile(t *testing.T) {
	tenantID := uuid.New()
	unknownZoneID := uuid.New()
	zoneInTenant := &models.DomainZone{ID: uuid.New(), TenantID: tenantID}
	zoneOtherTenant := &models.DomainZone{ID: uuid.New(), TenantID: uuid.New()}

	tests := []struct {
		name      string
		actor     authz.Actor
		tenantCtx *uuid.UUID
		in        CreateInput
		zones     []*models.DomainZone
		wantKind  app.ErrorKind
		wantMsg   string
	}{
		{
			name:     "missing name is bad request",
			actor:    tenantAdmin(),
			in:       CreateInput{Name: ""},
			wantKind: app.KindBadRequest,
			wantMsg:  "name is required",
		},
		{
			name:     "tenant admin without tenant context is forbidden",
			actor:    tenantAdmin(),
			in:       CreateInput{Name: "p"},
			wantKind: app.KindForbidden,
			wantMsg:  "no tenant context",
		},
		{
			name:     "global profile cannot carry zone ids",
			actor:    superAdmin(),
			in:       CreateInput{Name: "p", AllowedZoneIDs: []uuid.UUID{zoneInTenant.ID}},
			wantKind: app.KindBadRequest,
			wantMsg:  "allowed_zone_ids require a tenant-scoped permission profile",
		},
		{
			name:      "zone of another tenant is rejected",
			actor:     tenantAdmin(),
			tenantCtx: &tenantID,
			in:        CreateInput{Name: "p", AllowedZoneIDs: []uuid.UUID{zoneOtherTenant.ID}},
			wantKind:  app.KindBadRequest,
			wantMsg:   "zone " + zoneOtherTenant.ID.String() + " not found or does not belong to target tenant",
		},
		{
			name:      "unknown zone is rejected",
			actor:     tenantAdmin(),
			tenantCtx: &tenantID,
			in:        CreateInput{Name: "p", AllowedZoneIDs: []uuid.UUID{unknownZoneID}},
			wantKind:  app.KindBadRequest,
			wantMsg:   "zone " + unknownZoneID.String() + " not found or does not belong to target tenant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStub()
			for _, z := range tt.zones {
				st.zones[z.ID] = z
			}
			_, err := New(st).CreateProfile(context.Background(), tt.actor, tt.tenantCtx, tt.in)
			assertKind(t, err, tt.wantKind, tt.wantMsg)
			if st.created != nil {
				t.Fatal("store must not be called on validation failure")
			}
		})
	}

	t.Run("tenant admin creates in own tenant ignoring body tenant_id", func(t *testing.T) {
		st := newStub()
		st.zones[zoneInTenant.ID] = zoneInTenant
		in := CreateInput{
			Name:           "p",
			TenantID:       ptr(uuid.New()),
			AllowedZoneIDs: []uuid.UUID{zoneInTenant.ID},
		}
		profile, err := New(st).CreateProfile(context.Background(), tenantAdmin(), &tenantID, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile.TenantID == nil || *profile.TenantID != tenantID {
			t.Fatalf("expected tenant-scoped profile, got %v", profile.TenantID)
		}
		if profile.IsSystem {
			t.Fatal("created profile must not be system")
		}
		if profile.ID == uuid.Nil || profile.CreatedAt.IsZero() {
			t.Fatal("expected id and timestamps to be populated")
		}
	})

	t.Run("super admin creates global profile with valid zones", func(t *testing.T) {
		st := newStub()
		st.zones[zoneInTenant.ID] = zoneInTenant
		in := CreateInput{Name: "p", TenantID: &tenantID, AllowedZoneIDs: []uuid.UUID{zoneInTenant.ID}}
		profile, err := New(st).CreateProfile(context.Background(), superAdmin(), nil, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if profile.TenantID == nil || *profile.TenantID != tenantID {
			t.Fatalf("expected super-admin chosen tenant, got %v", profile.TenantID)
		}
	})

	t.Run("zone store failure is internal", func(t *testing.T) {
		st := newStub()
		st.getZoneErr = errors.New("db down")
		in := CreateInput{Name: "p", TenantID: &tenantID, AllowedZoneIDs: []uuid.UUID{uuid.New()}}
		_, err := New(st).CreateProfile(context.Background(), superAdmin(), nil, in)
		assertKind(t, err, app.KindInternal, "")
	})

	t.Run("store failure is internal", func(t *testing.T) {
		st := newStub()
		st.createErr = errors.New("db down")
		_, err := New(st).CreateProfile(context.Background(), tenantAdmin(), &tenantID, CreateInput{Name: "p"})
		assertKind(t, err, app.KindInternal, "")
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
