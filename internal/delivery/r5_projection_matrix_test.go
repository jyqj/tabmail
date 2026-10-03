package delivery_test

import (
	"fmt"
	"testing"

	"tabmail/internal/company"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
)

// This truth table tests both the policy and the public company forwarding
// entrypoint. It is not a PostgreSQL or HTTP boundary acceptance test.
func TestR5SubmissionProjectionMatrix(t *testing.T) {
	jobs := []models.OutboundState{models.OutboundPending, models.OutboundProcessing, models.OutboundRetry, models.OutboundSent, models.OutboundFailed, models.OutboundDead, models.OutboundCancelled, "unknown", ""}
	// Columns: all accepted, partial accepted, untouched/temporary, permanent,
	// uncertain or malformed. No status means final delivery to the recipient.
	table := map[models.OutboundState][5]string{
		models.OutboundPending:    {"submitted", "submitted", "submitted", "submitted", "needs_attention"},
		models.OutboundProcessing: {"sending", "sending", "sending", "sending", "needs_attention"},
		models.OutboundRetry:      {"accepted", "partially_accepted", "sending", "needs_attention", "needs_attention"},
		models.OutboundSent:       {"accepted", "partially_accepted", "needs_attention", "needs_attention", "needs_attention"},
		models.OutboundFailed:     {"accepted", "partially_accepted", "needs_attention", "needs_attention", "needs_attention"},
		models.OutboundDead:       {"accepted", "partially_accepted", "needs_attention", "needs_attention", "needs_attention"},
		models.OutboundCancelled:  {"accepted", "partially_accepted", "cancelled", "cancelled", "needs_attention"},
		"unknown":                 {"needs_attention", "needs_attention", "needs_attention", "needs_attention", "needs_attention"},
		"":                        {"needs_attention", "needs_attention", "needs_attention", "needs_attention", "needs_attention"},
	}
	alphabet := []string{delivery.Pending, delivery.Accepted, delivery.Temporary, delivery.Permanent, delivery.Uncertain, "unknown", "", "cancelled"}
	var ledgers [][]string
	var add func([]string, int)
	add = func(prefix []string, remaining int) {
		if remaining == 0 {
			ledgers = append(ledgers, append([]string{}, prefix...))
			return
		}
		for _, state := range alphabet {
			add(append(prefix, state), remaining-1)
		}
	}
	for length := 1; length <= 3; length++ {
		add(nil, length)
	}
	for _, job := range jobs {
		for _, ledger := range ledgers {
			column, accepted, permanent, malformed := 2, 0, false, false
			for _, state := range ledger {
				switch state {
				case delivery.Accepted:
					accepted++
				case delivery.Pending, delivery.Temporary:
				case delivery.Permanent:
					permanent = true
				default:
					malformed = true
				}
			}
			switch {
			case malformed:
				column = 4
			case accepted == len(ledger):
				column = 0
			case accepted > 0:
				column = 1
			case permanent:
				column = 3
			}
			want := table[job][column]
			t.Run(fmt.Sprintf("%s/%v", job, ledger), func(t *testing.T) {
				if got := delivery.DeriveSubmissionStatus(job, ledger); got != want {
					t.Fatalf("delivery = %q, want %q", got, want)
				}
				if got := company.DeriveSubmissionStatus(job, ledger); got != want {
					t.Fatalf("company = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestR5SubmissionEmptyLedgerFailsClosed(t *testing.T) {
	for _, job := range []models.OutboundState{models.OutboundPending, models.OutboundProcessing, models.OutboundRetry, models.OutboundSent, models.OutboundFailed, models.OutboundDead, models.OutboundCancelled, "unknown", ""} {
		for _, ledger := range [][]string{nil, {}} {
			if got := company.DeriveSubmissionStatus(job, ledger); got != delivery.SubmissionNeedsAttention {
				t.Errorf("%q + empty ledger (nil=%v) = %q, want needs_attention", job, ledger == nil, got)
			}
		}
	}
}
