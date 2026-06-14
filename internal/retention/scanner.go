package retention

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/metrics"
	"tabmail/internal/rawobject"
)

type retentionStore interface {
	ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error)
	DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error)
	DeleteExpiredMessagesReturningKeys(ctx context.Context, before time.Time, limit int) (int, []string, error)
	PurgeOldIngestJobs(ctx context.Context, before time.Time, limit int) (int, []string, error)
	EnqueueOrphanRetry(ctx context.Context, key string) error
	ListPendingOrphanRetries(ctx context.Context, limit int) ([]string, error)
	ClearOrphanRetry(ctx context.Context, key string) error
	ReapExhaustedOrphanRetries(ctx context.Context) (int, error)
}

type Scanner struct {
	objects *rawobject.Store
	store   retentionStore
	cfg     config.Storage
	logger  zerolog.Logger
}

func New(objects *rawobject.Store, s retentionStore, cfg config.Storage, logger zerolog.Logger) *Scanner {
	return &Scanner{
		objects: objects,
		store:   s,
		cfg:     cfg,
		logger:  logger.With().Str("component", "retention").Logger(),
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
	defer func() { metrics.ObserveRetentionSweepDuration(time.Since(started)) }()

	now := time.Now()
	total := 0

	sc.retryFailedKeys(ctx)
	sc.reapExhausted(ctx)

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
	switch out, err := sc.objects.Release(ctx, key); out {
	case rawobject.CountFailed:
		sc.logger.Warn().Err(err).Str("key", key).Msg("counting object references")
		sc.enqueueRetry(ctx, key)
	case rawobject.DeleteFailed:
		metrics.RetentionObjectFailed()
		sc.logger.Warn().Err(err).Str("key", key).Msg("deleting object")
		sc.enqueueRetry(ctx, key)
	case rawobject.Deleted:
		metrics.RetentionObjectDeleted()
		sc.clearRetry(ctx, key)
	default: // StillReferenced or Noop
		sc.clearRetry(ctx, key)
	}
}

func (sc *Scanner) enqueueRetry(ctx context.Context, key string) {
	if err := sc.store.EnqueueOrphanRetry(ctx, key); err != nil {
		sc.logger.Warn().Err(err).Str("key", key).Msg("enqueue orphan retry")
	}
}

func (sc *Scanner) clearRetry(ctx context.Context, key string) {
	if err := sc.store.ClearOrphanRetry(ctx, key); err != nil {
		sc.logger.Warn().Err(err).Str("key", key).Msg("clear orphan retry")
	}
}

func (sc *Scanner) retryFailedKeys(ctx context.Context) {
	keys, err := sc.store.ListPendingOrphanRetries(ctx, sc.cfg.RetentionBatchSize)
	if err != nil {
		sc.logger.Warn().Err(err).Msg("listing pending orphan retries")
		return
	}
	if len(keys) == 0 {
		return
	}
	retried := 0
	for _, key := range keys {
		sc.deleteObjectIfOrphaned(ctx, key)
		retried++
	}
	if retried > 0 {
		sc.logger.Info().Int("retried", retried).Msg("retried previously failed object deletions")
	}
}

// reapExhausted drops retry entries that have hit the attempt cap. Without this
// the orphan_objects table would accumulate zombie rows — keys that are no
// longer retried (filtered out of ListPendingOrphanRetries) yet never cleared.
func (sc *Scanner) reapExhausted(ctx context.Context) {
	n, err := sc.store.ReapExhaustedOrphanRetries(ctx)
	if err != nil {
		sc.logger.Warn().Err(err).Msg("reaping exhausted orphan retries")
		return
	}
	if n > 0 {
		sc.logger.Info().Int("dropped", n).Msg("dropped exhausted orphan object retries")
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
