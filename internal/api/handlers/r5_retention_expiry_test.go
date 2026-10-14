package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/settings"
	"tabmail/internal/testutil"
)

// Exercise shipping Auth, handlers, admin service and settings manager; only
// persistence is synthetic. Rejected input must never reach a write/audit port.
type r5RetentionConfigStore struct {
	*testutil.FakeStore
	writes, audits int
	hours          []int
}

func (s *r5RetentionConfigStore) CreatePlan(ctx context.Context, p *models.Plan) error {
	s.writes++
	s.hours = append(s.hours, p.RetentionHours)
	return s.FakeStore.CreatePlan(ctx, p)
}
func (s *r5RetentionConfigStore) UpdatePlan(ctx context.Context, p *models.Plan) error {
	s.writes++
	s.hours = append(s.hours, p.RetentionHours)
	return s.FakeStore.UpdatePlan(ctx, p)
}
func (s *r5RetentionConfigStore) UpsertOverride(ctx context.Context, p *models.TenantOverride) error {
	s.writes++
	if p.RetentionHours != nil {
		s.hours = append(s.hours, *p.RetentionHours)
	}
	return s.FakeStore.UpsertOverride(ctx, p)
}
func (s *r5RetentionConfigStore) UpsertSetting(ctx context.Context, key, value, desc string) error {
	s.writes++
	if key == models.SettingFallbackRetentionH {
		n, _ := strconv.Atoi(value)
		s.hours = append(s.hours, n)
	}
	return s.FakeStore.UpsertSetting(ctx, key, value, desc)
}
func (s *r5RetentionConfigStore) InsertAudit(ctx context.Context, audit *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, audit)
}

func TestR5RetentionExpiryConfigurationWrites(t *testing.T) {
	for _, endpoint := range []string{"create-plan", "update-plan", "tenant-override", "bulk-setting", "direct-setting"} {
		for _, hours := range []int{24, 3000000, 0, -1, -3000000, math.MaxInt32, math.MinInt32, int(^uint(0) >> 1), -int(^uint(0)>>1) - 1} {
			t.Run(endpoint+"/"+strconv.Itoa(hours), func(t *testing.T) {
				f := newOutboundAccessFixture(t)
				plan := &models.Plan{ID: uuid.New(), Name: "unchanged", RetentionHours: 24}
				f.st.SeedPlan(plan)
				st := &r5RetentionConfigStore{FakeStore: f.st}
				sm := settings.NewManager(st, zerolog.Nop())
				h := NewAdminHandler(st, nil, models.SMTPPolicy{}, sm, nil, zerolog.Nop())
				invalid := hours > 3000000 || hours < -3000000
				if endpoint == "direct-setting" {
					err := h.service.UpdateSetting(context.Background(), models.SettingFallbackRetentionH, strconv.Itoa(hours), "synthetic-admin")
					if invalid {
						problem, ok := app.As(err)
						if !ok || problem.Kind != app.KindBadRequest || !strings.Contains(problem.Message, "retention") {
							t.Errorf("invalid direct setting not classified: %v", err)
						}
					} else if err != nil {
						t.Errorf("valid direct setting rejected: %v", err)
					}
				} else {
					method, path, id := http.MethodPost, "/api/v1/admin/plans", uuid.Nil
					var body any = models.Plan{Name: "retention-plan", RetentionHours: hours}
					call, wantStatus := h.CreatePlan, http.StatusCreated
					switch endpoint {
					case "update-plan":
						method, path, id, call, wantStatus = http.MethodPatch, "/api/v1/admin/plans/"+plan.ID.String(), plan.ID, h.UpdatePlan, http.StatusOK
					case "tenant-override":
						method, path, id, call, wantStatus = http.MethodPatch, "/api/v1/admin/tenants/"+f.tenantID.String(), f.tenantID, h.UpdateTenantOverride, http.StatusOK
						body = map[string]int{"retention_hours": hours}
					case "bulk-setting":
						method, path, call, wantStatus = http.MethodPatch, "/api/v1/admin/settings", h.UpdateSettings, http.StatusOK
						// One invalid setting gives a deterministic no-write oracle;
						// this is not a promise of transactional bulk-setting writes.
						body = map[string]string{models.SettingFallbackRetentionH: strconv.Itoa(hours)}
					}
					payload, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					req := httptest.NewRequest(method, path, bytes.NewReader(payload))
					for key, value := range outboundUserHeaders(t, f.platformAdmin) {
						req.Header.Set(key, value)
					}
					if id != uuid.Nil {
						route := chi.NewRouteContext()
						route.URLParams.Add("id", id.String())
						req = req.WithContext(withRouteContext(req, route))
					}
					w := httptest.NewRecorder()
					state := middleware.NewAuthState(nil)
					middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireSuperAdmin(http.HandlerFunc(call))).ServeHTTP(w, req)
					if err := state.StopContext(context.Background()); err != nil {
						t.Fatal(err)
					}
					if invalid {
						wantStatus = http.StatusBadRequest
					}
					if w.Code != wantStatus {
						t.Errorf("status=%d want=%d body=%s", w.Code, wantStatus, w.Body.String())
					}
					if invalid && !strings.Contains(w.Body.String(), "retention") {
						t.Errorf("error omits invalid retention: %s", w.Body.String())
					}
				}
				if invalid {
					if st.writes != 0 || st.audits != 0 {
						t.Errorf("invalid retention reached writes=%d audits=%d", st.writes, st.audits)
					}
				} else if st.writes != 1 || st.audits != 1 || len(st.hours) != 1 || st.hours[0] != hours {
					t.Errorf("valid finite/zero/negative retention changed: writes=%d audits=%d hours=%v", st.writes, st.audits, st.hours)
				}
			})
		}
	}
}

func TestR5RetentionExpiryOverrideInheritance(t *testing.T) {
	for _, body := range []string{`{}`, `{"retention_hours":null}`} {
		t.Run(body, func(t *testing.T) {
			h, st, _, tenant := seededAdminHandler(t)
			rr := doAdminRequest(t, st, http.MethodPatch, "/api/v1/admin/tenants/"+tenant.String(), json.RawMessage(body), map[string]string{"id": tenant.String()}, h.UpdateTenantOverride)
			if rr.Code != http.StatusOK {
				t.Fatalf("inherited retention rejected: %d %s", rr.Code, rr.Body.String())
			}
			stored, err := st.GetOverride(context.Background(), tenant)
			if err != nil || stored == nil || stored.RetentionHours != nil {
				t.Fatalf("nil inheritance changed: %+v %v", stored, err)
			}
		})
	}
}
