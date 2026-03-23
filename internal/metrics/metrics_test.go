package metrics

import (
	"strings"
	"testing"
	"time"

	"tabmail/internal/models"
)

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
