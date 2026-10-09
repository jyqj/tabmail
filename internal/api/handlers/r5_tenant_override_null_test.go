package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

// Exercise the real replacement handler and app service. The persistence spy
// starts with nonempty overrides and records every lookup, write, and audit;
// it does not stand in for PostgreSQL transaction or authorization acceptance.
type tenantOverrideBodyStore struct {
	adminStore
	tenantID      uuid.UUID
	current       models.TenantOverride
	reads, writes int
	audits        []models.AuditEntry
}

func (s *tenantOverrideBodyStore) GetTenant(context.Context, uuid.UUID) (*models.Tenant, error) {
	s.reads++
	return &models.Tenant{ID: s.tenantID}, nil
}

func (s *tenantOverrideBodyStore) UpsertOverride(_ context.Context, value *models.TenantOverride) error {
	s.writes++
	s.current = *value
	return nil
}

func (s *tenantOverrideBodyStore) InsertAudit(_ context.Context, entry *models.AuditEntry) error {
	s.audits = append(s.audits, *entry)
	return nil
}

type tenantOverrideBodyReader struct {
	io.Reader
	chunk, closed int
}

func (r *tenantOverrideBodyReader) Read(p []byte) (int, error) {
	if r.chunk > 0 && len(p) > r.chunk {
		p = p[:r.chunk]
	}
	return r.Reader.Read(p)
}

func (r *tenantOverrideBodyReader) Close() error {
	r.closed++
	return nil
}

func TestR5TenantOverrideJSONDocument(t *testing.T) {
	zero, seven := 0, 7
	for _, tc := range []struct {
		name, body string
		valid      bool
		want       models.TenantOverride
	}{
		{name: "null", body: `null`},
		{name: "whitespace_null", body: " \r\n\tnull\t\n "},
		{name: "empty", body: ""},
		{name: "whitespace", body: " \r\n\t"},
		{name: "number", body: `42`},
		{name: "string", body: `"null"`},
		{name: "boolean", body: `false`},
		{name: "array", body: `[]`},
		{name: "array_of_null", body: `[null]`},
		{name: "unknown_field", body: `{"max_domains":0,"unknown":true}`},
		{name: "malformed", body: `{"max_domains":0`},
		{name: "trailing_null", body: `{"max_domains":0} null`},
		{name: "second_object", body: `{} {}`},
		{name: "empty_object_clears", body: `{}`, valid: true},
		{name: "field_null_inherits", body: `{"max_domains":null,"retention_hours":null}`, valid: true},
		{name: "zero_stays_explicit", body: `{"max_domains":0,"daily_quota":7}`, valid: true, want: models.TenantOverride{MaxDomains: &zero, DailyQuota: &seven}},
		{name: "values_replace", body: `{"max_mailboxes_per_domain":7,"max_messages_per_mailbox":7,"max_message_bytes":7,"retention_hours":7,"rpm_limit":7}`, valid: true, want: models.TenantOverride{MaxMailboxesPerDomain: &seven, MaxMessagesPerMailbox: &seven, MaxMessageBytes: &seven, RetentionHours: &seven, RPMLimit: &seven}},
	} {
		for _, streamed := range []bool{false, true} {
			mode := "known_length"
			if streamed {
				mode = "one_byte_stream"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				tenant := uuid.New()
				old := 19
				st := &tenantOverrideBodyStore{tenantID: tenant, current: models.TenantOverride{
					TenantID: tenant, MaxDomains: &old, MaxMailboxesPerDomain: &old,
					MaxMessagesPerMailbox: &old, MaxMessageBytes: &old,
					RetentionHours: &old, RPMLimit: &old, DailyQuota: &old,
				}}
				before, err := json.Marshal(st.current)
				if err != nil {
					t.Fatal(err)
				}
				h := NewAdminHandler(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
				router := chi.NewRouter()
				router.Patch("/tenants/{id}/override", h.UpdateTenantOverride)
				r := httptest.NewRequest(http.MethodPatch, "/tenants/"+tenant.String()+"/override", strings.NewReader(tc.body))
				reader := &tenantOverrideBodyReader{Reader: r.Body}
				if streamed {
					r.ContentLength = -1
					r.TransferEncoding = []string{"chunked"}
					reader.chunk = 1
				}
				r.Body = reader
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if reader.closed != 1 {
					t.Errorf("body closed %d times; want 1", reader.closed)
				}
				if !tc.valid {
					after, err := json.Marshal(st.current)
					if err != nil {
						t.Fatal(err)
					}
					if w.Code != http.StatusBadRequest || st.reads != 0 || st.writes != 0 || len(st.audits) != 0 || !bytes.Equal(before, after) {
						t.Fatalf("invalid replacement changed state: status=%d reads=%d writes=%d audits=%d unchanged=%v body=%s", w.Code, st.reads, st.writes, len(st.audits), bytes.Equal(before, after), w.Body.String())
					}
					return
				}
				want := tc.want
				want.TenantID = tenant
				if w.Code != http.StatusOK || st.reads != 1 || st.writes != 1 || len(st.audits) != 1 || !reflect.DeepEqual(st.current, want) {
					t.Fatalf("valid replacement changed intent: status=%d reads=%d writes=%d audits=%d got=%+v want=%+v", w.Code, st.reads, st.writes, len(st.audits), st.current, want)
				}
				if st.audits[0].Action != "tenant.override.upsert" || st.audits[0].TenantID == nil || *st.audits[0].TenantID != tenant {
					t.Fatalf("replacement audit changed: %+v", st.audits[0])
				}
			})
		}
	}
}
