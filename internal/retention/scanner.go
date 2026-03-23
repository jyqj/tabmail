package retention

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/metrics"
	"tabmail/internal/store"
)

type retentionStore interface {
	ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error)
	DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error)
	DeleteExpiredMessagesReturningKeys(ctx context.Context, before time.Time, limit int) (int, []string, error)
	CountRawObjectReferences(ctx context.Context, objectKey string) (int, error)
	PurgeOldIngestJobs(ctx context.Context, before time.Time, limit int) (int, []string, error)
}

type Scanner struct {
	store      retentionStore
	obj        store.ObjectStore
	cfg        config.Storage
	logger     zerolog.Logger
	failedKeys map[string]struct{}
}

func New(s retentionStore, obj store.ObjectStore, cfg config.Storage, logger zerolog.Logger) *Scanner {
	return &Scanner{
		store:      s,
		obj:        obj,
		cfg:        cfg,
		logger:     logger.With().Str("component", "retention").Logger(),
		failedKeys: make(map[string]struct{}),
	}
}

// Run blocks until ctx is cancelled, scanning at configured intervals.
func (sc *Scanner) Run(ctx context.Context) {
	ticker := time.NewTicker(sc.cfg.RetentionScanInterval)
	defer ticker.Stop()

	sc.logger.Info().
		Dur("interval", sc.cfg.RetentionScanInterval).
		Int("batch", sc.cfg.RetentionBatchSize).
		Msg("retention scanner started")

	for {
		select {
		case <-ctx.Done():
			sc.logger.Info().Msg("retention scanner stopped")
			return
		case <-ticker.C:
			sc.sweep(ctx)
		}
	}
}

func (sc *Scanner) sweep(ctx context.Context) {
	started := time.Now()
	defer metrics.ObserveRetentionSweepDuration(time.Since(started))

	now := time.Now()
	total := 0

	sc.retryFailedKeys(ctx)

	for {
		n, keys, err := sc.store.DeleteExpiredMessagesReturningKeys(ctx, now, sc.cfg.RetentionBatchSize)
		if err != nil {
			sc.logger.Err(err).Msg("deleting expired messages")
			break
		}
		for _, key := range dedupeStrings(keys) {
			sc.deleteObjectIfOrphaned(ctx, key)
		}
		total += n
		if n < sc.cfg.RetentionBatchSize {
			break
		}
	}

	if total > 0 {
		metrics.RetentionMessagesDeleted(total)
		sc.logger.Info().Int("deleted", total).Msg("retention sweep complete")
	}

	sc.purgeIngestJobs(ctx)
}

const ingestJobRetention = 7 * 24 * time.Hour

func (sc *Scanner) purgeIngestJobs(ctx context.Context) {
	n, orphanKeys, err := sc.store.PurgeOldIngestJobs(ctx, time.Now().Add(-ingestJobRetention), sc.cfg.RetentionBatchSize)
	if err != nil {
		sc.logger.Warn().Err(err).Msg("purging old ingest jobs")
		return
	}
	for _, key := range dedupeStrings(orphanKeys) {
		sc.deleteObjectIfOrphaned(ctx, key)
	}
	if n > 0 {
		sc.logger.Info().Int("purged", n).Int("orphan_keys", len(orphanKeys)).Msg("old ingest jobs cleaned up")
	}
}

func (sc *Scanner) deleteObjectIfOrphaned(ctx context.Context, key string) {
	refs, err := sc.store.CountRawObjectReferences(ctx, key)
	if err != nil {
		sc.logger.Warn().Err(err).Str("key", key).Msg("counting object references")
		sc.failedKeys[key] = struct{}{}
		return
	}
	if refs > 0 {
		delete(sc.failedKeys, key)
		return
	}
	if err := sc.obj.Delete(ctx, key); err != nil {
		metrics.RetentionObjectFailed()
		sc.logger.Warn().Err(err).Str("key", key).Msg("deleting object")
		sc.failedKeys[key] = struct{}{}
		return
	}
	metrics.RetentionObjectDeleted()
	delete(sc.failedKeys, key)
}

func (sc *Scanner) retryFailedKeys(ctx context.Context) {
	if len(sc.failedKeys) == 0 {
		return
	}
	retried := 0
	for key := range sc.failedKeys {
		sc.deleteObjectIfOrphaned(ctx, key)
		retried++
	}
	if retried > 0 {
		sc.logger.Info().Int("retried", retried).Int("remaining", len(sc.failedKeys)).Msg("retried previously failed object deletions")
	}
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
