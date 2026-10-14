package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// This fast assembly test pins credential-specific interpretation, not the PG
// merger. The companion PostgreSQL stage suite owns canonical merge evidence.
type r5PrincipalAuthStore struct {
	*testutil.FakeStore
	permission      *models.EffectivePermission
	tenant          *models.Tenant
	key             *models.TenantAPIKey
	permissionReads int
}

func (s *r5PrincipalAuthStore) EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error) {
	s.permissionReads++
	p := *s.permission
	p.AllowedZoneIDs = append([]uuid.UUID(nil), p.AllowedZoneIDs...)
	return &p, nil
}
func (s *r5PrincipalAuthStore) ResolveAPIKey(context.Context, string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	return s.tenant, &s.key.ID, s.key.Scopes, s.key.AllowedZoneIDs, s.key.OwnerUserID, nil
}
func (s *r5PrincipalAuthStore) TouchAPIKey(context.Context, uuid.UUID, string) error { return nil }

func TestR5PermissionPrincipalCredentialMatrix(t *testing.T) {
	for _, principal := range []string{"jwt-user", "jwt-admin", "jwt-super-admin", "owned-user-key", "owned-admin-key", "ownerless-key"} {
		for _, mode := range []string{"all", "list", "none", "legacy-inherited-list"} {
			for _, canSend := range []bool{false, true} {
				for _, keyAllows := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/send=%t/key-zone=%t", principal, mode, canSend, keyAllows), func(t *testing.T) {
						zone, other := uuid.New(), uuid.New()
						tenant := &models.Tenant{ID: uuid.New()}
						st := &r5PrincipalAuthStore{FakeStore: testutil.NewFakeStore(), tenant: tenant, permission: &models.EffectivePermission{CanSend: canSend, DomainAccessMode: mode}}
						if mode == "legacy-inherited-list" {
							st.permission.DomainAccessMode = ""
						}
						if mode == "list" || mode == "legacy-inherited-list" {
							st.permission.AllowedZoneIDs = []uuid.UUID{zone}
						}
						st.SeedTenant(tenant)
						role := models.RoleUser
						if principal == "jwt-admin" || principal == "owned-admin-key" {
							role = models.RoleAdmin
						}
						if principal == "jwt-super-admin" {
							role = models.RoleSuperAdmin
						}
						u := &models.User{ID: uuid.New(), TenantID: tenant.ID, Role: role, IsActive: true, SessionVersion: 7}
						if err := st.CreateUser(context.Background(), u); err != nil {
							t.Fatal(err)
						}
						keyZones := []uuid.UUID{zone}
						if !keyAllows {
							keyZones = []uuid.UUID{other}
						}
						st.key = &models.TenantAPIKey{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &u.ID, Scopes: []string{"send:write"}, AllowedZoneIDs: keyZones}
						if principal == "ownerless-key" {
							st.key.OwnerUserID = nil
						}
						var got authz.Actor
						h := Auth(NewCachedAuthStore(st, nil), "r5-principal-fixture", tenant.ID.String())(PermissionLoader(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ActorFromContext(r.Context()); w.WriteHeader(204) })))
						req := httptest.NewRequest("GET", "/", nil)
						if principal == "jwt-user" || principal == "jwt-admin" || principal == "jwt-super-admin" {
							token, err := authn.IssueAccessToken("r5-principal-fixture", u)
							if err != nil {
								t.Fatal(err)
							}
							req.Header.Set("Authorization", "Bearer "+token)
						} else {
							req.Header.Set("X-API-Key", "r5-synthetic-key")
						}
						rr := httptest.NewRecorder()
						h.ServeHTTP(rr, req)
						if rr.Code != 204 {
							t.Fatalf("credential status=%d body=%s", rr.Code, rr.Body.String())
						}
						bypass := principal == "jwt-admin" || principal == "jwt-super-admin"
						wantSend, wantZone := canSend, mode != "none"
						if bypass {
							wantSend, wantZone = true, true
						}
						if principal == "owned-user-key" || principal == "owned-admin-key" {
							wantZone = wantZone && keyAllows
						}
						if principal == "ownerless-key" {
							wantSend, wantZone = true, keyAllows
						}
						if got.Permission == nil || got.Permission.CanSend != wantSend || got.Permission.AllowsZone(zone) != wantZone {
							t.Fatalf("permission=%+v want send=%t zone=%t", got.Permission, wantSend, wantZone)
						}
						expectedReads := 1
						if bypass || principal == "ownerless-key" {
							expectedReads = 0
						}
						if st.permissionReads != expectedReads {
							t.Fatalf("canonical reads=%d want=%d", st.permissionReads, expectedReads)
						}
						if principal == "owned-admin-key" && (got.IsTenantAdmin() || got.Type != authz.PrincipalAPIKey) {
							t.Fatal("admin-owned key acquired JWT management bypass")
						}
						if principal == "ownerless-key" && (!got.TenantWide || got.EffectiveUserID() != nil) {
							t.Fatal("ownerless key lost integration boundary")
						}
					})
				}
			}
		}
	}
}

func TestR5PermissionPrincipalCacheReadsCurrentAuthority(t *testing.T) {
	st := &r5PrincipalAuthStore{FakeStore: testutil.NewFakeStore(), permission: &models.EffectivePermission{CanSend: true}}
	u := &models.User{ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser, IsActive: true, SessionVersion: 1}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	cached := NewCachedAuthStore(st, nil)
	first, err := cached.EffectivePermission(context.Background(), u.ID)
	if err != nil || !first.CanSend {
		t.Fatal("positive control", err)
	}
	st.permission = &models.EffectivePermission{CanSend: false, DomainAccessMode: "none"}
	current, err := cached.EffectivePermission(context.Background(), u.ID)
	if err != nil || current.CanSend || current.AllowsZone(uuid.New()) || st.permissionReads != 2 {
		t.Fatalf("permission cached across revoke: %+v reads=%d err=%v", current, st.permissionReads, err)
	}
	firstUser, err := cached.GetUser(context.Background(), u.ID)
	if err != nil || !firstUser.IsActive {
		t.Fatal("active positive control", err)
	}
	u.IsActive = false
	u.SessionVersion++
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	currentUser, err := cached.GetUser(context.Background(), u.ID)
	if err != nil || currentUser.IsActive || currentUser.SessionVersion != 2 {
		t.Fatalf("user cached across freeze: %+v err=%v", currentUser, err)
	}
}
