package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// These nullable fields are legal persisted queue rows, including rows made
// by messageOutbox/companyAudit and CreateWebhookDeliveries in production.
// Never repair the fixture to hide an adapter's NULL decoding failure.
func TestR5QueueNullableDiagnosticsClaimAndRead(t *testing.T) {
	for _, mode := range []string{"outbox", "webhook", "dead-webhook", "ingest"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			id := uuid.New()
			switch mode {
			case "outbox":
				_, e := f.pool.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload) VALUES($1,'nullable.fixture','{}')`, id)
				must(t, e)
				rows, e := f.st.ClaimOutboxEvents(ctx, time.Now().Add(time.Second), 100)
				must(t, e)
				found := false
				for _, r := range rows {
					if r.ID == id {
						found = true
						if r.LastError != "" {
							t.Fatal("NULL diagnostic was not empty")
						}
					}
				}
				if !found {
					t.Fatal("legal NULL outbox row not claimed")
				}
			case "webhook", "dead-webhook":
				j := &models.OutboxEvent{ID: id, EventType: "nullable.fixture", Payload: []byte(`{}`)}
				must(t, f.st.CreateOutboxEvent(ctx, j))
				must(t, f.st.CreateWebhookDeliveries(ctx, j, []string{"http://127.0.0.1:1/never-sent"}))
				if mode == "dead-webhook" {
					_, e := f.pool.Exec(ctx, `UPDATE webhook_deliveries SET state='dead',last_error=NULL WHERE event_id=$1`, id)
					must(t, e)
					rows, e := f.st.ListDeadWebhookDeliveries(ctx, 100)
					must(t, e)
					if len(rows) != 1 || rows[0].LastError != "" {
						t.Fatal("legal NULL dead letter not returned")
					}
				} else {
					rows, e := f.st.ClaimWebhookDeliveries(ctx, time.Now().Add(time.Second), 100)
					must(t, e)
					if len(rows) != 1 || rows[0].EventID != id || rows[0].LastError != "" {
						t.Fatal("legal NULL delivery not claimed")
					}
				}
			case "ingest":
				_, e := f.pool.Exec(ctx, `INSERT INTO ingest_jobs(id,source,mail_from,recipients,raw_object_key) VALUES($1,'nullable.fixture','sender@fixture.test',ARRAY['recipient@fixture.test'],'nullable-fixture')`, id)
				must(t, e)
				rows, _, e := f.st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 100}, "", "", "")
				must(t, e)
				if len(rows) != 1 || rows[0].ID != id || rows[0].LastError != "" {
					t.Fatal("legal NULL ingest diagnostic not returned")
				}
			}
		})
	}
}
