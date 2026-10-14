package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
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

func r5VisibilityHTTPProfile(t *testing.T, f *testpg.R5HTTPFixture, tenant *uuid.UUID, name string) *models.PermissionProfile {
	t.Helper()
	p := &models.PermissionProfile{TenantID: tenant, Name: name, Description: "http-private-" + uuid.NewString(), CanSend: true, DailySendQuota: 19, CanCreateAPIKeys: true}
	if err := f.Store.CreatePermissionProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func r5VisibilityHTTPList(t *testing.T, f *testpg.R5HTTPFixture, token, path string, selected uuid.UUID, want int) ([]*models.PermissionProfile, []byte) {
	t.Helper()
	before := r5PermissionAuthorityHTTPState(t, f)
	body := r5PermissionAuthorityHTTPCall(t, f, token, http.MethodGet, path, "", selected, want)
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if _, ok := envelope["meta"]; ok {
		t.Fatal("unpaginated list exposed page/total metadata")
	}
	if _, ok := envelope["total"]; ok {
		t.Fatal("unpaginated list exposed total")
	}
	if want >= 400 {
		if raw, ok := envelope["data"]; ok && string(raw) != "null" {
			t.Fatal("denied profile HTTP returned data")
		}
		if r5PermissionAuthorityHTTPState(t, f) != before {
			t.Fatal("denied list changed durable effects")
		}
		return nil, body
	}
	var items []*models.PermissionProfile
	if err := json.Unmarshal(envelope["data"], &items); err != nil {
		t.Fatal(err)
	}
	if items == nil {
		t.Fatal("successful list did not return an array")
	}
	for i := 1; i < len(items); i++ {
		if items[i-1].Name == items[i].Name && items[i-1].ID.String() > items[i].ID.String() {
			t.Fatal("equal-name profile order lacks stable ID tie-breaker")
		}
	}
	if r5PermissionAuthorityHTTPState(t, f) != before {
		t.Fatal("profile list produced effects")
	}
	return items, body
}

func r5VisibilityHTTPExpect(t *testing.T, items []*models.PermissionProfile, all []*models.PermissionProfile, tenant *uuid.UUID) {
	t.Helper()
	expected := []*models.PermissionProfile{}
	for _, p := range all {
		if tenant == nil || p.TenantID == nil || *p.TenantID == *tenant {
			expected = append(expected, p)
		}
	}
	// Preserve PostgreSQL name collation from the passive all-profile observer.
	for first := 0; first < len(expected); {
		last := first + 1
		for last < len(expected) && expected[last].Name == expected[first].Name {
			last++
		}
		group := expected[first:last]
		sort.Slice(group, func(i, j int) bool { return group[i].ID.String() < group[j].ID.String() })
		first = last
	}
	// Compare the actual public wire value: private raw-field normalization stays
	// with the existing serializer, not a test-local replacement list policy.
	gotJSON, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, expectedValue any
	if err = json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, expectedValue) {
		t.Fatal("HTTP profiles/fields differ from current visible scope")
	}
}

func TestR5PermissionProfileVisibilityHTTPScope(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	super := r5PermissionAuthorityHTTPSuper(t, f)
	a, b := f.Companies[0], f.Companies[1]
	name := "HTTP visibility " + uuid.NewString()
	global := r5VisibilityHTTPProfile(t, f, nil, name)
	r5VisibilityHTTPProfile(t, f, &a.Tenant.ID, name)
	foreign := r5VisibilityHTTPProfile(t, f, &b.Tenant.ID, name)
	selected := super
	selected.TenantID = b.Tenant.ID
	snap, err := f.Store.GetPermissionEditorSnapshot(context.Background(), selected, b.Users["sender"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Store.AssignPermissionEditor(context.Background(), selected, b.Users["sender"].ID, company.PermissionAssignmentCommand{ExpectedRevision: snap.Revision, ProfileID: &global.ID, ProfileRevision: &global.Revision}); err != nil {
		t.Fatal(err)
	}
	all, err := f.Store.ListPermissionProfiles(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/permissions"
	for _, tc := range []struct {
		name, token string
		selected    uuid.UUID
		visible     *uuid.UUID
	}{
		{"super-default", f.JWT(0, "reader"), uuid.Nil, nil},
		{"super-selected-a", f.JWT(0, "reader"), a.Tenant.ID, nil},
		{"super-selected-b", f.JWT(0, "reader"), b.Tenant.ID, nil},
		{"super-switch-back-a", f.JWT(0, "reader"), a.Tenant.ID, nil},
		{"tenant-admin-a", f.JWT(0, "admin"), uuid.Nil, &a.Tenant.ID},
		{"tenant-admin-b", f.JWT(1, "admin"), uuid.Nil, &b.Tenant.ID},
		// Existing Auth only honors selected-company headers for current super.
		{"tenant-admin-foreign-header", f.JWT(0, "admin"), b.Tenant.ID, &a.Tenant.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, _ := r5VisibilityHTTPList(t, f, tc.token, path, tc.selected, 200)
			r5VisibilityHTTPExpect(t, items, all, tc.visible)
		})
	}
	items, before := r5VisibilityHTTPList(t, f, f.JWT(0, "admin"), path, uuid.Nil, 200)
	r5VisibilityHTTPExpect(t, items, all, &a.Tenant.ID)
	for _, secret := range []string{foreign.ID.String(), foreign.Description, b.Tenant.ID.String()} {
		if bytes.Contains(before, []byte(secret)) {
			t.Fatal("admin list exposed foreign profile identity/description/tenant")
		}
	}
	// Foreign rows preceding visible rows cannot move a page or change any
	// visible count/fields. This API is currently unpaginated; query strings
	// neither create a new pagination contract nor broaden qualification.
	for i := 0; i < 3; i++ {
		r5VisibilityHTTPProfile(t, f, &b.Tenant.ID, "000-foreign-"+uuid.NewString())
	}
	for _, query := range []string{"", "?page=1&per_page=1", "?offset=0&limit=1", "?tenant_id=" + b.Tenant.ID.String()} {
		_, after := r5VisibilityHTTPList(t, f, f.JWT(0, "admin"), path+query, uuid.Nil, 200)
		if !bytes.Equal(before, after) {
			t.Fatal("foreign profiles or unrecognized paging/scope parameters affected visible list")
		}
	}
	for _, tc := range []struct {
		name, token string
		selected    uuid.UUID
		status      int
	}{
		{"reader", f.JWT(0, "sender"), uuid.Nil, 403},
		{"global-reference-reader", f.JWT(1, "sender"), uuid.Nil, 403},
		{"frozen", f.JWT(0, "frozen"), uuid.Nil, 401},
		{"no-principal", "", uuid.Nil, 401},
		{"missing-selected", f.JWT(0, "reader"), uuid.New(), 404},
	} {
		t.Run(tc.name, func(t *testing.T) { r5VisibilityHTTPList(t, f, tc.token, path, tc.selected, tc.status) })
	}
	issue := func(owner *uuid.UUID) string {
		raw := "tb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		sum := sha256.Sum256([]byte(raw))
		key := &models.TenantAPIKey{TenantID: a.Tenant.ID, KeyHash: hex.EncodeToString(sum[:]), KeyPrefix: raw[:12], Label: "profile list fixture", Scopes: []string{"domains:read"}, OwnerUserID: owner}
		if err := f.Store.CreateAPIKey(context.Background(), key); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, tc := range []struct{ name, token string }{{"owned-admin-key", issue(&a.Admin.ID)}, {"owned-super-key", issue(&super.ID)}, {"ownerless-key", issue(nil)}} {
		t.Run(tc.name, func(t *testing.T) { r5VisibilityHTTPList(t, f, tc.token, path, b.Tenant.ID, 403) })
	}
}

func TestR5PermissionProfileVisibilityHTTPAuthorityChanges(t *testing.T) {
	for _, change := range []string{"super-to-admin", "admin-to-reader", "freeze-reactivate", "password-epoch"} {
		t.Run(change, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			super := r5PermissionAuthorityHTTPSuper(t, f)
			a, b := f.Companies[0], f.Companies[1]
			path := "/api/v1/admin/permissions"
			role, jwt := models.RoleUser, "admin"
			target := a.Admin.ID
			if change == "super-to-admin" {
				// Separate stored platform actor governs demotion, not an invented header.
				root := r5PermissionAuthorityHTTPUser(t, f, a.Tenant.ID, models.RoleSuperAdmin)
				v := root.SessionVersion
				super = authz.Actor{Type: authz.PrincipalUser, ID: root.ID, TenantID: a.Tenant.ID, Role: root.Role, IsSuperAdmin: true, SessionVersion: &v}
				target = a.Users["reader"].ID
				jwt = "reader"
				role = models.RoleAdmin
			}
			token := f.JWT(0, jwt)
			r5VisibilityHTTPList(t, f, token, path, uuid.Nil, 200)
			switch change {
			case "super-to-admin", "admin-to-reader":
				if _, err := f.Store.UpdateUserGuarded(context.Background(), super, a.Tenant.ID, target, models.UserAdminPatch{Role: &role}); err != nil {
					t.Fatal(err)
				}
			case "freeze-reactivate":
				inactive := false
				if _, err := f.Store.UpdateUserGuarded(context.Background(), super, a.Tenant.ID, target, models.UserAdminPatch{IsActive: &inactive}); err != nil {
					t.Fatal(err)
				}
				r5VisibilityHTTPList(t, f, token, path, uuid.Nil, 401)
				active := true
				if _, err := f.Store.UpdateUserGuarded(context.Background(), super, a.Tenant.ID, target, models.UserAdminPatch{IsActive: &active}); err != nil {
					t.Fatal(err)
				}
			case "password-epoch":
				if err := f.Store.UpdateUserPassword(context.Background(), target, "profile-list-test-password"); err != nil {
					t.Fatal(err)
				}
			}
			// The actual old JWT stays stale even if the same account is reactivated.
			r5VisibilityHTTPList(t, f, token, path, b.Tenant.ID, 401)
			f.RefreshJWT(t, 0, jwt)
			if change == "admin-to-reader" {
				r5VisibilityHTTPList(t, f, f.JWT(0, jwt), path, uuid.Nil, 403)
				return
			}
			all, err := f.Store.ListPermissionProfiles(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			items, _ := r5VisibilityHTTPList(t, f, f.JWT(0, jwt), path, b.Tenant.ID, 200)
			// A demoted current admin cannot select B; its header is ignored by Auth.
			r5VisibilityHTTPExpect(t, items, all, &a.Tenant.ID)
		})
	}
}

func r5VisibilityHTTPWaitPID(t *testing.T, f *testpg.R5HTTPFixture, ctx context.Context, blocker uint32, early func()) uint32 {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		early()
		var pid uint32
		err := f.Pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) LIMIT 1`, int32(blocker)).Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual HTTP actor-read PostgreSQL wait missing")
		case <-ticker.C:
		}
	}
}

func TestR5PermissionProfileVisibilityHTTPLockWait(t *testing.T) {
	for _, change := range []string{"demote", "freeze"} {
		t.Run(change, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			super := r5PermissionAuthorityHTTPSuper(t, f)
			a := f.Companies[0]
			token := f.JWT(0, "admin")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			gate, err := f.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Rollback(context.Background())
			if _, err = gate.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`); err != nil {
				t.Fatal(err)
			}
			writerDone := make(chan error, 1)
			go func() {
				patch := models.UserAdminPatch{}
				if change == "demote" {
					r := models.RoleUser
					patch.Role = &r
				} else {
					v := false
					patch.IsActive = &v
				}
				_, err := f.Store.UpdateUserGuarded(ctx, super, a.Tenant.ID, a.Admin.ID, patch)
				writerDone <- err
			}()
			writerPID := r5VisibilityHTTPWaitPID(t, f, ctx, gate.Conn().PgConn().PID(), func() {
				select {
				case err := <-writerDone:
					t.Fatalf("production update escaped audit gate: %v", err)
				default:
				}
			})
			readDone := make(chan r5PermissionAuthorityHTTPResult, 1)
			go func() {
				readDone <- r5PermissionAuthorityHTTPRequest(ctx, f, token, http.MethodGet, "/api/v1/admin/permissions", "", uuid.Nil)
			}()
			r5VisibilityHTTPWaitPID(t, f, ctx, writerPID, func() {
				select {
				case result := <-readDone:
					t.Fatalf("HTTP returned before actor fence: status=%d err=%v", result.status, result.err)
				default:
				}
			})
			if err = gate.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-writerDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("production update did not finish")
			}
			select {
			case result := <-readDone:
				body := r5PermissionAuthorityHTTPStatus(t, result, 403)
				var envelope map[string]json.RawMessage
				if err = json.Unmarshal(body, &envelope); err != nil {
					t.Fatal(err)
				}
				if _, ok := envelope["data"]; ok {
					t.Fatal("post-wait stale HTTP list returned data")
				}
			case <-ctx.Done():
				t.Fatal("waited HTTP list did not finish")
			}
			// Subsequent authentication sees the new row, not the already-accepted race.
			status := 401
			if change == "demote" {
				f.RefreshJWT(t, 0, "admin")
				token = f.JWT(0, "admin")
				status = 403
			}
			r5VisibilityHTTPList(t, f, token, "/api/v1/admin/permissions", uuid.Nil, status)
			r5VisibilityHTTPList(t, f, f.JWT(0, "reader"), "/api/v1/admin/permissions", a.Tenant.ID, 200)
		})
	}
}
