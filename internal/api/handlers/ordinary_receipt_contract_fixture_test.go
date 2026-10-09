package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// TestOrdinaryReceiptOpenAPIWireFixtures exports the real typed projections and
// shipping envelope encoders, NOT a router, authorization, PG or delivery test.
// The separate Python contract test validates these bytes against every ordinary
// response schema. The sole executor may supply a new (nonexistent) output path.
func TestOrdinaryReceiptOpenAPIWireFixtures(t *testing.T) {
	type fixture struct {
		Name   string          `json:"name"`
		Path   string          `json:"path"`
		Method string          `json:"method"`
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	fixtures := []fixture{}
	capture := func(name, path, method string, status int, value any, list bool) {
		t.Helper()
		rr := httptest.NewRecorder()
		if list {
			okList(rr, value, 1, 1, 30)
		} else if status == 201 {
			created(rr, value)
		} else {
			ok(rr, value)
		}
		if rr.Code != status || !json.Valid(rr.Body.Bytes()) {
			t.Fatalf("fixture %s: status=%d invalid JSON=%v", name, rr.Code, !json.Valid(rr.Body.Bytes()))
		}
		fixtures = append(fixtures, fixture{name, path, method, status, append(json.RawMessage{}, rr.Body.Bytes()...)})
	}
	now := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	job := &models.OutboundJob{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), TenantID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), State: models.OutboundRetry, RecipientLedger: true, CreatedAt: now, UpdatedAt: now, Attempts: 2, NextAttemptAt: now.Add(time.Minute)}
	known := company.ProjectOutboundReceipt(job, []string{"accepted", "temporary", "permanent"}, true)
	known.Capabilities = &company.SubmissionCapabilities{ViewContent: true, RetryBlockReason: "state_not_retryable"}
	unknown := company.ProjectOutboundReceipt(job, nil, false)
	unknown.Capabilities = &company.SubmissionCapabilities{RetryBlockReason: "unknown"}
	fallback := company.CommittedOutboundReceiptFallback(job)
	for _, route := range []struct {
		path   string
		method string
		status int
		list   bool
	}{
		{"/api/v1/outbound", "get", 200, true},
		{"/api/v1/outbound/{id}", "get", 200, false},
		{"/api/v1/outbound/{id}/attempts", "get", 200, false},
		{"/api/v1/outbound/{id}/retry", "post", 200, false},
		{"/api/v1/company/submissions", "get", 200, true},
		{"/api/v1/company/submissions/{id}", "get", 200, false},
		{"/api/v1/company/drafts/{id}/submit", "post", 200, false},
		{"/api/v1/company/drafts/{id}/submit", "post", 201, false},
	} {
		for _, v := range []struct {
			name string
			data *company.OutboundReceipt
		}{{"known", known}, {"unknown", unknown}} {
			var value any = v.data
			if route.list {
				value = []*company.OutboundReceipt{v.data}
			}
			capture(v.name, route.path, route.method, route.status, value, route.list)
		}
		if route.path == "/api/v1/outbound/{id}/retry" || route.status == 201 {
			capture("committed-fallback", route.path, route.method, route.status, fallback, false)
		}
	}
	for _, c := range []struct {
		name         string
		completeness string
		bcc          []string
	}{{"known-empty-bcc", "complete", []string{}}, {"known-bcc", "complete", []string{"bcc@fixture.test"}}, {"legacy-unknown-bcc", "legacy_unknown", nil}} {
		v := &company.SubmissionContent{ID: job.ID, Subject: "content-only fixture", MailFrom: "sender@fixture.test", To: []string{"to@fixture.test"}, BCC: c.bcc, RecipientCompleteness: c.completeness, CreatedAt: now}
		capture(c.name, "/api/v1/company/submissions/{id}/content", "get", 200, v, false)
	}
	payload, err := json.MarshalIndent(struct {
		Evidence string    `json:"evidence"`
		Fixtures []fixture `json:"fixtures"`
	}{"typed-projection-and-shipping-envelope-only; not HTTP admission or PG acceptance", fixtures}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("ORDINARY_RECEIPT_WIRE_FIXTURE")
	if path == "" {
		path = filepath.Join(t.TempDir(), "ordinary-receipt-wire.json")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(payload); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("typed wire fixtures: %d; output=%s", len(fixtures), path)
}
