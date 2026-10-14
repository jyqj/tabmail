package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func TestR5AdminDeliveryMetricsAggregateWire(t *testing.T) {
	// Give ten admitted real keys positive delivery volume. The rejection-only
	// overflow must still be visible in the actual admin service's Top(10).
	for i := 0; i < 10; i++ {
		metrics.SMTPDeliverySucceeded(fmt.Sprintf("r5-wire-success-tenant-%d", i), fmt.Sprintf("r5-wire-success-%d@example.test", i))
	}
	before := metrics.Snapshot(false, 0).SMTP.RecipientsRejected
	for i := 0; i < 8192; i++ {
		metrics.SMTPRecipientRejected()
		metrics.TenantRecipientRejected(fmt.Sprintf("r5-wire-rejected-tenant-%d", i))
		metrics.MailboxRecipientRejected(fmt.Sprintf("r5-wire-rejected-%d@example.test", i))
	}
	h := NewAdminHandler(testutil.NewFakeStore(), nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
	rr := httptest.NewRecorder()
	h.Stats(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/stats", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		Data struct {
			Metrics models.MetricsSnapshot `json:"metrics"`
			Tenant  []map[string]any       `json:"tenant_delivery"`
			Mailbox []map[string]any       `json:"mailbox_delivery"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, dimension := range []struct {
		name string
		rows []map[string]any
	}{
		{"tenant", response.Data.Tenant},
		{"mailbox", response.Data.Mailbox},
	} {
		t.Run(dimension.name+"_top_contains_typed_overflow", func(t *testing.T) {
			if len(dimension.rows) != 10 {
				t.Fatalf("API returned %d rows, want at most 10 including aggregate", len(dimension.rows))
			}
			last := dimension.rows[len(dimension.rows)-1]
			if last["aggregate"] != true || last["key"] != "" {
				t.Fatalf("API did not project a distinct aggregate row: %v", last)
			}
			if rejected, ok := last["rejected"].(float64); !ok || rejected < 4096 {
				t.Fatalf("API lost overflow rejections: %v", last)
			}
			for _, row := range dimension.rows[:len(dimension.rows)-1] {
				if row["key"] == "" || row["aggregate"] == true {
					t.Fatalf("ordinary key mislabeled as aggregate: %v", row)
				}
				if _, present := row["aggregate"]; present {
					t.Fatalf("optional false aggregate field changed existing ordinary wire rows: %v", row)
				}
			}
		})
	}
	t.Run("stats_global_totals_keep_the_full_population", func(t *testing.T) {
		if got := response.Data.Metrics.SMTP.RecipientsRejected - before; got != 8192 {
			t.Fatalf("stats dropped overflow in global count: delta=%d", got)
		}
	})
}
