package adminapp

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/settings"
)

// The real settings reader and admin service must agree on the full persisted
// integer. A accepted prefix must never turn into a different runtime default.
type settingIntegerStore struct {
	Store
	values map[string]string
	writes int
	audits int
	fail   error
}

func (s *settingIntegerStore) UpsertSetting(_ context.Context, key, value, _ string) error {
	s.writes++
	if s.fail != nil {
		return s.fail
	}
	s.values[key] = value
	return nil
}

func (s *settingIntegerStore) ListSettings(context.Context) ([]*models.SystemSetting, error) {
	var out []*models.SystemSetting
	for key, value := range s.values {
		out = append(out, &models.SystemSetting{Key: key, Value: value})
	}
	return out, nil
}

func (s *settingIntegerStore) InsertAudit(context.Context, *models.AuditEntry) error {
	s.audits++
	return nil
}

func newSettingIntegerService() (*Service, *settingIntegerStore, *settings.Manager) {
	st := &settingIntegerStore{values: make(map[string]string)}
	manager := settings.NewManager(st, zerolog.Nop())
	return NewService(st, nil, models.SMTPPolicy{}, manager, nil, zerolog.Nop()), st, manager
}

func TestR5SettingIntegerRejectsIncompleteValues(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"route suffix", models.SettingAutoCreateRouteRPM, "20oops"},
		{"tenant fraction", models.SettingAutoCreateTenantRPM, "1.5"},
		{"history two numbers", models.SettingMonitorHistory, "3 4"},
		{"retention exponent", models.SettingFallbackRetentionH, "1e3"},
		{"public hex", models.SettingPublicIPRPM, "0x10"},
		{"leading whitespace", models.SettingPublicIPRPM, " 20"},
		{"trailing whitespace", models.SettingPublicIPRPM, "20 "},
		{"trailing newline", models.SettingMonitorHistory, "20\n"},
		{"trailing null", models.SettingFallbackRetentionH, "20\x00"},
		{"empty", models.SettingPublicIPRPM, ""},
		{"noninteger", models.SettingPublicIPRPM, "many"},
		{"overflow", models.SettingPublicIPRPM, "999999999999999999999999999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, st, manager := newSettingIntegerService()
			st.values[tc.key] = "37"
			err := svc.UpdateSetting(context.Background(), tc.key, tc.value, "synthetic-admin")
			classified, ok := app.As(err)
			if !ok || classified.Kind != app.KindBadRequest {
				t.Errorf("invalid integer was not rejected: %v", err)
			}
			if st.writes != 0 || st.audits != 0 || st.values[tc.key] != "37" {
				t.Errorf("rejected input had side effects: writes=%d audits=%d persisted=%q", st.writes, st.audits, st.values[tc.key])
			}
			if got := manager.GetInt(context.Background(), tc.key, 999); got != 37 {
				t.Errorf("runtime integer changed after rejected input: %d", got)
			}
		})
	}
}

func TestR5SettingIntegerPreservesCompleteValues(t *testing.T) {
	for _, value := range []string{"0", "-1", "+12", "0012", "20", strconv.Itoa(int(^uint(0) >> 1)), strconv.Itoa(-int(^uint(0)>>1) - 1)} {
		t.Run(value, func(t *testing.T) {
			svc, st, manager := newSettingIntegerService()
			key := models.SettingPublicIPRPM
			if err := svc.UpdateSetting(context.Background(), key, value, "synthetic-admin"); err != nil {
				t.Fatalf("complete integer rejected: %v", err)
			}
			want, err := strconv.Atoi(value)
			if err != nil {
				t.Fatal(err)
			}
			if st.writes != 1 || st.audits != 1 || st.values[key] != value || manager.GetInt(context.Background(), key, 999) != want {
				t.Fatalf("valid integer changed: writes=%d audits=%d persisted=%q runtime=%d want=%d", st.writes, st.audits, st.values[key], manager.GetInt(context.Background(), key, 999), want)
			}
		})
	}
}

func TestR5SettingIntegerStorageFailurePreservesCause(t *testing.T) {
	svc, st, _ := newSettingIntegerService()
	failure := errors.New("synthetic settings write failure")
	st.fail = failure
	err := svc.UpdateSetting(context.Background(), models.SettingPublicIPRPM, "20", "synthetic-admin")
	classified, ok := app.As(err)
	if !ok || classified.Kind != app.KindInternal || !errors.Is(err, failure) || st.writes != 1 || st.audits != 0 {
		t.Fatalf("write failure lost its boundary: %v writes=%d audits=%d", err, st.writes, st.audits)
	}
}

func TestR5SettingIntegerLeavesOtherSettingKindsUnchanged(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{models.SettingStripPlusTag, "false"},
		{models.SettingMailboxNaming, "local"},
		{"custom_setting", "12 items are allowed here"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			svc, st, _ := newSettingIntegerService()
			if err := svc.UpdateSetting(context.Background(), tc.key, tc.value, "synthetic-admin"); err != nil {
				t.Fatalf("other setting changed: %v", err)
			}
			if st.values[tc.key] != tc.value || st.writes != 1 || st.audits != 1 {
				t.Fatalf("other setting was not persisted: %+v", st)
			}
		})
	}
}
