package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"tabmail/internal/models"
)

func TestMailboxCountersAreBounded(t *testing.T) {
	t.Cleanup(func() {
		c.mu.Lock()
		c.mailboxes = map[string]*deliveryCounter{}
		c.mu.Unlock()
		mailboxCountersEvicted.Store(0)
	})

	// A busy mailbox must survive a flood of one-off recipients, which is
	// what an address-scanning sender produces.
	const busy = "real@mail.test"
	for i := 0; i < 50; i++ {
		MailboxRecipientAccepted(busy)
	}
	for i := 0; i < maxTrackedMailboxes*2; i++ {
		MailboxRecipientRejected(fmt.Sprintf("scan-%d@mail.test", i))
	}

	if got := trackedMailboxCount(); got > maxTrackedMailboxes {
		t.Fatalf("tracked mailboxes = %d, want <= %d", got, maxTrackedMailboxes)
	}
	if mailboxCountersEvicted.Load() == 0 {
		t.Fatal("expected evictions to be counted")
	}

	c.mu.Lock()
	_, kept := c.mailboxes[busy]
	c.mu.Unlock()
	if !kept {
		t.Fatalf("the busiest mailbox was evicted ahead of one-off recipients")
	}
}

func TestRenderPrometheusIncludesHistograms(t *testing.T) {
	ObserveIngestJobLatency(1500 * time.Millisecond)
	ObserveRetentionSweepDuration(120 * time.Millisecond)

	body := RenderPrometheus(models.MetricsSnapshot{}, map[string]float64{
		"tabmail_ingest_queue_depth": 3,
	})

	for _, needle := range []string{
		`tabmail_ingest_job_latency_seconds_bucket{le="2"}`,
		`tabmail_ingest_job_latency_seconds_sum`,
		`tabmail_retention_sweep_duration_seconds_bucket{le="0.25"}`,
		`tabmail_retention_sweep_duration_seconds_count`,
		`tabmail_ingest_queue_depth 3`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected prometheus output to contain %q, got:\n%s", needle, body)
		}
	}
}
