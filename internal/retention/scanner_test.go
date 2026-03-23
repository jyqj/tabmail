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

	sc := New(store, obj, config.Storage{
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
