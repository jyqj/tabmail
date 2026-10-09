package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func r5PermissionAuditSeed(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN is required for permission audit regression")
	}
	return seedCompany(t)
}

func r5PermissionAuditProfile(t *testing.T, f *companyFixture) *models.PermissionProfile {
	t.Helper()
	p, err := f.st.CreatePermissionProfileGuarded(t.Context(), f.a, &f.tenant.ID, &models.PermissionProfile{TenantID: &f.tenant.ID, Name: "Permission audit fixture", CanSend: true, DailySendQuota: 23})
	must(t, err)
	return p
}

// Each action is emitted by its actual governed command. Reading audit rows
// inserted directly by a fixture alone cannot prove these production paths.
func TestR5PermissionAuditActualCommandsVisible(t *testing.T) {
	f := r5PermissionAuditSeed(t)
	ctx := t.Context()
	var p *models.PermissionProfile
	for _, tc := range []struct {
		action string
		write  func() uuid.UUID
	}{
		{"permission.profile.create", func() uuid.UUID {
			p = r5PermissionAuditProfile(t, f)
			return p.ID
		}},
		{"permission.profile.update", func() uuid.UUID {
			desired := *p
			desired.Name = "Permission audit updated"
			var err error
			p, err = f.st.UpdatePermissionProfileCAS(ctx, f.a, &desired, p.Revision)
			must(t, err)
			return p.ID
		}},
		{"permission.profile.assign", func() uuid.UUID {
			before, err := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.employee.ID)
			must(t, err)
			_, err = f.st.AssignPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionAssignmentCommand{ExpectedRevision: before.Revision, ProfileID: &p.ID, ProfileRevision: &p.Revision})
			must(t, err)
			return f.employee.ID
		}},
		{"permission.override.patch", func() uuid.UUID {
			before, err := f.st.GetPermissionEditorSnapshot(ctx, f.a, f.employee.ID)
			must(t, err)
			_, err = f.st.PatchPermissionEditor(ctx, f.a, f.employee.ID, company.PermissionEditorCommand{ExpectedRevision: before.Revision, Patch: company.PermissionPatch{DailySendQuota: company.PermissionField[int]{Present: true, Value: 7}}})
			must(t, err)
			return f.employee.ID
		}},
		{"permission.profile.delete", func() uuid.UUID {
			preview, err := f.st.GetPermissionProfileDeletionPreview(ctx, f.a, p.ID)
			must(t, err)
			must(t, f.st.DeletePermissionProfileCAS(ctx, f.a, p.ID, preview.ProfileRevision, preview.Members))
			return p.ID
		}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			resource := tc.write()
			var auditID uuid.UUID
			must(t, f.pool.QueryRow(ctx, `SELECT id FROM audit_log WHERE tenant_id=$1 AND action=$2 AND resource_id=$3 ORDER BY id DESC LIMIT 1`, f.tenant.ID, tc.action, resource).Scan(&auditID))
			rows, total, err := f.st.ListCompanyAudit(ctx, f.a, models.Page{PerPage: 100})
			must(t, err)
			found := false
			for _, row := range rows {
				if row.ID == auditID {
					found = row.Action == tc.action && row.ResourceID != nil && *row.ResourceID == resource && row.Actor == f.a.AuditLabel()
				}
			}
			if !found || total != len(rows) {
				t.Errorf("committed %s audit %s is absent or list count differs: total=%d rows=%d found=%v", tc.action, auditID, total, len(rows), found)
			}
		})
	}
}

func r5PermissionAuditHTTP(t *testing.T, f *companyFixture) http.Handler {
	t.Helper()
	state := middleware.NewAuthState(nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := state.StopContext(ctx); err != nil {
			t.Errorf("audit auth cleanup: %v", err)
		}
	})
	h := handlers.NewCompanyConsoleHandler(f.st, zerolog.Nop())
	return middleware.Auth(f.st, "r5-permission-audit-secret", f.tenant.ID.String(), state)(middleware.RequireAuth(middleware.RequireAdmin(http.HandlerFunc(h.Audit))))
}

func r5PermissionAuditRequest(t *testing.T, h http.Handler, user *models.User, page int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/company/audit?page=%d&per_page=2", page), nil)
	token, err := authn.IssueAccessToken("r5-permission-audit-secret", user)
	must(t, err)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestR5PermissionAuditHTTPProjectionPaginationAndBoundaries(t *testing.T) {
	f := r5PermissionAuditSeed(t)
	ctx := t.Context()
	p := r5PermissionAuditProfile(t, f)
	wanted := map[uuid.UUID]bool{}
	var actualCreate uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT id FROM audit_log WHERE tenant_id=$1 AND action='permission.profile.create' AND resource_id=$2`, f.tenant.ID, p.ID).Scan(&actualCreate))
	wanted[actualCreate] = true
	foreign := &models.Tenant{Name: "Foreign permission audit", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, foreign))
	excluded := map[uuid.UUID]bool{}
	// Existing event categories remain visible; only the five known permission
	// commands gain visibility. Arbitrary permission.* strings remain private.
	for _, tc := range []struct {
		action  string
		foreign bool
		visible bool
	}{
		{"permission.profile.create", false, true},
		{"permission.profile.update", false, true},
		{"permission.profile.delete", false, true},
		{"permission.profile.assign", false, true},
		{"permission.override.patch", false, true},
		{"company.configure", false, true},
		{"employee.invite", false, true},
		{"mailbox.provision", false, true},
		{"template.save", false, true},
		{"domain.configure", false, true},
		{"permission.profile.secret", false, false},
		{"permission.profile.update.extra", false, false},
		{"permission.profile.update", true, false},
	} {
		tenant := f.tenant.ID
		if tc.foreign {
			tenant = foreign.ID
		}
		var id uuid.UUID
		must(t, f.pool.QueryRow(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,'synthetic-audit-actor',$2,'permission_profile',$3,$4) RETURNING id`, tenant, tc.action, p.ID, `{"reason":"approved synthetic review","password_hash":"PRIVATE_PERMISSION_AUDIT","before_revision":{"private":"PRIVATE_PERMISSION_AUDIT"},"text_body":"PRIVATE_PERMISSION_AUDIT"}`).Scan(&id))
		if tc.visible {
			wanted[id] = true
		} else {
			excluded[id] = true
		}
	}
	h := r5PermissionAuditHTTP(t, f)
	t.Run("admin_paginated_safe_projection", func(t *testing.T) {
		seen := map[uuid.UUID]bool{}
		total := -1
		for page := 1; ; page++ {
			if page > 30 {
				t.Fatal("audit pagination did not terminate")
			}
			w := r5PermissionAuditRequest(t, h, f.admin, page)
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(w.Body.String(), "PRIVATE_PERMISSION_AUDIT") {
				t.Fatalf("unsafe audit HTTP response: status=%d body=%s", w.Code, w.Body.String())
			}
			var reply struct {
				Data []map[string]json.RawMessage `json:"data"`
				Meta struct {
					Total, Page int
					PerPage     int `json:"per_page"`
				} `json:"meta"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &reply))
			if total < 0 {
				total = reply.Meta.Total
			}
			if reply.Meta.Total != total || reply.Meta.Page != page || reply.Meta.PerPage != 2 || len(reply.Data) > 2 {
				t.Fatal("unstable audit pagination metadata")
			}
			for _, row := range reply.Data {
				allowed := map[string]bool{"id": true, "actor": true, "action": true, "resource_type": true, "resource_id": true, "reason": true, "created_at": true}
				for key := range row {
					if !allowed[key] {
						t.Errorf("audit field escaped public projection: %s", key)
					}
				}
				var id uuid.UUID
				must(t, json.Unmarshal(row["id"], &id))
				if seen[id] || excluded[id] {
					t.Errorf("duplicate, foreign or unknown-action audit row %s", id)
				}
				seen[id] = true
			}
			if len(reply.Data) == 0 {
				break
			}
		}
		if total != len(seen) {
			t.Errorf("count=%d does not match complete page union=%d", total, len(seen))
		}
		for id := range wanted {
			if !seen[id] {
				t.Errorf("allowed administrative audit %s absent from all pages", id)
			}
		}
	})
	t.Run("employee_cannot_read", func(t *testing.T) {
		if w := r5PermissionAuditRequest(t, h, f.employee, 1); w.Code != http.StatusForbidden {
			t.Fatalf("employee audit status=%d", w.Code)
		}
	})
	t.Run("foreign_actor_cannot_read", func(t *testing.T) {
		actor := f.a
		actor.TenantID = foreign.ID
		rows, total, err := f.st.ListCompanyAudit(ctx, actor, models.Page{})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Fatal("foreign actor read another company's administrative audit")
		}
	})
	t.Run("api_key_cannot_read", func(t *testing.T) {
		actor := authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), OwnerUserID: &f.admin.ID, TenantID: f.tenant.ID, IsAdmin: true}
		if _, _, err := f.st.ListCompanyAudit(ctx, actor, models.Page{}); err == nil {
			t.Fatal("administrator-owned API key read interactive company audit")
		}
	})
	t.Run("current_role_rechecked", func(t *testing.T) {
		_, err := f.pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.admin.ID)
		must(t, err)
		if rows, total, err := f.st.ListCompanyAudit(ctx, f.a, models.Page{}); err == nil || len(rows) != 0 || total != 0 {
			t.Fatal("stale administrator actor retained audit access")
		}
	})
}
