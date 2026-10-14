package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type statsWireStore struct {
	*testutil.FakeStore
	audit []*models.AuditEntry
	err   error
	calls int
}

func (s *statsWireStore) ListAuditEntries(_ context.Context, limit int) ([]*models.AuditEntry, error) {
	s.calls++
	if limit != 12 {
		return nil, errors.New("unexpected recent audit limit")
	}
	return s.audit, s.err
}

// These requests exercise the handler/service JSON projection, not a full
// authenticated router or PostgreSQL acceptance run.
func TestAdminStatsRecentAuditWireProjection(t *testing.T) {
	entry := &models.AuditEntry{ID: uuid.New(), Actor: "audit actor", Action: "audit.action", ResourceType: "tenant", Details: json.RawMessage(`{"key":"value"}`), CreatedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	for _, tc := range []struct {
		name   string
		audit  []*models.AuditEntry
		err    error
		status int
	}{
		{"nil rows", nil, nil, http.StatusOK},
		{"empty rows", []*models.AuditEntry{}, nil, http.StatusOK},
		{"complete rows", []*models.AuditEntry{entry, entry}, nil, http.StatusOK},
		{"nil entry", []*models.AuditEntry{entry, nil}, nil, http.StatusInternalServerError},
		{"storage error", nil, errors.New("PRIVATE audit storage failure"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &statsWireStore{FakeStore: testutil.NewFakeStore(), audit: tc.audit, err: tc.err}
			h := NewAdminHandler(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
			rr := httptest.NewRecorder()
			h.Stats(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil))
			if rr.Code != tc.status || st.calls != 1 {
				t.Fatalf("status=%d calls=%d body=%s", rr.Code, st.calls, rr.Body.String())
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if tc.status != http.StatusOK {
				if _, ok := envelope["data"]; ok {
					t.Fatalf("failed projection returned success data: %s", rr.Body.String())
				}
				if len(envelope["error"]) == 0 || strings.Contains(rr.Body.String(), "PRIVATE") || strings.Contains(rr.Body.String(), "audit actor") {
					t.Fatalf("invalid error projection: %s", rr.Body.String())
				}
				return
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(envelope["data"], &data); err != nil {
				t.Fatal(err)
			}
			var audit []json.RawMessage
			if err := json.Unmarshal(data["recent_audit"], &audit); err != nil {
				t.Fatal(err)
			}
			if audit == nil || len(audit) != len(tc.audit) {
				t.Fatalf("recent_audit is null, missing or truncated: %s", data["recent_audit"])
			}
			for i, raw := range audit {
				want, err := json.Marshal(tc.audit[i])
				if err != nil {
					t.Fatal(err)
				}
				if string(raw) != string(want) {
					t.Fatalf("entry %d lost wire fields: %s != %s", i, raw, want)
				}
			}
		})
	}
}

func TestAdminStatsProjectionRemainsBehindSuperAdminGate(t *testing.T) {
	st := &statsWireStore{FakeStore: testutil.NewFakeStore()}
	h := NewAdminHandler(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
	rr := httptest.NewRecorder()
	middleware.RequireSuperAdmin(http.HandlerFunc(h.Stats)).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil))
	if rr.Code != http.StatusForbidden || st.calls != 0 {
		t.Fatalf("unauthenticated stats reached projection: status=%d calls=%d", rr.Code, st.calls)
	}
}
