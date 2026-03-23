package retention

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/store"
)

type retentionStore interface {
	ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error)
	DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error)
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
}

type Scanner struct {
	store  retentionStore
	obj    store.ObjectStore
	cfg    config.Storage
	logger zerolog.Logger
}

func New(s retentionStore, obj store.ObjectStore, cfg config.Storage, logger zerolog.Logger) *Scanner {
	return &Scanner{
		store:  s,
		obj:    obj,
		cfg:    cfg,
		logger: logger.With().Str("component", "retention").Logger(),
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
	now := time.Now()
	total := 0

	for {
		keys, err := sc.store.ListExpiredObjectKeys(ctx, now, sc.cfg.RetentionBatchSize)
		if err != nil {
			sc.logger.Err(err).Msg("listing expired object keys")
			break
		}

		n, err := sc.store.DeleteExpiredMessages(ctx, now, sc.cfg.RetentionBatchSize)
		if err != nil {
			sc.logger.Err(err).Msg("deleting expired messages")
			break
		}
		for _, key := range dedupeStrings(keys) {
			refs, err := sc.store.CountMessagesByObjectKey(ctx, key)
			if err != nil {
				sc.logger.Warn().Err(err).Str("key", key).Msg("counting object references after retention delete")
				continue
			}
			if refs == 0 {
				if err := sc.obj.Delete(ctx, key); err != nil {
					sc.logger.Warn().Err(err).Str("key", key).Msg("deleting object")
				}
			}
		}
		total += n
		if n < sc.cfg.RetentionBatchSize {
			break
		}
	}

	if total > 0 {
		sc.logger.Info().Int("deleted", total).Msg("retention sweep complete")
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
