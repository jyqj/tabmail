package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestFakeStoreClaimLeasesAndReclaimsExpiredProcessing(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("outbox", func(t *testing.T) {
		st := NewFakeStore()
		event := &models.OutboxEvent{EventType: "message.received", Payload: []byte(`{}`), NextAttemptAt: now}
		if err := st.CreateOutboxEvent(ctx, event); err != nil {
			t.Fatal(err)
		}

		claimed, err := st.ClaimOutboxEvents(ctx, now, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].State != "processing" || claimed[0].Attempts != 1 {
			t.Fatalf("unexpected first claim: %#v", claimed)
		}
		if claimed[0].ClaimedAt == nil || claimed[0].LeaseUntil == nil || !claimed[0].LeaseUntil.After(now) {
			t.Fatalf("expected claim lease fields, got claimed_at=%v lease_until=%v", claimed[0].ClaimedAt, claimed[0].LeaseUntil)
		}

		claimed, err = st.ClaimOutboxEvents(ctx, now.Add(time.Minute), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 0 {
			t.Fatalf("expected active lease to suppress reclaim, got %d", len(claimed))
		}

		claimed, err = st.ClaimOutboxEvents(ctx, now.Add(fakeClaimLeaseDuration+time.Second), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].Attempts != 2 {
			t.Fatalf("expected expired processing reclaim, got %#v", claimed)
		}
		if err := st.MarkOutboxEventDone(ctx, event.ID); err != nil {
			t.Fatal(err)
		}
		stored := st.outbox[event.ID]
		if stored.ClaimedAt != nil || stored.LeaseUntil != nil {
			t.Fatalf("expected done to clear lease, got claimed_at=%v lease_until=%v", stored.ClaimedAt, stored.LeaseUntil)
		}
	})

	t.Run("webhook", func(t *testing.T) {
		st := NewFakeStore()
		event := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: []byte(`{}`)}
		if err := st.CreateWebhookDeliveries(ctx, event, []string{"https://example.com/hook"}); err != nil {
			t.Fatal(err)
		}
		claimNow := time.Now().UTC().Add(time.Second)
		claimed, err := st.ClaimWebhookDeliveries(ctx, claimNow, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].ClaimedAt == nil || claimed[0].LeaseUntil == nil {
			t.Fatalf("expected webhook lease on claim, got %#v", claimed)
		}
		claimed, err = st.ClaimWebhookDeliveries(ctx, claimNow.Add(fakeClaimLeaseDuration+time.Second), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].Attempts != 2 {
			t.Fatalf("expected webhook expired reclaim, got %#v", claimed)
		}
		if err := st.MarkWebhookDeliveryRetry(ctx, claimed[0].ID, "boom", now.Add(time.Minute), false); err != nil {
			t.Fatal(err)
		}
		stored := st.deliveries[claimed[0].ID]
		if stored.ClaimedAt != nil || stored.LeaseUntil != nil {
			t.Fatalf("expected retry to clear webhook lease, got claimed_at=%v lease_until=%v", stored.ClaimedAt, stored.LeaseUntil)
		}
	})

	t.Run("ingest", func(t *testing.T) {
		st := NewFakeStore()
		job := &models.IngestJob{RawObjectKey: "raw/job.eml", Recipients: []string{"a@mail.test"}, NextAttemptAt: now}
		if err := st.CreateIngestJob(ctx, job, nil); err != nil {
			t.Fatal(err)
		}
		claimed, err := st.ClaimIngestJobs(ctx, now, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].ClaimedAt == nil || claimed[0].LeaseUntil == nil {
			t.Fatalf("expected ingest lease on claim, got %#v", claimed)
		}
		claimed, err = st.ClaimIngestJobs(ctx, now.Add(fakeClaimLeaseDuration+time.Second), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) != 1 || claimed[0].Attempts != 2 {
			t.Fatalf("expected ingest expired reclaim, got %#v", claimed)
		}
		if err := st.MarkIngestJobRetry(ctx, job.ID, "boom", now.Add(time.Minute), true); err != nil {
			t.Fatal(err)
		}
		stored := st.ingestJobs[job.ID]
		if stored.State != "dead" || stored.ClaimedAt != nil || stored.LeaseUntil != nil {
			t.Fatalf("expected dead retry to clear ingest lease, got state=%s claimed_at=%v lease_until=%v", stored.State, stored.ClaimedAt, stored.LeaseUntil)
		}
	})
}
