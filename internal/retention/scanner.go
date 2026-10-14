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
	// Atomically deletes expired messages and returns affected object keys so
	// the scanner can release the raw objects through the reference protocol.
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
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	defer func() { metrics.ObserveRetentionSweepDuration(time.Since(started)) }()

	now := time.Now()
	total := 0

	if metadata, ok := sc.store.(interface{ SweepCompanyMetadata(context.Context) error }); ok {
		if err := metadata.SweepCompanyMetadata(ctx); err != nil {
			sc.logger.Warn().Err(err).Msg("company metadata housekeeping")
		}
	}
	if ctx.Err() != nil {
		return
	}
	sc.retryFailedKeys(ctx)
	if ctx.Err() != nil {
		return
	}
	sc.reapExhausted(ctx)

	for {
		if ctx.Err() != nil {
			break
		}
		n, keys, err := sc.store.DeleteExpiredMessagesReturningKeys(ctx, now, sc.cfg.RetentionBatchSize)
		if err != nil {
			sc.logger.Err(err).Msg("deleting expired messages")
			if ctx.Err() != nil {
				// Preserve any known candidates without treating an error's
				// row count or an uncertain commit as confirmed deletion.
				sc.handoffCancelledKeys(ctx, dedupeStrings(keys))
			}
			break
		}
		total += n
		sc.releaseCommittedKeys(ctx, keys)
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
	if ctx.Err() != nil {
		return
	}
	n, orphanKeys, err := sc.store.PurgeOldIngestJobs(ctx, time.Now().Add(-ingestJobRetention), sc.cfg.RetentionBatchSize)
	if err != nil {
		sc.logger.Warn().Err(err).Msg("purging old ingest jobs")
		if ctx.Err() != nil {
			// A rows error may accompany already-returned candidate keys.
			// Queue only those known keys; do not infer the commit or missing rows.
			sc.handoffCancelledKeys(ctx, dedupeStrings(orphanKeys))
		}
		return
	}
	sc.releaseCommittedKeys(ctx, orphanKeys)
	if n > 0 {
		sc.logger.Info().Int("purged", n).Int("orphan_keys", len(orphanKeys)).Msg("old ingest jobs cleaned up")
	}
}

// After a successful metadata deletion the returned keys are no longer owned
// by that row. Cancellation stops object work, but must first hand the current
// unfinished suffix to the existing durable retry protocol.
func (sc *Scanner) releaseCommittedKeys(ctx context.Context, keys []string) {
	keys = dedupeStrings(keys)
	for i, key := range keys {
		if !sc.deleteObjectIfOrphaned(ctx, key) {
			sc.handoffCancelledKeys(ctx, keys[i:])
			return
		}
	}
}

// This is one bounded handoff attempt for the whole suffix, not a transaction
// with the earlier deletion. Each failed/uncertain write remains visible in the
// log; unreturned keys and crash/commit ambiguity still need the wider recovery
// protocol. Existing EnqueueOrphanRetry attempt/cap semantics are unchanged.
func (sc *Scanner) handoffCancelledKeys(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	handoff, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for i, key := range keys {
		if err := handoff.Err(); err != nil {
			sc.logger.Warn().Err(err).Int("remaining", len(keys)-i).Msg("orphan retry handoff budget exhausted before remaining keys")
			return
		}
		if err := sc.store.EnqueueOrphanRetry(handoff, key); err != nil {
			sc.logger.Warn().Err(err).Str("key", key).Msg("enqueue orphan retry during cancellation")
		}
	}
}

// false means cancellation was observed, including inside an enqueue/clear
// call. A committed batch must retain that current key; the retry-list path
// already has a durable entry and simply stops without another queue mutation.
func (sc *Scanner) deleteObjectIfOrphaned(ctx context.Context, key string) bool {
	if ctx.Err() != nil {
		return false
	}
	out, err := sc.objects.Release(ctx, key)
	// Metrics describe the actual release outcome even if cancellation arrived
	// after it; they do not authorize a subsequent retry write or acknowledgement.
	switch out {
	case rawobject.DeleteFailed:
		metrics.RetentionObjectFailed()
	case rawobject.Deleted:
		metrics.RetentionObjectDeleted()
	}
	if ctx.Err() != nil {
		if err != nil {
			sc.logger.Warn().Err(err).Str("key", key).Msg("object release cancelled; retained for retry")
		}
		return false
	}
	switch out {
	case rawobject.CountFailed:
		sc.logger.Warn().Err(err).Str("key", key).Msg("counting object references")
		sc.enqueueRetry(ctx, key)
	case rawobject.DeleteFailed:
		sc.logger.Warn().Err(err).Str("key", key).Msg("deleting object")
		sc.enqueueRetry(ctx, key)
	case rawobject.Deleted:
		sc.clearRetry(ctx, key)
	default: // StillReferenced or Noop
		sc.clearRetry(ctx, key)
	}
	return ctx.Err() == nil
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
	if ctx.Err() != nil {
		return
	}
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
		if !sc.deleteObjectIfOrphaned(ctx, key) {
			return
		}
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
	if ctx.Err() != nil {
		return
	}
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
