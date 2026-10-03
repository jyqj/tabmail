package submissions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func requireOrdinaryReceiptWhitelist(t *testing.T, v *company.OutboundReceipt) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "tenant_id": true, "state": true, "status": true, "progress": true, "created_at": true, "updated_at": true, "attempt_count": true, "next_retry": true, "delivery_uncertain": true, "capabilities": true}
	for k := range wire {
		if !allowed[k] {
			t.Fatalf("ordinary receipt field is not allowlisted: %s", k)
		}
	}
	for _, s := range []string{"PRIVATE", "private@fixture.test", "secret@fixture.test", "STALE_ORIGINAL_BODY", "CURRENT_SNAPSHOT_BODY"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("ordinary receipt leaked %q", s)
		}
	}
	if v != nil && v.Progress.Completeness == "unknown" && v.Progress.Counts != nil {
		t.Fatal("unknown ledger fabricated known zero/partial counts")
	}
	if v != nil && v.Capabilities != nil {
		switch v.Capabilities.RetryBlockReason {
		case "", CapabilityUnknown, CapabilityDeliveryUncertain, CapabilityStateNotRetryable, CapabilitySenderAuthority:
		default:
			t.Fatal("retry reason is not a safe stable category")
		}
	}
}

func TestR5OutboundReceiptExplicitProjection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		states    []string
		known     bool
		status    string
		uncertain bool
	}{
		{"accepted", []string{"accepted", "accepted"}, true, "accepted", false},
		{"hidden-permanent", []string{"accepted", "permanent"}, true, "partially_accepted", false},
		{"hidden-uncertain", []string{"accepted", "uncertain"}, true, "needs_attention", true},
		{"empty-ledger", nil, true, "needs_attention", false},
		{"unknown-source", []string{"accepted"}, false, "needs_attention", false},
		{"invalid-ledger", []string{"accepted", "PRIVATE-invalid"}, true, "needs_attention", false},
		{"invalid-job-state", []string{"accepted"}, true, "needs_attention", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			job := &models.OutboundJob{ID: uuid.New(), TenantID: uuid.New(), State: models.OutboundSent, RecipientLedger: true, CreatedAt: now, UpdatedAt: now, Attempts: 2, Subject: "PRIVATE-subject", MailFrom: "secret@fixture.test", To: []string{"secret@fixture.test"}, BCC: []string{"private@fixture.test"}, TextBody: "PRIVATE-body", HeadersJSON: json.RawMessage(`{"X-Private":"PRIVATE-header"}`), LastError: "PRIVATE-diagnostic", SMTPResponse: "PRIVATE-smtp", RawMIME: []byte("PRIVATE-eml")}
			if tc.name == "invalid-job-state" {
				job.State = "PRIVATE-invalid-state"
			}
			v := company.ProjectOutboundReceipt(job, tc.states, tc.known)
			requireOrdinaryReceiptWhitelist(t, v)
			if v.Status != tc.status || v.DeliveryUncertain != tc.uncertain {
				t.Fatalf("projection=%+v", v)
			}
			known := tc.known && len(tc.states) > 0 && tc.name != "invalid-ledger" && tc.name != "invalid-job-state"
			if (v.Progress.Completeness == "known") != known {
				t.Fatal("progress completeness invented")
			}
			if known && v.Progress.Counts.Total != len(tc.states) {
				t.Fatal("ledger count was recipient-filtered")
			}
			if job.Subject != "PRIVATE-subject" || len(job.BCC) != 1 {
				t.Fatal("projection mutated source")
			}
		})
	}
}

func TestR5OutboundReceiptPostCommitDisplayFailure(t *testing.T) {
	for _, mode := range []string{"db-error", "revoked", "canceled", "nil", "wrong-id", "wrong-tenant"} {
		t.Run(mode, func(t *testing.T) {
			p, s, a, j := replayReceiptFixture()
			switch mode {
			case "db-error":
				p.err = errors.New("PRIVATE-storage-diagnostic")
			case "revoked":
				p.err = app.Forbidden("PRIVATE-revocation")
			case "canceled":
				p.err = context.Canceled
			case "nil":
				p.result = nil
			case "wrong-id":
				p.result.Job.ID = uuid.New()
			case "wrong-tenant":
				p.result.Job.TenantID = uuid.New()
			}
			v := s.CommittedReceiptView(context.Background(), a, j)
			requireOrdinaryReceiptWhitelist(t, v)
			if v == nil || v.ID != j.ID || v.TenantID != nil || v.AttemptCount != nil || v.CreatedAt != nil || v.UpdatedAt != nil || v.Status != "needs_attention" || v.Progress.Completeness != "unknown" || v.Capabilities == nil || v.Capabilities.ViewContent || v.Capabilities.Retry || v.Capabilities.RetryBlockReason != CapabilityUnknown {
				t.Fatalf("post-commit fallback falsely claimed authority/evidence: %+v", v)
			}
			if p.calls != 1 || p.scope != "send:write" {
				t.Fatal("display check changed command scope or retried commit")
			}
		})
	}
}

func TestR5OutboundReceiptCommittedAndAttemptsUseSafeSnapshot(t *testing.T) {
	p, s, a, j := replayReceiptFixture()
	for _, mode := range []string{"committed", "detail", "attempts", "recipients"} {
		t.Run(mode, func(t *testing.T) {
			var v *company.OutboundReceipt
			var err error
			switch mode {
			case "committed":
				v = s.CommittedReceiptView(context.Background(), a, j)
			case "attempts":
				v, err = s.OutboundAttemptViews(context.Background(), &models.Tenant{ID: a.TenantID}, a, j.ID)
			default:
				v, err = s.OutboundReceiptView(context.Background(), &models.Tenant{ID: a.TenantID}, a, j.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			requireOrdinaryReceiptWhitelist(t, v)
			if v.Status != "partially_accepted" || v.Progress.Counts == nil || v.Progress.Counts.Permanent != 1 || v.Capabilities == nil || !v.Capabilities.ViewContent {
				t.Fatal("safe projection weakened true content hint or complete ledger")
			}
		})
	}
	if p.result.Job.TextBody != "CURRENT_SNAPSHOT_BODY" {
		t.Fatal("wire projection mutated source")
	}
}
