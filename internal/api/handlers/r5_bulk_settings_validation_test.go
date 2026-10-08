package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/settings"
	"tabmail/internal/testutil"
)

// Keep the active handler, application service and settings cache. Only the
// persistence boundary is instrumented; these tests make no transaction claim.
type r5BulkSettingsStore struct {
	*testutil.FakeStore
	writes    []string
	audits    int
	lists     int
	failWrite int
	failure   error
}

func (s *r5BulkSettingsStore) UpsertSetting(ctx context.Context, key, value, description string) error {
	s.writes = append(s.writes, key)
	if len(s.writes) == s.failWrite {
		return s.failure
	}
	return s.FakeStore.UpsertSetting(ctx, key, value, description)
}

func (s *r5BulkSettingsStore) InsertAudit(ctx context.Context, entry *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, entry)
}

func (s *r5BulkSettingsStore) ListSettings(ctx context.Context) ([]*models.SystemSetting, error) {
	s.lists++
	return s.FakeStore.ListSettings(ctx)
}

func newR5BulkSettingsFixture(t *testing.T, base *testutil.FakeStore) (*AdminHandler, *r5BulkSettingsStore, *settings.Manager) {
	t.Helper()
	if base == nil {
		base = testutil.NewFakeStore()
	}
	for key, value := range map[string]string{
		models.SettingPublicIPRPM:      "17",
		models.SettingOpenRegistration: "false",
		models.SettingMailboxNaming:    "full",
		"untouched_setting":            "unchanged",
	} {
		if err := base.UpsertSetting(context.Background(), key, value, ""); err != nil {
			t.Fatal(err)
		}
	}
	st := &r5BulkSettingsStore{FakeStore: base}
	manager := settings.NewManager(st, zerolog.Nop())
	h := NewAdminHandler(st, nil, models.SMTPPolicy{}, manager, nil, zerolog.Nop())
	if got := manager.GetInt(context.Background(), models.SettingPublicIPRPM, 999); got != 17 {
		t.Fatalf("cache setup: public RPM=%d", got)
	}
	return h, st, manager
}

func r5BulkSettingsSnapshot(t *testing.T, st *r5BulkSettingsStore) map[string]string {
	t.Helper()
	items, err := st.FakeStore.ListSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(items))
	for _, item := range items {
		values[item.Key] = item.Value
	}
	return values
}

func r5BulkSettingsValidUpdates() map[string]string {
	return map[string]string{
		models.SettingAutoCreateRouteRPM:  "0",
		models.SettingAutoCreateTenantRPM: "-1",
		models.SettingMonitorHistory:      "0012",
		models.SettingPublicIPRPM:         "+20",
		models.SettingFallbackRetentionH:  "24",
		models.SettingStripPlusTag:        "false",
		models.SettingOpenRegistration:    "true",
		models.SettingMailboxNaming:       "local",
		"custom_greeting":                 "  你好\n",
		"custom_empty":                    "",
	}
}

func TestR5BulkSettingsValidationRejectsWholeRequest(t *testing.T) {
	for _, tc := range []struct{ name, key, value, message string }{
		{"empty_key", "", "anything", "key is required"},
		{"route_integer", models.SettingAutoCreateRouteRPM, "20oops", "integer"},
		{"tenant_integer", models.SettingAutoCreateTenantRPM, "1.5", "integer"},
		{"history_integer", models.SettingMonitorHistory, "3 4", "integer"},
		{"public_integer", models.SettingPublicIPRPM, "20 ", "integer"},
		{"retention_integer", models.SettingFallbackRetentionH, "1e3", "integer"},
		{"retention_range", models.SettingFallbackRetentionH, strconv.Itoa(int(^uint(0) >> 1)), "retention"},
		{"strip_plus_bool", models.SettingStripPlusTag, "TRUE", "true or false"},
		{"registration_bool", models.SettingOpenRegistration, "1", "true or false"},
		{"mailbox_naming", models.SettingMailboxNaming, "prefix", "full, local, or domain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Go map order varies. Every one of these mixed requests must be
			// rejected without effects, even when a valid entry is visited first.
			for attempt := 0; attempt < 32; attempt++ {
				h, st, manager := newR5BulkSettingsFixture(t, nil)
				before := r5BulkSettingsSnapshot(t, st)
				updates := r5BulkSettingsValidUpdates()
				for i := 0; i < 16; i++ {
					updates[fmt.Sprintf("custom_extension_%02d", i)] = "valid value"
				}
				updates[tc.key] = tc.value
				err := h.service.BulkUpdateSettings(context.Background(), updates, "synthetic-admin")
				problem, ok := app.As(err)
				if !ok || problem.Kind != app.KindBadRequest || !strings.Contains(problem.Message, tc.message) {
					t.Fatalf("attempt %d: wrong validation error: %v", attempt, err)
				}
				cachedRPM := manager.GetInt(context.Background(), models.SettingPublicIPRPM, 999)
				changed := !reflect.DeepEqual(r5BulkSettingsSnapshot(t, st), before)
				if len(st.writes) != 0 || st.audits != 0 || st.lists != 1 || changed || cachedRPM != 17 {
					t.Fatalf("attempt %d: rejected batch changed state: writes=%v audits=%d cache_reads=%d persisted_changed=%t cached_RPM=%d", attempt, st.writes, st.audits, st.lists, changed, cachedRPM)
				}
			}
		})
	}
}

func TestR5BulkSettingsValidationAcceptsMixedValues(t *testing.T) {
	h, st, manager := newR5BulkSettingsFixture(t, nil)
	updates := r5BulkSettingsValidUpdates()
	if err := h.service.BulkUpdateSettings(context.Background(), updates, "synthetic-admin"); err != nil {
		t.Fatal(err)
	}
	if len(st.writes) != len(updates) || st.audits != len(updates) {
		t.Fatalf("valid batch writes=%v audits=%d want=%d", st.writes, st.audits, len(updates))
	}
	persisted := r5BulkSettingsSnapshot(t, st)
	for key, value := range updates {
		if got, exists := persisted[key]; !exists || got != value || manager.Get(context.Background(), key, "missing") != value {
			t.Errorf("value %q lost: persisted=%q present=%t cached=%q want=%q", key, got, exists, manager.Get(context.Background(), key, "missing"), value)
		}
	}
	if persisted["untouched_setting"] != "unchanged" || manager.GetInt(context.Background(), models.SettingPublicIPRPM, 999) != 20 || manager.GetInt(context.Background(), models.SettingAutoCreateTenantRPM, 999) != -1 || !manager.GetBool(context.Background(), models.SettingOpenRegistration, false) {
		t.Error("valid updates changed unrelated settings or runtime parsing")
	}
	audits, err := st.FakeStore.ListAuditEntries(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, audit := range audits {
		var details map[string]string
		if err := json.Unmarshal(audit.Details, &details); err != nil {
			t.Fatal(err)
		}
		key := details["key"]
		want, exists := updates[key]
		if audit.Action != "setting.update" || audit.ResourceType != "system_setting" || audit.Actor != "synthetic-admin" || !exists || details["value"] != want || seen[key] {
			t.Errorf("wrong or duplicate setting audit: %+v details=%v", audit, details)
		}
		seen[key] = true
	}
	if len(seen) != len(updates) {
		t.Errorf("missing setting audits: got=%d want=%d", len(seen), len(updates))
	}
}

func TestR5BulkSettingsValidationStorageFailure(t *testing.T) {
	h, st, manager := newR5BulkSettingsFixture(t, nil)
	st.failure, st.failWrite = errors.New("synthetic backend write failure"), 2
	updates := map[string]string{"custom_a": "a", "custom_b": "b", "custom_c": "c"}
	err := h.service.BulkUpdateSettings(context.Background(), updates, "synthetic-admin")
	problem, ok := app.As(err)
	if !ok || problem.Kind != app.KindInternal || !errors.Is(err, st.failure) {
		t.Fatalf("write failure lost classification or cause: %v", err)
	}
	// Validation all-or-none does not promise a database transaction: retain
	// the successful write and its audit, stop on the next storage failure.
	if len(st.writes) != 2 || st.audits != 1 {
		t.Fatalf("storage failure boundary changed: writes=%v audits=%d", st.writes, st.audits)
	}
	persisted := r5BulkSettingsSnapshot(t, st)
	if persisted[st.writes[0]] != updates[st.writes[0]] || manager.Get(context.Background(), st.writes[0], "missing") != updates[st.writes[0]] {
		t.Fatal("preceding confirmed write or its cache refresh was lost")
	}
	if _, exists := persisted[st.writes[1]]; exists {
		t.Fatal("failing write unexpectedly persisted")
	}
}

func TestR5BulkSettingsValidationHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, role, body string
		status           int
		failWrite        bool
	}{
		{"invalid_mixed", "super", `{"public_ip_rpm":"20","open_registration":"TRUE","custom_a":"a","custom_b":"b","custom_c":"c","custom_d":"d"}`, http.StatusBadRequest, false},
		{"valid_mixed", "super", `{"public_ip_rpm":"+20","open_registration":"true","custom_greeting":"  你好  "}`, http.StatusOK, false},
		{"empty", "super", `{}`, http.StatusBadRequest, false},
		{"null", "super", `null`, http.StatusBadRequest, false},
		{"non_string", "super", `{"public_ip_rpm":20}`, http.StatusBadRequest, false},
		{"tenant_admin", "tenant", `{"public_ip_rpm":"20"}`, http.StatusForbidden, false},
		{"member", "member", `{"public_ip_rpm":"20"}`, http.StatusForbidden, false},
		{"storage_error", "super", `{"public_ip_rpm":"20"}`, http.StatusInternalServerError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 1
			if tc.name == "invalid_mixed" {
				attempts = 32
			}
			for attempt := 0; attempt < attempts; attempt++ {
				f := newOutboundAccessFixture(t)
				h, st, manager := newR5BulkSettingsFixture(t, f.st)
				before := r5BulkSettingsSnapshot(t, st)
				user := f.platformAdmin
				if tc.role == "tenant" {
					user = f.tenantAdmin
				} else if tc.role == "member" {
					user = f.userA
				}
				if tc.failWrite {
					st.failWrite, st.failure = 1, errors.New("private database diagnostic")
				}
				req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/settings", bytes.NewBufferString(tc.body))
				for key, value := range outboundUserHeaders(t, user) {
					req.Header.Set(key, value)
				}
				state := middleware.NewAuthState(nil)
				w := httptest.NewRecorder()
				middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireSuperAdmin(http.HandlerFunc(h.UpdateSettings))).ServeHTTP(w, req)
				if err := state.StopContext(context.Background()); err != nil {
					t.Fatal(err)
				}
				if w.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
				}
				if tc.status == http.StatusOK {
					var response struct {
						Data []*models.SystemSetting `json:"data"`
					}
					if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					values := map[string]string{}
					for _, item := range response.Data {
						values[item.Key] = item.Value
					}
					if len(st.writes) != 3 || st.audits != 3 || values[models.SettingPublicIPRPM] != "+20" || values[models.SettingOpenRegistration] != "true" || values["custom_greeting"] != "  你好  " || values["untouched_setting"] != "unchanged" || manager.GetInt(context.Background(), models.SettingPublicIPRPM, 999) != 20 {
						t.Fatalf("valid HTTP batch changed: writes=%v audits=%d values=%v", st.writes, st.audits, values)
					}
					continue
				}
				wantWrites := 0
				if tc.failWrite {
					wantWrites = 1
					if strings.Contains(w.Body.String(), st.failure.Error()) {
						t.Fatal("storage failure leaked internal diagnostic")
					}
				}
				cachedRPM := manager.GetInt(context.Background(), models.SettingPublicIPRPM, 999)
				if len(st.writes) != wantWrites || st.audits != 0 || st.lists != 1 || !reflect.DeepEqual(r5BulkSettingsSnapshot(t, st), before) || cachedRPM != 17 {
					t.Fatalf("attempt %d: rejected HTTP batch changed state: writes=%v audits=%d cache_reads=%d cached_RPM=%d", attempt, st.writes, st.audits, st.lists, cachedRPM)
				}
			}
		})
	}
}
