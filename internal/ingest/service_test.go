package ingest

import (
	"context"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
	"testing"
	"time"
)

// A fake without the transactional ledger is no longer evidence of durable
// acceptance. Real PostgreSQL replaces the old tests that expected failed mail
// to be silently completed and its only raw object to be deleted.
func TestDurableRejectsStoreWithoutLedger(t *testing.T) {
	st := testutil.NewFakeStore()
	s := NewService(st, testutil.NewMemoryObjectStore(), resolver.New(st, policy.NamingFull, false), nil, nil, models.SMTPPolicy{}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	result, err := s.Accept(context.Background(), Envelope{Recipients: []string{"a@example.test"}}, []byte("test"), nil)
	if err == nil || result.Queued {
		t.Fatal("nontransactional adapter acknowledged durable mail")
	}
}

func TestRetryBackoffUsesPrecisePowersOfTwo(t *testing.T) {
	cases := []struct {
		attempts int
		min      time.Duration
		max      time.Duration
	}{
		{attempts: 1, min: 1 * time.Second, max: 2 * time.Second},
		{attempts: 2, min: 2 * time.Second, max: 3 * time.Second},
		{attempts: 3, min: 4 * time.Second, max: 5 * time.Second},
		{attempts: 4, min: 8 * time.Second, max: 9 * time.Second},
	}
	for _, tc := range cases {
		got := retryBackoff(tc.attempts)
		if got < tc.min || got >= tc.max {
			t.Fatalf("attempt=%d expected duration in [%s,%s), got %s", tc.attempts, tc.min, tc.max, got)
		}
	}
}
