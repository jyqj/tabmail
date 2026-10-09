package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

type r5PermissionAuthorityHTTPResult struct {
	status int
	body   []byte
	err    error
}

// Actual TCP and shipping api.NewRouter, including JWT/session freshness,
// RequireAdmin, selected-tenant middleware, strict wire decoding and PgStore.
func r5PermissionAuthorityHTTPRequest(ctx context.Context, f *testpg.R5HTTPFixture, token, method, path, raw string, selected uuid.UUID) r5PermissionAuthorityHTTPResult {
	req, err := http.NewRequestWithContext(ctx, method, f.Server.URL+path, strings.NewReader(raw))
	if err != nil {
		return r5PermissionAuthorityHTTPResult{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(token, "tb_") {
		req.Header.Set("X-API-Key", token)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if selected != uuid.Nil {
		req.Header.Set("X-Tenant-ID", selected.String())
	}
	res, err := f.Server.Client().Do(req)
	if err != nil {
		return r5PermissionAuthorityHTTPResult{err: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	return r5PermissionAuthorityHTTPResult{status: res.StatusCode, body: data, err: err}
}

func r5PermissionAuthorityHTTPStatus(t *testing.T, result r5PermissionAuthorityHTTPResult, want int) []byte {
	t.Helper()
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.status != want {
		t.Fatalf("authority HTTP status=%d want=%d", result.status, want)
	}
	if want >= 400 {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(result.body, &envelope); err != nil {
			t.Fatal(err)
		}
		code := map[int]string{401: "UNAUTHORIZED", 403: "FORBIDDEN", 404: "NOT_FOUND", 409: "CONFLICT"}[want]
		if code != "" && envelope.Error.Code != code {
			t.Fatalf("HTTP error code=%s want=%s", envelope.Error.Code, code)
		}
	}
	return result.body
}

func r5PermissionAuthorityHTTPCall(t *testing.T, f *testpg.R5HTTPFixture, token, method, path, raw string, selected uuid.UUID, want int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return r5PermissionAuthorityHTTPStatus(t, r5PermissionAuthorityHTTPRequest(ctx, f, token, method, path, raw, selected), want)
}

func r5PermissionAuthorityHTTPState(t *testing.T, f *testpg.R5HTTPFixture) string {
	t.Helper()
	var state string
	// Sequences are not composite rows; reading explicit columns is passive.
	// Allocator equality proves pre-mutation denial/read/no-op only. An aborted
	// audit/outbox write may retain a nontransactional nextval gap, while its
	// users/profiles permission_revision columns must still roll back.
	if err := f.Pool.QueryRow(context.Background(), `SELECT jsonb_build_object('users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY u.id) FROM users u),'overrides',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM user_permission_overrides o),'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM permission_profiles p),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM outbox_events o),'allocator',(SELECT jsonb_build_object('last_value',last_value,'is_called',is_called) FROM permission_editor_revision_seq))::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func r5PermissionAuthorityHTTPSuper(t *testing.T, f *testpg.R5HTTPFixture) authz.Actor {
	t.Helper()
	// RefreshJWT signs the real current stored user, not a synthetic role claim.
	u, err := f.Store.GetUser(context.Background(), f.Companies[0].Users["reader"].ID)
	if err != nil {
		t.Fatal(err)
	}
	u.Role = models.RoleSuperAdmin
	if err := f.Store.UpdateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	f.RefreshJWT(t, 0, "reader")
	v := u.SessionVersion
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, IsSuperAdmin: true, SessionVersion: &v}
}

func r5PermissionAuthorityHTTPUser(t *testing.T, f *testpg.R5HTTPFixture, tenant uuid.UUID, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{TenantID: tenant, Email: uuid.NewString() + "@http-authority.test", Role: role, IsActive: true, PasswordHash: "test-only-http-authority-password"}
	if err := f.Store.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func r5PermissionAuthorityHTTPRevision(t *testing.T, f *testpg.R5HTTPFixture, super authz.Actor, target, tenant uuid.UUID, missing bool) company.PermissionRevision {
	t.Helper()
	if missing {
		return company.PermissionRevision{UserID: target, TenantID: tenant, UserRevision: "1"}
	}
	super.TenantID = tenant
	s, err := f.Store.GetPermissionEditorSnapshot(context.Background(), super, target)
	if err != nil {
		t.Fatal(err)
	}
	return s.Revision
}

func r5PermissionAuthorityHTTPProfile(t *testing.T, f *testpg.R5HTTPFixture, super authz.Actor, target, tenant uuid.UUID) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: &tenant, Name: "HTTP authority " + uuid.NewString(), CanSend: true, DailySendQuota: 31, CanCreateAPIKeys: true}
	if err := f.Store.CreatePermissionProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p, err := f.Store.GetPermissionProfile(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := r5PermissionAuthorityHTTPRevision(t, f, super, target, tenant, false)
	super.TenantID = tenant
	if _, err := f.Store.AssignPermissionEditor(context.Background(), super, target, company.PermissionAssignmentCommand{ExpectedRevision: r, ProfileID: &p.ID, ProfileRevision: &p.Revision}); err != nil {
		t.Fatal(err)
	}
	return p
}

func r5PermissionAuthorityHTTPWire(t *testing.T, op string, target uuid.UUID, r company.PermissionRevision, p *models.PermissionProfile, members []company.PermissionRevision) (string, string, string) {
	t.Helper()
	base := r5EditorHTTPPath(target)
	profile := "/api/v1/admin/permissions/" + p.ID.String()
	switch op {
	case "get":
		return "GET", base, ""
	case "patch":
		return "PATCH", base, r5EditorHTTPCommand(t, r, `{}`)
	case "assignment":
		return "POST", base + "/assignment", r5EditorHTTPJSON(t, map[string]any{"expected_revision": r, "profile_id": r.ProfileID, "profile_revision": r.ProfileRevision, "patch": map[string]any{}})
	case "profile-cas":
		return "PATCH", profile, r5EditorHTTPJSON(t, map[string]any{"expected_revision": p.Revision, "description": "HTTP authority CAS observation"})
	case "preview":
		return "GET", profile + "/deletion-preview", ""
	case "delete":
		return "DELETE", profile, r5EditorHTTPJSON(t, map[string]any{"expected_revision": p.Revision, "confirmed_members": members})
	default:
		t.Fatal("unknown HTTP authority operation")
		return "", "", ""
	}
}

// Pure wire sanity: does not substitute for any real-DB matrix evidence.
func TestR5PermissionAuthorityHTTPWireFixture(t *testing.T) {
	for _, selected := range []bool{false, true} {
		r := company.PermissionRevision{UserID: uuid.New(), TenantID: uuid.New(), UserRevision: "11"}
		if selected {
			id, revision := uuid.New(), "17"
			r.ProfileID, r.ProfileRevision = &id, &revision
		}
		cmd := company.PermissionAssignmentCommand{ExpectedRevision: r, ProfileID: r.ProfileID, ProfileRevision: r.ProfileRevision}
		raw := r5EditorHTTPAssignmentJSON(t, cmd)
		decoded, err := company.DecodePermissionAssignmentCommand(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !decoded.ExpectedRevision.Equal(r) || !decoded.Patch.Empty() || (decoded.ProfileID == nil) != !selected || (decoded.ProfileRevision == nil) != !selected {
			t.Fatal("HTTP assignment fixture changed required nullable wire observations")
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 4 || string(body["patch"]) != "{}" {
			t.Fatal("internal presence state escaped into assignment JSON patch")
		}
	}
}

func TestR5PermissionAuthorityHTTPMatrixHierarchy(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	super := r5PermissionAuthorityHTTPSuper(t, f)
	c := f.Companies[0]
	for _, tc := range []struct {
		name, jwt string
		user      *models.User
		role      models.UserRole
	}{
		{"user", "sender", c.Users["sender"], models.RoleUser},
		{"admin", "admin", c.Admin, models.RoleAdmin},
		{"super-admin", "reader", c.Users["reader"], models.RoleSuperAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.RefreshJWT(t, 0, tc.jwt)
			lower, upper := models.RoleUser, models.RoleAdmin
			if tc.role == models.RoleAdmin {
				upper = models.RoleSuperAdmin
			}
			if tc.role == models.RoleSuperAdmin {
				lower, upper = models.RoleAdmin, models.RoleSuperAdmin
			}
			// No fictional role below user or above super_admin: extremal slots
			// use peers and explicitly name that boundary.
			for _, target := range []struct {
				name    string
				user    *models.User
				tenant  uuid.UUID
				missing bool
			}{
				{"self", tc.user, c.Tenant.ID, false},
				{"lower-or-minimum-peer", r5PermissionAuthorityHTTPUser(t, f, c.Tenant.ID, lower), c.Tenant.ID, false},
				{"peer", r5PermissionAuthorityHTTPUser(t, f, c.Tenant.ID, tc.role), c.Tenant.ID, false},
				{"upper-or-maximum-peer", r5PermissionAuthorityHTTPUser(t, f, c.Tenant.ID, upper), c.Tenant.ID, false},
				{"cross-tenant", r5PermissionAuthorityHTTPUser(t, f, f.Companies[1].Tenant.ID, models.RoleUser), f.Companies[1].Tenant.ID, false},
				{"missing", &models.User{ID: uuid.New()}, c.Tenant.ID, true},
			} {
				t.Run(target.name, func(t *testing.T) {
					p := &models.PermissionProfile{ID: uuid.New(), TenantID: &c.Tenant.ID, Name: "Missing HTTP authority profile", Revision: "1"}
					if !target.missing {
						p = r5PermissionAuthorityHTTPProfile(t, f, super, target.user.ID, target.tenant)
					}
					want := 200
					// The self fixture pointer predates promotion to super_admin;
					// hierarchy expectations use the persisted target role.
					targetRole := target.user.Role
					if target.name == "self" {
						targetRole = tc.role
					}
					if tc.role == models.RoleUser {
						want = 403
					} else if target.missing || target.tenant != c.Tenant.ID {
						want = 404
					} else if tc.role == models.RoleAdmin && targetRole != models.RoleUser {
						want = 403
					}
					for _, op := range []string{"get", "patch", "assignment", "profile-cas", "preview", "delete"} {
						t.Run(op, func(t *testing.T) {
							r := r5PermissionAuthorityHTTPRevision(t, f, super, target.user.ID, target.tenant, target.missing)
							r.TenantID = c.Tenant.ID // Match actual request context; avoid early PatchEditor 409.
							members := []company.PermissionRevision{}
							if !target.missing {
								var err error
								p, err = f.Store.GetPermissionProfile(context.Background(), p.ID)
								if err != nil {
									t.Fatal(err)
								}
								scope := super
								scope.TenantID = target.tenant
								preview, err := f.Store.GetPermissionProfileDeletionPreview(context.Background(), scope, p.ID)
								if err != nil {
									t.Fatal(err)
								}
								members = preview.Members
							}
							method, path, raw := r5PermissionAuthorityHTTPWire(t, op, target.user.ID, r, p, members)
							status := want
							if want == 200 && op == "delete" {
								status = 204
							}
							before := r5PermissionAuthorityHTTPState(t, f)
							r5PermissionAuthorityHTTPCall(t, f, f.JWT(0, tc.jwt), method, path, raw, uuid.Nil, status)
							if (want != 200 || op == "get" || op == "patch" || op == "assignment" || op == "preview") && r5PermissionAuthorityHTTPState(t, f) != before {
								t.Fatal("HTTP denial/read/no-op consumed revision or changed durable effects")
							}
						})
					}
					if tc.role == models.RoleSuperAdmin && target.tenant != c.Tenant.ID {
						// All six formal ports also work with the legitimate selected
						// company, including true writes and confirmed deletion.
						for _, op := range []string{"get", "patch", "assignment", "profile-cas", "preview", "delete"} {
							r := r5PermissionAuthorityHTTPRevision(t, f, super, target.user.ID, target.tenant, false)
							var err error
							p, err = f.Store.GetPermissionProfile(context.Background(), p.ID)
							if err != nil {
								t.Fatal(err)
							}
							scope := super
							scope.TenantID = target.tenant
							preview, err := f.Store.GetPermissionProfileDeletionPreview(context.Background(), scope, p.ID)
							if err != nil {
								t.Fatal(err)
							}
							method, path, raw := r5PermissionAuthorityHTTPWire(t, op, target.user.ID, r, p, preview.Members)
							status := 200
							if op == "delete" {
								status = 204
							}
							r5PermissionAuthorityHTTPCall(t, f, f.JWT(0, tc.jwt), method, path, raw, target.tenant, status)
						}
					}
				})
			}
		})
	}
}

func TestR5PermissionAuthorityHTTPMatrixNonInteractive(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	super := r5PermissionAuthorityHTTPSuper(t, f)
	c := f.Companies[0]
	p := r5PermissionAuthorityHTTPProfile(t, f, super, c.Users["sender"].ID, c.Tenant.ID)
	r := r5PermissionAuthorityHTTPRevision(t, f, super, c.Users["sender"].ID, c.Tenant.ID, false)
	preview, err := f.Store.GetPermissionProfileDeletionPreview(context.Background(), super, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	// PgStore seeds real persisted keys so ownership is unambiguous; every
	// attempted management command still traverses ResolveAPIKey and router.
	issue := func(owner *uuid.UUID) string {
		raw := "tb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		hash := sha256.Sum256([]byte(raw))
		key := &models.TenantAPIKey{TenantID: c.Tenant.ID, KeyHash: hex.EncodeToString(hash[:]), KeyPrefix: raw[:12], Label: "HTTP authority fixture", Scopes: []string{"domains:read"}, OwnerUserID: owner}
		if err := f.Store.CreateAPIKey(context.Background(), key); err != nil {
			t.Fatal(err)
		}
		_, _, _, _, actualOwner, err := f.Store.ResolveAPIKey(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if (owner == nil) != (actualOwner == nil) || (owner != nil && *owner != *actualOwner) {
			t.Fatal("persisted fixture key ownership differs")
		}
		return raw
	}
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"owned-admin-key", issue(&c.Admin.ID), 403}, {"ownerless-key", issue(nil), 403}, {"no-principal", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, op := range []string{"get", "patch", "assignment", "profile-cas", "preview", "delete"} {
				t.Run(op, func(t *testing.T) {
					method, path, raw := r5PermissionAuthorityHTTPWire(t, op, r.UserID, r, p, preview.Members)
					before := r5PermissionAuthorityHTTPState(t, f)
					r5PermissionAuthorityHTTPCall(t, f, tc.token, method, path, raw, uuid.Nil, tc.status)
					if r5PermissionAuthorityHTTPState(t, f) != before {
						t.Fatal("noninteractive HTTP command changed permissions/audits/outbox/revision")
					}
				})
			}
		})
	}
}

func TestR5PermissionAuthorityHTTPMatrixEmptyCASAndNullableIntent(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	old := r5EditorHTTPRead(t, f)
	base := r5EditorHTTPPath(old.UserID)
	token := f.JWT(0, "admin")
	first := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5PermissionAuthorityHTTPCall(t, f, token, "PATCH", base, r5EditorHTTPCommand(t, old.Revision, `{"can_send":false,"domain_access":{"mode":"none","zone_ids":[]},"daily_receive_quota":null}`), uuid.Nil, 200))
	quota := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5PermissionAuthorityHTTPCall(t, f, token, "PATCH", base, r5EditorHTTPCommand(t, first.Revision, `{"daily_send_quota":23}`), uuid.Nil, 200))
	if quota.Overrides == nil || quota.Overrides.CanSend == nil || *quota.Overrides.CanSend || quota.Overrides.DomainAccess.Mode != "none" || quota.Overrides.DailyReceiveQuota != nil || quota.FieldSources["daily_receive_quota"] != "profile" || quota.Effective.CanSend || quota.Effective.AllowsZone(f.Companies[0].Zone.ID) || quota.Effective.DailySendQuota != 23 {
		t.Fatal("HTTP quota-only patch altered nullable inheritance or revoked fields")
	}
	before := r5PermissionAuthorityHTTPState(t, f)
	r5PermissionAuthorityHTTPCall(t, f, token, "PATCH", base, r5EditorHTTPCommand(t, old.Revision, `{}`), uuid.Nil, 409)
	r5PermissionAuthorityHTTPCall(t, f, f.JWT(0, "sender"), "PATCH", base, r5EditorHTTPCommand(t, quota.Revision, `{}`), uuid.Nil, 403)
	unchanged := r5EditorHTTPData[company.PermissionEditorSnapshot](t, r5PermissionAuthorityHTTPCall(t, f, token, "PATCH", base, r5EditorHTTPCommand(t, quota.Revision, `{}`), uuid.Nil, 200))
	if !unchanged.Revision.Equal(quota.Revision) || r5PermissionAuthorityHTTPState(t, f) != before {
		t.Fatal("HTTP empty patch bypassed authority/CAS or consumed revision/audit/outbox")
	}
}

func TestR5PermissionAuthorityHTTPMatrixLockWaitRecheck(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	super := r5PermissionAuthorityHTTPSuper(t, f)
	c := f.Companies[0]
	p := r5PermissionAuthorityHTTPProfile(t, f, super, c.Users["sender"].ID, c.Tenant.ID)
	for _, change := range []string{"demote", "freeze", "session-version"} {
		for _, op := range []string{"patch", "assignment", "profile-cas", "delete"} {
			t.Run(change+"/"+op, func(t *testing.T) {
				u, err := f.Store.GetUser(context.Background(), c.Admin.ID)
				if err != nil {
					t.Fatal(err)
				}
				u.Role = models.RoleAdmin
				u.IsActive = true
				if err := f.Store.UpdateUser(context.Background(), u); err != nil {
					t.Fatal(err)
				}
				f.RefreshJWT(t, 0, "admin")
				r := r5PermissionAuthorityHTTPRevision(t, f, super, c.Users["sender"].ID, c.Tenant.ID, false)
				preview, err := f.Store.GetPermissionProfileDeletionPreview(context.Background(), super, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				method, path, raw := r5PermissionAuthorityHTTPWire(t, op, r.UserID, r, p, preview.Members)
				if op == "patch" {
					raw = r5EditorHTTPCommand(t, r, `{"daily_send_quota":73}`)
				}
				if op == "assignment" {
					raw = r5EditorHTTPJSON(t, map[string]any{"expected_revision": r, "profile_id": nil, "profile_revision": nil, "patch": map[string]any{}})
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				gate, err := f.Pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer gate.Rollback(context.Background())
				if _, err = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, c.Tenant.ID); err != nil {
					t.Fatal(err)
				}
				done := make(chan r5PermissionAuthorityHTTPResult, 1)
				go func() {
					done <- r5PermissionAuthorityHTTPRequest(ctx, f, f.JWT(0, "admin"), method, path, raw, uuid.Nil)
				}()
				tick := time.NewTicker(10 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case result := <-done:
						t.Fatalf("HTTP command completed before SQL wait: status=%d error=%v", result.status, result.err)
					default:
					}
					var pid uint32
					err = f.Pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(gate.Conn().PgConn().PID())).Scan(&pid)
					if err == nil {
						break
					}
					if !errors.Is(err, pgx.ErrNoRows) {
						t.Fatal(err)
					}
					select {
					case <-ctx.Done():
						t.Fatal("no exact HTTP command PostgreSQL wait edge")
					case <-tick.C:
					}
				}
				switch change {
				case "demote":
					u.Role = models.RoleUser
					err = f.Store.UpdateUser(ctx, u)
				case "freeze":
					u.IsActive = false
					err = f.Store.UpdateUser(ctx, u)
				case "session-version":
					err = f.Store.UpdateUserPassword(ctx, u.ID, "test-only-http-authority-new-password")
				}
				if err != nil {
					t.Fatal(err)
				}
				before := r5PermissionAuthorityHTTPState(t, f)
				if err = gate.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				// Authentication already accepted the old JWT before its command
				// waited. Transactional freshness maps all three races to 403,
				// not the pre-auth 401 of a subsequent frozen/old-epoch request.
				select {
				case result := <-done:
					r5PermissionAuthorityHTTPStatus(t, result, 403)
				case <-ctx.Done():
					t.Fatal("post-wait HTTP command failed to terminate")
				}
				if r5PermissionAuthorityHTTPState(t, f) != before {
					t.Fatal("HTTP stale authority produced effects after real wait")
				}
			})
		}
	}
}
