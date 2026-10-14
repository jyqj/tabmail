package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Test-binary-only bridge: the production wrapper remains unexported.
func R5SweepCompanyAttachmentsForTest(ctx context.Context, s *PgStore, tenant uuid.UUID) error {
	return s.sweepCompanyAttachments(ctx, tenant)
}

func TestR5AttachmentGCTenantConnectDeadlineCoversDNSAndFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		connect time.Duration
		caller  time.Duration
		want    time.Duration
	}{
		{"default_total_cap", 0, 30 * time.Second, 5 * time.Second},
		{"shorter_config", 250 * time.Millisecond, 30 * time.Second, 250 * time.Millisecond},
		{"shorter_caller", 5 * time.Second, 100 * time.Millisecond, 100 * time.Millisecond},
		{"shared_fallback_budget", 150 * time.Millisecond, 30 * time.Second, 150 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.caller)
			defer cancel()
			cfg, err := pgxpool.ParseConfig("host=first.invalid,second.invalid port=5432 user=fixture dbname=fixture sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			cfg.MinConns, cfg.MinIdleConns = 0, 0
			cfg.ConnConfig.ConnectTimeout = tc.connect
			var deadlines []time.Time
			probeErr := errors.New("controlled scheduler DNS failure")
			cfg.ConnConfig.LookupFunc = func(lookupCtx context.Context, host string) ([]string, error) {
				deadline, ok := lookupCtx.Deadline()
				if !ok || time.Until(deadline) > tc.want {
					t.Errorf("DNS/fallback %s has no bounded total deadline: present=%t remaining=%s want<=%s", host, ok, time.Until(deadline), tc.want)
				}
				deadlines = append(deadlines, deadline)
				if tc.name == "shared_fallback_budget" {
					if len(deadlines) == 1 {
						firstCtx, firstCancel := context.WithTimeout(lookupCtx, 50*time.Millisecond)
						<-firstCtx.Done()
						firstCancel()
					} else {
						<-lookupCtx.Done()
						return nil, lookupCtx.Err()
					}
				}
				return nil, probeErr
			}
			// Lazy zero-minimum pool: no live PostgreSQL or network dial is used.
			// Exercise the production scheduler's real ConnectConfig call, not a
			// separate mock timeout helper that the entry point might bypass.
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			st := &PgStore{pool: pool}
			if err := st.sweepCompanyAttachmentTenants(ctx); err == nil {
				t.Fatal("DNS failure was swallowed")
			}
			if len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
				t.Fatalf("DNS fallbacks did not share a single connect deadline: %v", deadlines)
			}
			if tc.name == "shared_fallback_budget" && ctx.Err() != nil {
				t.Fatal("connect timeout consumed/cancelled the outer scanner context")
			}
		})
	}
}
