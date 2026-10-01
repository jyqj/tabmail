package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

var r5FanoutURLs = []string{"http://127.0.0.1:1/r5-fanout/a", "http://127.0.0.1:1/r5-fanout/b"}

func r5FanoutFixture(t *testing.T) (*companyFixture, *models.OutboxEvent) {
	t.Helper()
	st, pool, _ := testpg.NewPostgres(t)
	f := &companyFixture{st: st, pool: pool}
	e := &models.OutboxEvent{EventType: "r5.fanout", Payload: json.RawMessage(`{"type":"r5.fanout"}`)}
	must(t, st.CreateOutboxEvent(context.Background(), e))
	return f, e
}

// Seed only a legal near-expiry processing lease, then observe the actual DB
// clock and real ClaimOutboxEvents reclaim. This represents a stalled handler;
// the production worker's leaseTTL is metadata, not a cancellation deadline.
func r5FanoutTwoClaims(t *testing.T, f *companyFixture, ctx context.Context, e *models.OutboxEvent) (*models.OutboxEvent, *models.OutboxEvent) {
	t.Helper()
	first, err := f.st.ClaimOutboxEvents(ctx, time.Now(), 10)
	must(t, err)
	if len(first) != 1 || first[0].ID != e.ID {
		t.Fatal("first actual claim did not own the exact event")
	}
	second, err := f.st.ClaimOutboxEvents(ctx, time.Now(), 10)
	must(t, err)
	if len(second) != 0 {
		t.Fatal("an unexpired current processing claim was not mutually exclusive")
	}
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE outbox_events SET lease_until=clock_timestamp()+interval '1 second' WHERE id=$1 AND state='processing' RETURNING lease_until`, e.ID).Scan(&deadline))
	first[0].LeaseUntil = &deadline
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired))
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual processing lease did not expire; no reclaim evidence")
		case <-ticker.C:
		}
	}
	second, err = f.st.ClaimOutboxEvents(ctx, time.Now(), 10)
	must(t, err)
	if len(second) != 1 || second[0].ID != e.ID || second[0].Attempts != first[0].Attempts+1 || second[0].LeaseUntil == nil || !second[0].LeaseUntil.After(deadline) {
		t.Fatal("actual expired claim did not produce the same-event reclaim generation")
	}
	return first[0], second[0]
}

// No HTTP worker is started: these are the real DB commands used by
// Dispatcher.processOutbox, including expired-generation compatibility.
func TestR5WebhookFanoutClaimReclaimAndUniqueReplay(t *testing.T) {
	f, e := r5FanoutFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	old, current := r5FanoutTwoClaims(t, f, ctx, e)
	parent := r5FanoutParent(t, f, e.ID)
	must(t, f.st.CreateWebhookDeliveries(ctx, old, r5FanoutURLs))
	before := r5FanoutRows(t, f, e.ID)
	must(t, f.st.CreateWebhookDeliveries(ctx, current, []string{r5FanoutURLs[1], r5FanoutURLs[0], r5FanoutURLs[1]}))
	if r5FanoutRows(t, f, e.ID) != before {
		t.Fatal("same-event duplicate URL replay rewrote IDs/payload/state/timestamps")
	}
	r5FanoutEffects(t, f, e, parent, 2, true)
}

func TestR5WebhookFanoutBatchFailureRollsBack(t *testing.T) {
	f, e := r5FanoutFixture(t)
	ctx := context.Background()
	parent := r5FanoutParent(t, f, e.ID)
	_, err := f.pool.Exec(ctx, `ALTER TABLE webhook_deliveries ADD CONSTRAINT r5_fanout_second_failure CHECK(url<>'`+r5FanoutURLs[1]+`') NOT VALID`)
	must(t, err)
	err = f.st.CreateWebhookDeliveries(ctx, e, r5FanoutURLs)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "r5_fanout_second_failure" {
		t.Fatalf("second-URL failure not observed: SQLSTATE=%s err=%v", r5SQLState(err), err)
	}
	r5FanoutEffects(t, f, e, parent, 0, false)
	_, err = f.pool.Exec(ctx, `ALTER TABLE webhook_deliveries DROP CONSTRAINT r5_fanout_second_failure`)
	must(t, err)
	must(t, f.st.CreateWebhookDeliveries(ctx, e, r5FanoutURLs))
	r5FanoutEffects(t, f, e, parent, 2, true)
}

func TestR5WebhookFanoutCompleteCommandsURLLockOrder(t *testing.T) {
	for _, order := range []string{"ab-first", "ba-first"} {
		t.Run(order, func(t *testing.T) {
			f, e := r5FanoutFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			old, current := r5FanoutTwoClaims(t, f, ctx, e)
			parent := r5FanoutParent(t, f, e.ID)
			name := "r5_fanout_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			prefix := "r5-fanout:" + e.ID.String() + ":"
			// AFTER INSERT proves the first unique row really exists in the
			// command transaction before its second URL is attempted.
			_, err := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_id='`+e.ID.String()+`'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+prefix+`'||NEW.url,0)); END IF; RETURN NEW; END $$; CREATE TRIGGER `+name+` AFTER INSERT ON webhook_deliveries FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
			must(t, err)
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			for _, url := range r5FanoutURLs {
				_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, prefix+url)
				must(t, err)
			}
			firstURLs, secondURLs := r5FanoutURLs, []string{r5FanoutURLs[1], r5FanoutURLs[0]}
			if order == "ba-first" {
				firstURLs, secondURLs = secondURLs, firstURLs
			}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- f.st.CreateWebhookDeliveries(ctx, old, firstURLs) }()
			firstPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "INSERT INTO webhook_deliveries")
			go func() { secondDone <- f.st.CreateWebhookDeliveries(ctx, current, secondURLs) }()
			// A corrected command may serialize on the first writer instead
			// of manufacturing two opposite prefix owners. Observe either edge.
			secondPID, serialized := r5FanoutSecondWait(t, f, ctx, gate.Conn().PgConn().PID(), firstPID)
			r5MaintenanceTrace(t, f, ctx, gate.Conn().PgConn().PID(), firstPID, secondPID)
			must(t, gate.Rollback(ctx))
			firstErr := r5ConcurrentResult(t, ctx, firstDone)
			secondErr := r5ConcurrentResult(t, ctx, secondDone)
			firstDead, secondDead := r5SQLState(firstErr) == "40P01", r5SQLState(secondErr) == "40P01"
			if firstErr != nil && !firstDead {
				t.Fatalf("unexpected first command SQLSTATE=%s err=%v", r5SQLState(firstErr), firstErr)
			}
			if secondErr != nil && !secondDead {
				t.Fatalf("unexpected second command SQLSTATE=%s err=%v", r5SQLState(secondErr), secondErr)
			}
			if firstErr != nil && secondErr != nil {
				t.Fatal("neither fanout command committed")
			}
			// All rows must be from the successful complete command. Its
			// single captured now timestamp detects a retained victim prefix.
			r5FanoutEffects(t, f, e, parent, 2, true)
			rows := r5FanoutRows(t, f, e.ID)
			must(t, f.st.CreateWebhookDeliveries(ctx, current, secondURLs))
			if rows != r5FanoutRows(t, f, e.ID) {
				t.Fatal("fresh retry after victim rollback was not byte-idempotent")
			}
			if firstDead || secondDead {
				t.Errorf("actual same-event fanout 40P01; whole-batch victim rollback/unique rows/payload/parent/no-external effects verified: first=%s second=%s", r5SQLState(firstErr), r5SQLState(secondErr))
			} else {
				t.Logf("complete same-event commands serialized=%v; no assumed URL inverse edge", serialized)
			}
		})
	}
}

func r5FanoutSecondWait(t *testing.T, f *companyFixture, ctx context.Context, gate, first uint32) (uint32, bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid uint32
		var onFirst bool
		err := f.pool.QueryRow(ctx, `SELECT pid,$2::int=ANY(pg_blocking_pids(pid)) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND pid<>$2::int AND ($1::int=ANY(pg_blocking_pids(pid)) OR $2::int=ANY(pg_blocking_pids(pid))) LIMIT 1`, int32(gate), int32(first)).Scan(&pid, &onFirst)
		if err == nil {
			return pid, onFirst
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			must(t, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("second command SQL blocker not observed; timeout is not lock-order evidence")
		case <-ticker.C:
		}
	}
}
func r5FanoutParent(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var parent string
	must(t, f.pool.QueryRow(context.Background(), `SELECT to_jsonb(e)::text FROM outbox_events e WHERE id=$1`, id).Scan(&parent))
	return parent
}
func r5FanoutRows(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var rows string
	must(t, f.pool.QueryRow(context.Background(), `SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY url),'[]'::jsonb)::text FROM webhook_deliveries d WHERE event_id=$1`, id).Scan(&rows))
	return rows
}
func r5FanoutEffects(t *testing.T, f *companyFixture, e *models.OutboxEvent, parent string, want int, oneCompleteBatch bool) {
	t.Helper()
	var count, distinctURLs, createdTimes, events, audits, content int
	var matching bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT count(*),count(DISTINCT url),count(DISTINCT created_at),COALESCE(bool_and(payload=$2::jsonb AND event_type=$3 AND state='pending' AND attempts=0 AND next_attempt_at=created_at AND updated_at=created_at AND url=ANY($4::text[])),true),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM audit_log),(SELECT count(*) FROM messages)+(SELECT count(*) FROM mail_documents)+(SELECT count(*) FROM mail_index_jobs) FROM webhook_deliveries WHERE event_id=$1`, e.ID, e.Payload, e.EventType, r5FanoutURLs).Scan(&count, &distinctURLs, &createdTimes, &matching, &events, &audits, &content))
	if count != want || distinctURLs != want || !matching || events != 1 || audits != 0 || content != 0 {
		t.Fatalf("fanout rows/unique/payload/state/parent/audit/content effects: count=%d unique=%d matching=%v events=%d audit=%d content=%d", count, distinctURLs, matching, events, audits, content)
	}
	if oneCompleteBatch && createdTimes != 1 {
		t.Fatal("failed fanout prefix survived beside a different successful command's rows")
	}
	if r5FanoutParent(t, f, e.ID) != parent {
		t.Fatal("fanout altered claimed event payload/state/lease/attempt ownership")
	}
}
