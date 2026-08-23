package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMailboxCountersAreBounded(t *testing.T) {
	t.Cleanup(func() {
		c.mu.Lock()
		c.mailboxes = map[string]*deliveryCounter{}
		c.mu.Unlock()
	})
	// Prometheus counters cannot be reset, so evictions are measured as a delta.
	evictedBefore := intValue(mailboxCountersEvicted)

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
	if intValue(mailboxCountersEvicted) == evictedBefore {
		t.Fatal("expected evictions to be counted")
	}

	c.mu.Lock()
	_, kept := c.mailboxes[busy]
	c.mu.Unlock()
	if !kept {
		t.Fatalf("the busiest mailbox was evicted ahead of one-off recipients")
	}
}

func TestHandlerExposesTabMailMetrics(t *testing.T) {
	ObserveIngestJobLatency(1500 * time.Millisecond)
	ObserveRetentionSweepDuration(120 * time.Millisecond)
	SetBacklogs(Backlogs{IngestReady: 2, IngestProcessing: 1})

	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("metrics handler returned %d", rr.Code)
	}
	body := rr.Body.String()
	for _, needle := range []string{
		`tabmail_ingest_job_latency_seconds_bucket{le="2"}`,
		`tabmail_ingest_job_latency_seconds_sum`,
		`tabmail_retention_sweep_duration_seconds_bucket{le="0.25"}`,
		`tabmail_retention_sweep_duration_seconds_count`,
		"tabmail_ingest_queue_depth 3",
		"tabmail_ingest_queue_ready_depth 2",
		"tabmail_ingest_queue_inflight 1",
		"tabmail_uptime_seconds",
		// The runtime collectors come with the official client and are what
		// makes the migration worth its dependency.
		"go_goroutines",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected prometheus output to contain %q, got:\n%s", needle, body)
		}
	}
}

// The dashboard JSON and the scrape endpoint read the same collectors, so a
// counter bumped through the public API has to show up in both.
func TestSnapshotReadsBackTheCollectors(t *testing.T) {
	before := Snapshot(true, 0).SMTP.MessagesAccepted
	SMTPMessageAccepted()

	snapshot := Snapshot(true, 7)
	if got := snapshot.SMTP.MessagesAccepted; got != before+1 {
		t.Fatalf("messages accepted = %d, want %d", got, before+1)
	}
	if snapshot.Webhooks.DeadLetterSize != 7 {
		t.Fatalf("dead letter size = %d, want 7", snapshot.Webhooks.DeadLetterSize)
	}
	if len(snapshot.TimeSeries) == 0 {
		t.Fatal("expected the snapshot to carry a time series point")
	}
}
