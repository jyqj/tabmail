package retention

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/testutil"
)

func TestScannerSweepDeletesExpiredMessagesAndObjectsAcrossBatches(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	expiredA := &models.Message{
		ID:           uuid.New(),
		MailboxID:    uuid.New(),
		TenantID:     uuid.New(),
		ZoneID:       uuid.New(),
		RawObjectKey: "raw/a.eml",
		ExpiresAt:    time.Now().Add(-2 * time.Hour),
	}
	expiredB := &models.Message{
		ID:           uuid.New(),
		MailboxID:    uuid.New(),
		TenantID:     uuid.New(),
		ZoneID:       uuid.New(),
		RawObjectKey: "raw/b.eml",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	active := &models.Message{
		ID:           uuid.New(),
		MailboxID:    uuid.New(),
		TenantID:     uuid.New(),
		ZoneID:       uuid.New(),
		RawObjectKey: "raw/active.eml",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	store.SeedMessage(expiredA)
	store.SeedMessage(expiredB)
	store.SeedMessage(active)

	for _, key := range []string{expiredA.RawObjectKey, expiredB.RawObjectKey, active.RawObjectKey} {
		if err := obj.Put(ctx, key, bytes.NewBufferString(key), 0); err != nil {
			t.Fatalf("seed object %s: %v", key, err)
		}
	}

	sc := New(rawobject.NewStore(obj, store), store, config.Storage{
		RetentionBatchSize: 1,
	}, zerolog.Nop())

	sc.sweep(ctx)

	if got, err := store.CountAllMessages(ctx); err != nil || got != 1 {
		t.Fatalf("expected 1 remaining message, got %d err=%v", got, err)
	}
	if msg, err := store.GetMessage(ctx, active.ID); err != nil || msg == nil {
		t.Fatalf("expected active message to remain, msg=%#v err=%v", msg, err)
	}
	if msg, err := store.GetMessage(ctx, expiredA.ID); err != nil || msg != nil {
		t.Fatalf("expected expiredA deleted, msg=%#v err=%v", msg, err)
	}
	if msg, err := store.GetMessage(ctx, expiredB.ID); err != nil || msg != nil {
		t.Fatalf("expected expiredB deleted, msg=%#v err=%v", msg, err)
	}

	if _, err := obj.Get(ctx, expiredA.RawObjectKey); err == nil {
		t.Fatalf("expected object %s to be deleted", expiredA.RawObjectKey)
	}
	if _, err := obj.Get(ctx, expiredB.RawObjectKey); err == nil {
		t.Fatalf("expected object %s to be deleted", expiredB.RawObjectKey)
	}
	if _, err := obj.Get(ctx, active.RawObjectKey); err != nil {
		t.Fatalf("expected active object to remain: %v", err)
	}
}

func TestScannerSweepKeepsObjectReferencedByActiveIngestJob(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	key := "raw/shared.eml"
	expired := &models.Message{
		ID:           uuid.New(),
		MailboxID:    uuid.New(),
		TenantID:     uuid.New(),
		ZoneID:       uuid.New(),
		RawObjectKey: key,
		ExpiresAt:    time.Now().Add(-time.Hour),
	}
	store.SeedMessage(expired)
	if err := store.CreateIngestJob(ctx, &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  key,
		State:         "pending",
		NextAttemptAt: time.Now().Add(time.Hour),
	}, nil); err != nil {
		t.Fatalf("seed ingest job: %v", err)
	}
	if err := obj.Put(ctx, key, bytes.NewBufferString(key), 0); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	sc := New(rawobject.NewStore(obj, store), store, config.Storage{RetentionBatchSize: 10}, zerolog.Nop())
	sc.sweep(ctx)

	if _, err := obj.Get(ctx, key); err != nil {
		t.Fatalf("expected shared object to remain while ingest job still references it: %v", err)
	}
}

func TestScannerSweepReapsExhaustedOrphanRetries(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	// A key past the retry cap would otherwise linger as a zombie row: it is
	// filtered out of ListPendingOrphanRetries (never retried) and never reaches
	// the clear path. sweep must drop it via ReapExhaustedOrphanRetries.
	store.SeedOrphanRetry("raw/exhausted.eml", 10)

	sc := New(rawobject.NewStore(obj, store), store, config.Storage{RetentionBatchSize: 10}, zerolog.Nop())
	sc.sweep(ctx)

	if got := store.OrphanRetryAttempts("raw/exhausted.eml"); got != -1 {
		t.Fatalf("exhausted key (attempts>=cap) should be reaped, got attempts=%d", got)
	}
}

// TestScannerRetriesPersistedAcrossScanners verifies a retry enqueued by a
// prior (crashed) process is still visible to a brand-new scanner backed by the
// same store — the core benefit of moving off the in-memory failedKeys map.
func TestScannerRetriesPersistedAcrossScanners(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	key := "raw/stuck.eml"
	// Blob exists but is referenced by an active ingest job, so Release reports
	// StillReferenced and the scanner clears the retry entry.
	store.CreateIngestJob(ctx, &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  key,
		State:         "pending",
		NextAttemptAt: time.Now().Add(time.Hour),
	}, nil)
	if err := obj.Put(ctx, key, bytes.NewBufferString(key), 0); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	// Simulate a prior process that enqueued a retry, then crashed.
	store.SeedOrphanRetry(key, 2)

	// A fresh scanner over the same persisted queue must still see the key.
	sc := New(rawobject.NewStore(obj, store), store, config.Storage{RetentionBatchSize: 10}, zerolog.Nop())
	sc.sweep(ctx)

	if got := store.OrphanRetryAttempts(key); got != -1 {
		t.Fatalf("persisted orphan retry should be cleared by the new scanner (StillReferenced), got attempts=%d", got)
	}
}

func TestScannerSweepPurgesOldDoneIngestJobsAndDeletesOrphanObject(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	key := "raw/orphan.eml"
	job := &models.IngestJob{
		ID:            uuid.New(),
		RawObjectKey:  key,
		State:         "done",
		NextAttemptAt: time.Now().Add(-time.Hour),
	}
	if err := store.CreateIngestJob(ctx, job, nil); err != nil {
		t.Fatalf("seed ingest job: %v", err)
	}
	store.ForceIngestJobUpdatedAt(job.ID, time.Now().Add(-(ingestJobRetention + time.Hour)))
	if err := obj.Put(ctx, key, bytes.NewBufferString(key), 0); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	sc := New(rawobject.NewStore(obj, store), store, config.Storage{RetentionBatchSize: 10}, zerolog.Nop())
	sc.sweep(ctx)

	if _, err := obj.Get(ctx, key); err == nil {
		t.Fatalf("expected orphan object %s to be deleted after old ingest job purge", key)
	}
}
