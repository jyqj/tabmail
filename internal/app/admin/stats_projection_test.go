package adminapp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

type statsProjectionStore struct {
	Store
	audit []*models.AuditEntry
	err   error
	limit int
}

func (s *statsProjectionStore) ListTenants(context.Context) ([]*models.Tenant, error) {
	return []*models.Tenant{{}, {}}, nil
}
func (s *statsProjectionStore) ListPlans(context.Context) ([]*models.Plan, error) {
	return []*models.Plan{{}}, nil
}
func (s *statsProjectionStore) CountAllZones(context.Context) (int, error)     { return 3, nil }
func (s *statsProjectionStore) CountAllMailboxes(context.Context) (int, error) { return 4, nil }
func (s *statsProjectionStore) CountAllMessages(context.Context) (int, error)  { return 5, nil }
func (s *statsProjectionStore) ListAuditEntries(_ context.Context, limit int) ([]*models.AuditEntry, error) {
	s.limit = limit
	return s.audit, s.err
}

func TestStatsRecentAuditProjectionEmptyArrays(t *testing.T) {
	for _, audit := range [][]*models.AuditEntry{nil, {}} {
		st := &statsProjectionStore{audit: audit}
		got, err := NewService(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop()).Stats(context.Background())
		if err != nil || got == nil {
			t.Fatalf("Stats: %v, %v", got, err)
		}
		if got.RecentAudit == nil || len(got.RecentAudit) != 0 {
			t.Fatalf("recent audit must be a non-nil empty slice: %#v", got.RecentAudit)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if string(wire["recent_audit"]) != "[]" {
			t.Fatalf("recent_audit = %s", wire["recent_audit"])
		}
		if got.TenantsCount != 2 || got.PlansCount != 1 || got.DomainsCount != 3 || got.MailboxesCount != 4 || got.MessagesCount != 5 || st.limit != 12 {
			t.Fatalf("unrelated counts or audit limit changed: %+v, limit %d", got, st.limit)
		}
	}
}

func TestStatsRecentAuditProjectionCopiesCompleteValues(t *testing.T) {
	tenant, resource := uuid.New(), uuid.New()
	first := &models.AuditEntry{ID: uuid.New(), TenantID: &tenant, Actor: "first", Action: "first.action", ResourceType: "mailbox", ResourceID: &resource, Details: json.RawMessage(`{"reason":"preserve"}`), CreatedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	second := &models.AuditEntry{ID: uuid.New(), Actor: "second", Action: "second.action", ResourceType: "tenant", CreatedAt: first.CreatedAt.Add(-time.Second)}
	want := []models.AuditEntry{*first, *second}
	st := &statsProjectionStore{audit: []*models.AuditEntry{first, second}}
	got, err := NewService(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop()).Stats(context.Background())
	if err != nil || got == nil {
		t.Fatalf("Stats: %v, %v", got, err)
	}
	if !reflect.DeepEqual(got.RecentAudit, want) {
		t.Fatalf("audit fields/order lost: got %#v want %#v", got.RecentAudit, want)
	}
	first.Actor = "storage struct changed"
	if got.RecentAudit[0].Actor != "first" {
		t.Fatal("public entry still aliases the storage struct")
	}
}

func TestStatsRecentAuditProjectionRejectsMissingEntriesAndStoreErrors(t *testing.T) {
	storeErr := errors.New("audit storage failed")
	for _, tc := range []struct {
		name  string
		audit []*models.AuditEntry
		err   error
	}{
		{"nil first", []*models.AuditEntry{nil, {Actor: "must not be returned"}}, nil},
		{"nil last", []*models.AuditEntry{{Actor: "must not be partially returned"}, nil}, nil},
		{"store error", []*models.AuditEntry{{Actor: "must not be returned"}}, storeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &statsProjectionStore{audit: tc.audit, err: tc.err}
			got, err := NewService(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop()).Stats(context.Background())
			appErr, ok := app.As(err)
			if got != nil || !ok || appErr.Kind != app.KindInternal {
				t.Fatalf("expected internal error without payload, got %+v / %v", got, err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("store error cause lost: %v", err)
			}
		})
	}
}
