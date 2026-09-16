package company

import (
	"testing"

	"tabmail/internal/models"
)

// DeriveSubmissionStatus maps (job.state, per-recipient ledger states) onto the
// six user-facing submission statuses. The mapping is API contract: these cases
// pin it.
//
//	precedence: uncertainty > active sending > queued > ledger outcome
//	uncertain (ledger row or in-flight ambiguity)      -> needs_attention
//	processing                                          -> sending
//	pending                                             -> submitted
//	ledger present:
//	  all accepted                                      -> accepted
//	  mixed accepted / not-yet-final                    -> partially_accepted
//	  nothing accepted, permanent failure or terminal
//	  job state, or anomaly against job.state           -> needs_attention
//	  nothing accepted, still retrying                  -> sending
//	legacy jobs without a ledger fall back to job.state
func TestDeriveSubmissionStatus(t *testing.T) {
	cases := []struct {
		name     string
		state    models.OutboundState
		ledger   []string
		expected string
	}{
		{"queued without ledger", models.OutboundPending, nil, "submitted"},
		{"queued with untouched ledger", models.OutboundPending, []string{"pending", "pending"}, "submitted"},
		{"sending", models.OutboundProcessing, []string{"pending"}, "sending"},
		{"sending mid-retry", models.OutboundRetry, []string{"temporary"}, "sending"},
		{"accepted", models.OutboundSent, []string{"accepted"}, "accepted"},
		{"accepted fanout", models.OutboundSent, []string{"accepted", "accepted", "accepted"}, "accepted"},
		{"partial permanent", models.OutboundSent, []string{"accepted", "permanent"}, "partially_accepted"},
		{"partial temporary", models.OutboundSent, []string{"accepted", "temporary"}, "partially_accepted"},
		{"partial pending", models.OutboundSent, []string{"accepted", "pending"}, "partially_accepted"},
		{"all permanent", models.OutboundSent, []string{"permanent", "permanent"}, "needs_attention"},
		{"all temporary terminal", models.OutboundSent, []string{"temporary"}, "needs_attention"},
		{"retry exhausted", models.OutboundFailed, []string{"temporary"}, "needs_attention"},
		{"dead job", models.OutboundDead, []string{"pending"}, "needs_attention"},
		{"uncertain recipient wins", models.OutboundSent, []string{"accepted", "uncertain"}, "needs_attention"},
		{"uncertain beats processing", models.OutboundProcessing, []string{"uncertain"}, "needs_attention"},
		{"legacy sent", models.OutboundSent, nil, "accepted"},
		{"legacy retry", models.OutboundRetry, nil, "sending"},
		{"legacy failed", models.OutboundFailed, nil, "needs_attention"},
		{"legacy dead", models.OutboundDead, nil, "needs_attention"},
	}
	for _, tc := range cases {
		if got := DeriveSubmissionStatus(tc.state, tc.ledger); got != tc.expected {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.expected)
		}
	}
}
