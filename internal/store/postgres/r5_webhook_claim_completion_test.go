package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

type r5QueueCompletionCase struct {
	name, table, state string
	mark               func(context.Context, *postgres.PgStore, uuid.UUID, int) error
}

var r5QueueCompletionCases = []r5QueueCompletionCase{
	{"outbox-done", "outbox_events", "done", func(ctx context.Context, s *postgres.PgStore, id uuid.UUID, attempt int) error {
		return s.MarkOutboxEventDoneClaim(ctx, id, attempt)
	}},
	{"outbox-retry", "outbox_events", "retry", func(ctx context.Context, s *postgres.PgStore, id uuid.UUID, attempt int) error {
		return s.MarkOutboxEventRetryClaim(ctx, id, attempt, "owned retry", time.Now().Add(time.Hour))
	}},
	{"webhook-done", "webhook_deliveries", "delivered", func(ctx context.Context, s *postgres.PgStore, id uuid.UUID, attempt int) error {
		return s.MarkWebhookDeliveryDoneClaim(ctx, id, attempt)
	}},
	{"webhook-retry", "webhook_deliveries", "retry", func(ctx context.Context, s *postgres.PgStore, id uuid.UUID, attempt int) error {
		return s.MarkWebhookDeliveryRetryClaim(ctx, id, attempt, "owned retry", time.Now().Add(time.Hour), false)
	}},
	{"webhook-dead", "webhook_deliveries", "dead", func(ctx context.Context, s *postgres.PgStore, id uuid.UUID, attempt int) error {
		return s.MarkWebhookDeliveryRetryClaim(ctx, id, attempt, "owned failure", time.Now().Add(time.Hour), true)
	}},
}

// Each case owns a distinct event and delivery in this test's fresh database.
// Rows are claimed through the production SQL; only lease expiry is fixture
// data, avoiding a five-minute wall-clock wait for every reclaim case.
func r5QueueCompletionRow(t *testing.T, st *postgres.PgStore, pool *pgxpool.Pool, tc r5QueueCompletionCase) (uuid.UUID, int) {
	t.Helper()
	ctx := context.Background()
	event := &models.OutboxEvent{EventType: "r5.claim-completion", Payload: []byte(`{"synthetic":true}`)}
	must(t, st.CreateOutboxEvent(ctx, event))
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE id=$1`, event.ID)
		must(t, err)
	})
	id := event.ID
	if tc.table == "webhook_deliveries" {
		must(t, st.CreateWebhookDeliveries(ctx, event, []string{"http://127.0.0.1:1/never-dispatched"}))
		must(t, pool.QueryRow(ctx, `SELECT id FROM webhook_deliveries WHERE event_id=$1`, event.ID).Scan(&id))
	}
	return id, r5QueueCompletionClaim(t, st, pool, tc, id)
}

func r5QueueCompletionClaim(t *testing.T, st *postgres.PgStore, pool *pgxpool.Pool, tc r5QueueCompletionCase, id uuid.UUID) int {
	t.Helper()
	ctx := context.Background()
	var now time.Time
	must(t, pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now))
	if tc.table == "outbox_events" {
		rows, err := st.ClaimOutboxEvents(ctx, now, 100)
		must(t, err)
		for _, row := range rows {
			if row.ID == id && row.State == "processing" && row.LeaseUntil != nil {
				return row.Attempts
			}
		}
	} else {
		rows, err := st.ClaimWebhookDeliveries(ctx, now, 100)
		must(t, err)
		for _, row := range rows {
			if row.ID == id && row.State == "processing" && row.LeaseUntil != nil {
				return row.Attempts
			}
		}
	}
	t.Fatal("production claim did not return the owned processing row")
	return 0
}

func r5QueueCompletionSnapshot(t *testing.T, pool *pgxpool.Pool, table string, id uuid.UUID) string {
	t.Helper()
	var raw string
	must(t, pool.QueryRow(context.Background(), `SELECT row_to_json(q)::text FROM `+table+` q WHERE id=$1`, id).Scan(&raw))
	return raw
}

func TestR5WebhookClaimCompletionPreservesCurrentGeneration(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	for _, tc := range r5QueueCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			id, attempt := r5QueueCompletionRow(t, st, pool, tc)
			must(t, tc.mark(context.Background(), st, id, attempt))
			var state string
			var claimed, lease *time.Time
			must(t, pool.QueryRow(context.Background(), `SELECT state,claimed_at,lease_until FROM `+tc.table+` WHERE id=$1`, id).Scan(&state, &claimed, &lease))
			if state != tc.state || claimed != nil || lease != nil {
				t.Fatalf("current generation did not complete: state=%s claimed=%v lease=%v", state, claimed, lease)
			}
		})
	}
}

func TestR5WebhookClaimCompletionRejectsLostOwnership(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	for _, tc := range r5QueueCompletionCases {
		for _, boundary := range []string{"wrong-generation", "zero-generation", "negative-generation", "missing-row", "pending-state", "expired-lease", "null-lease", "reclaimed-processing", "same-generation-completed"} {
			t.Run(tc.name+"/"+boundary, func(t *testing.T) {
				ctx := context.Background()
				id, attempt := r5QueueCompletionRow(t, st, pool, tc)
				markID, markAttempt := id, attempt
				switch boundary {
				case "wrong-generation":
					markAttempt++
				case "zero-generation":
					markAttempt = 0
				case "negative-generation":
					markAttempt = -1
				case "missing-row":
					markID = uuid.New()
				case "pending-state":
					_, err := pool.Exec(ctx, `UPDATE `+tc.table+` SET state='pending',claimed_at=NULL,lease_until=NULL WHERE id=$1`, id)
					must(t, err)
				case "null-lease":
					_, err := pool.Exec(ctx, `UPDATE `+tc.table+` SET lease_until=NULL WHERE id=$1`, id)
					must(t, err)
				case "expired-lease", "reclaimed-processing":
					_, err := pool.Exec(ctx, `UPDATE `+tc.table+` SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
					must(t, err)
					if boundary != "expired-lease" {
						current := r5QueueCompletionClaim(t, st, pool, tc, id)
						if current != attempt+1 {
							t.Fatalf("reclaim generation=%d, previous=%d", current, attempt)
						}
					}
				case "same-generation-completed":
					must(t, tc.mark(ctx, st, id, attempt))
				}
				before := r5QueueCompletionSnapshot(t, pool, tc.table, id)
				err := tc.mark(ctx, st, markID, markAttempt)
				if !errors.Is(err, store.ErrClaimLeaseLost) {
					t.Errorf("lost ownership did not return ErrClaimLeaseLost: %v", err)
				}
				if after := r5QueueCompletionSnapshot(t, pool, tc.table, id); after != before {
					t.Error("lost completion rewrote queue state, diagnostics, schedule, or ownership timestamps")
				}
			})
		}
	}
}

func TestR5WebhookClaimCompletionPreservesNewerTerminalState(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	for _, stale := range r5QueueCompletionCases {
		for _, current := range r5QueueCompletionCases {
			if stale.table != current.table {
				continue
			}
			t.Run(stale.name+"/after-"+current.state, func(t *testing.T) {
				ctx := context.Background()
				id, oldAttempt := r5QueueCompletionRow(t, st, pool, stale)
				_, err := pool.Exec(ctx, `UPDATE `+stale.table+` SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
				must(t, err)
				currentAttempt := r5QueueCompletionClaim(t, st, pool, stale, id)
				if currentAttempt != oldAttempt+1 {
					t.Fatal("actual reclaim did not advance the generation")
				}
				must(t, current.mark(ctx, st, id, currentAttempt))
				before := r5QueueCompletionSnapshot(t, pool, stale.table, id)
				if err := stale.mark(ctx, st, id, oldAttempt); !errors.Is(err, store.ErrClaimLeaseLost) {
					t.Errorf("stale completion did not report lost generation: %v", err)
				}
				if after := r5QueueCompletionSnapshot(t, pool, stale.table, id); after != before {
					t.Error("stale completion rewrote a newer done, delivered, retry, or dead result")
				}
			})
		}
	}
}

func TestR5WebhookClaimCompletionPreservesCancellationAndDatabaseFailure(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	for _, tc := range r5QueueCompletionCases {
		for _, boundary := range []string{"cancelled", "write-rejected"} {
			t.Run(tc.name+"/"+boundary, func(t *testing.T) {
				id, attempt := r5QueueCompletionRow(t, st, pool, tc)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if boundary == "cancelled" {
					cancel()
				} else {
					_, err := pool.Exec(ctx, `ALTER TABLE `+tc.table+` ADD CONSTRAINT r5_owned_completion_failure CHECK(id<>'`+id.String()+`'::uuid OR state='processing') NOT VALID`)
					must(t, err)
					t.Cleanup(func() {
						_, err := pool.Exec(context.Background(), `ALTER TABLE `+tc.table+` DROP CONSTRAINT r5_owned_completion_failure`)
						must(t, err)
					})
				}
				before := r5QueueCompletionSnapshot(t, pool, tc.table, id)
				err := tc.mark(ctx, st, id, attempt)
				if boundary == "cancelled" {
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation cause lost: %v", err)
					}
				} else {
					var pgerr *pgconn.PgError
					if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "r5_owned_completion_failure" {
						t.Errorf("write failure cause lost: %v", err)
					}
				}
				if after := r5QueueCompletionSnapshot(t, pool, tc.table, id); after != before {
					t.Error("failed completion changed the processing row")
				}
			})
		}
	}
}

func TestR5WebhookClaimCompletionChecksLeaseAfterRowWait(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	for _, tc := range r5QueueCompletionCases {
		t.Run(tc.name, func(t *testing.T) {
			id, attempt := r5QueueCompletionRow(t, st, pool, tc)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			var deadline time.Time
			must(t, pool.QueryRow(ctx, `UPDATE `+tc.table+` SET lease_until=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING lease_until`, id).Scan(&deadline))
			gate, err := pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			var gatePID int
			must(t, gate.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&gatePID))
			_, err = gate.Exec(ctx, `SELECT id FROM `+tc.table+` WHERE id=$1 FOR UPDATE`, id)
			must(t, err)
			before := r5QueueCompletionSnapshot(t, pool, tc.table, id)
			result := make(chan error, 1)
			go func() { result <- tc.mark(ctx, st, id, attempt) }()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				must(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, gatePID).Scan(&waiting))
				if waiting {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("completion did not reach the actual held row lock: %v", err)
				case <-ctx.Done():
					t.Fatal("completion never waited for owned row lock")
				case <-ticker.C:
				}
			}
			for {
				var expired bool
				must(t, pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired))
				if expired {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("owned processing lease did not expire")
				case <-ticker.C:
				}
			}
			must(t, gate.Rollback(ctx))
			select {
			case err := <-result:
				if !errors.Is(err, store.ErrClaimLeaseLost) {
					t.Errorf("completion after lease expiry was not fenced: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("completion did not exit after releasing the lock")
			}
			if after := r5QueueCompletionSnapshot(t, pool, tc.table, id); after != before {
				t.Error("completion whose lease expired while waiting changed the row")
			}
		})
	}
}
