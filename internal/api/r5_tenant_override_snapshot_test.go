package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/api"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

var continueOverrideFields = []string{"max_domains", "max_mailboxes_per_domain", "max_messages_per_mailbox", "max_message_bytes", "retention_hours", "rpm_limit", "daily_quota"}

type continueOverrideReadStore struct {
	*testutil.FakeStore
	target                uuid.UUID
	override              *models.TenantOverride
	tenantErr, readErr    error
	reads, writes, audits int
}

func (s *continueOverrideReadStore) GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	if id == s.target && s.tenantErr != nil {
		return nil, s.tenantErr
	}
	return s.FakeStore.GetTenant(ctx, id)
}
func (s *continueOverrideReadStore) GetOverride(_ context.Context, id uuid.UUID) (*models.TenantOverride, error) {
	s.reads++
	if id != s.target {
		return nil, errors.New("unexpected override target")
	}
	return s.override, s.readErr
}
func (s *continueOverrideReadStore) UpsertOverride(ctx context.Context, v *models.TenantOverride) error {
	s.writes++
	return s.FakeStore.UpsertOverride(ctx, v)
}
func (s *continueOverrideReadStore) InsertAudit(ctx context.Context, v *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, v)
}
func continueOverridePointer(v int) *int { return &v }

// The shipping router/Auth/middleware/handler/service run together. Only store
// ports are synthetic; no live PostgreSQL or arbitrary concurrent-write claim.
func TestContinueTenantOverrideSnapshot(t *testing.T) {
	for _, name := range []string{"no-row", "all-null", "mixed", "signed-boundaries", "superadmin-selected-other", "unknown-tenant", "malformed-id", "tenant-error", "override-error", "wrong-tenant-row", "admin", "user", "api-key", "anonymous", "inactive", "stale-session", "demoted-superadmin", "invalid-bearer"} {
		t.Run(name, func(t *testing.T) {
			fake, obj, target := seededStores(t)
			st := &continueOverrideReadStore{FakeStore: fake, target: target}
			user := seedUserForTest(t, fake, uuid.MustParse(publicTenantID), models.RoleSuperAdmin)
			token := issueAccessTokenForExistingUser(t, user)
			wantStatus, wantReads := http.StatusOK, 1
			path := "/api/v1/admin/tenants/" + target.String()
			headers := map[string]string{"Authorization": "Bearer " + token}
			want := map[string]any{"tenant_id": target.String()}
			for _, field := range continueOverrideFields {
				want[field] = nil
			}
			switch name {
			case "all-null":
				st.override = &models.TenantOverride{ID: uuid.New(), TenantID: target, UpdatedAt: time.Now()}
			case "mixed", "superadmin-selected-other":
				st.override = &models.TenantOverride{ID: uuid.New(), TenantID: target, MaxDomains: continueOverridePointer(0), DailyQuota: continueOverridePointer(-1), MaxMessageBytes: continueOverridePointer(4242), UpdatedAt: time.Now()}
				want["max_domains"], want["daily_quota"], want["max_message_bytes"] = float64(0), float64(-1), float64(4242)
				if name == "superadmin-selected-other" {
					other := uuid.New()
					fake.SeedTenant(&models.Tenant{ID: other, PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000010"), Name: "selected-other"})
					headers["X-Tenant-ID"] = other.String()
				}
			case "signed-boundaries":
				st.override = &models.TenantOverride{TenantID: target, MaxDomains: continueOverridePointer(math.MinInt32), MaxMailboxesPerDomain: continueOverridePointer(math.MaxInt32), RetentionHours: continueOverridePointer(3000000)}
				want["max_domains"], want["max_mailboxes_per_domain"], want["retention_hours"] = float64(math.MinInt32), float64(math.MaxInt32), float64(3000000)
			case "unknown-tenant":
				path = "/api/v1/admin/tenants/" + uuid.NewString()
				wantStatus, wantReads = 404, 0
			case "malformed-id":
				path = "/api/v1/admin/tenants/not-a-uuid"
				wantStatus, wantReads = 400, 0
			case "tenant-error":
				st.tenantErr = errors.New("private-synthetic-tenant-failure")
				wantStatus, wantReads = 500, 0
			case "override-error":
				st.readErr = errors.New("private-synthetic-override-failure")
				wantStatus = 500
			case "wrong-tenant-row":
				st.override = &models.TenantOverride{TenantID: uuid.New(), MaxDomains: continueOverridePointer(9123)}
				wantStatus = 500
			case "admin", "user", "demoted-superadmin":
				user.Role = models.RoleAdmin
				if name == "user" {
					user.Role = models.RoleUser
				}
				if err := fake.UpdateUser(context.Background(), user); err != nil {
					t.Fatal(err)
				}
				if name != "demoted-superadmin" {
					headers["Authorization"] = "Bearer " + issueAccessTokenForExistingUser(t, user)
				}
				wantStatus, wantReads = 403, 0
			case "inactive":
				user.IsActive = false
				if err := fake.UpdateUser(context.Background(), user); err != nil {
					t.Fatal(err)
				}
				wantStatus, wantReads = 401, 0
			case "stale-session":
				user.SessionVersion++
				if err := fake.UpdateUser(context.Background(), user); err != nil {
					t.Fatal(err)
				}
				wantStatus, wantReads = 401, 0
			case "api-key":
				tenant, _ := fake.GetTenant(context.Background(), target)
				fake.RegisterAPIKey("synthetic-override-key", tenant, []string{"*"})
				headers = map[string]string{"X-API-Key": "synthetic-override-key"}
				wantStatus, wantReads = 403, 0
			case "anonymous":
				headers = nil
				wantStatus, wantReads = 401, 0
			case "invalid-bearer":
				headers["Authorization"] = "Bearer malformed"
				wantStatus, wantReads = 401, 0
			}
			router := testRouter(st, obj, nil).(*api.Router)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if e := router.StopContext(ctx); e != nil {
					t.Error(e)
				}
			}()
			r := httptest.NewRequest(http.MethodGet, path, nil)
			for k, v := range headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != wantStatus {
				t.Errorf("status=%d want=%d body=%s", w.Code, wantStatus, w.Body.String())
			}
			if st.reads != wantReads || st.writes != 0 || st.audits != 0 {
				t.Errorf("ports reads=%d want=%d writes=%d audits=%d", st.reads, wantReads, st.writes, st.audits)
			}
			if strings.Contains(w.Body.String(), "private-synthetic") || strings.Contains(w.Body.String(), "9123") {
				t.Error("private storage result leaked")
			}
			if w.Code == 200 {
				var response struct {
					Data map[string]any `json:"data"`
				}
				if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
					t.Fatal(e)
				}
				if !reflect.DeepEqual(response.Data, want) {
					t.Errorf("raw snapshot=%#v want=%#v; missing null or unexpected storage/effective fields", response.Data, want)
				}
				if w.Header().Get("Cache-Control") != "private, no-store" {
					t.Errorf("private snapshot cached: %q", w.Header().Get("Cache-Control"))
				}
			}
		})
	}
}
func TestContinueTenantOverrideExistingContracts(t *testing.T) {
	for _, operation := range []string{"effective-config", "replacement-empty", "replacement-explicit-null", "replacement-zero", "replacement-null-document"} {
		t.Run(operation, func(t *testing.T) {
			fake, obj, target := seededStores(t)
			st := &continueOverrideReadStore{FakeStore: fake, target: target}
			if e := fake.UpsertOverride(context.Background(), &models.TenantOverride{TenantID: target, DailyQuota: continueOverridePointer(23)}); e != nil {
				t.Fatal(e)
			}
			router := testRouter(st, obj, nil).(*api.Router)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if e := router.StopContext(ctx); e != nil {
					t.Error(e)
				}
			}()
			method, path, body, wantStatus := http.MethodPatch, "/api/v1/admin/tenants/"+target.String(), `{}`, 200
			if operation == "effective-config" {
				method = http.MethodGet
				path += "/config"
				body = ""
			}
			if operation == "replacement-explicit-null" {
				body = `{"daily_quota":null}`
			}
			if operation == "replacement-zero" {
				body = `{"daily_quota":0}`
			}
			if operation == "replacement-null-document" {
				body = `null`
				wantStatus = 400
			}
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			setAdminAuth(t, fake, r, uuid.Nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, wantStatus, w.Body.String())
			}
			if operation == "effective-config" {
				var v struct {
					Data map[string]any `json:"data"`
				}
				if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
					t.Fatal(e)
				}
				if len(v.Data) != 7 || v.Data["daily_quota"] != float64(23) || v.Data["max_domains"] != float64(10) {
					t.Fatalf("effective contract changed: %v", v.Data)
				}
			} else {
				got, e := fake.GetOverride(context.Background(), target)
				if e != nil || got == nil {
					t.Fatal("missing stored override", e)
				}
				var want *int
				if operation == "replacement-zero" {
					want = continueOverridePointer(0)
				}
				if operation == "replacement-null-document" {
					want = continueOverridePointer(23)
				}
				if !reflect.DeepEqual(got.DailyQuota, want) {
					t.Errorf("replacement inheritance changed: got%v want%v", got.DailyQuota, want)
				}
			}
		})
	}
}
