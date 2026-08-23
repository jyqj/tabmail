package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"tabmail/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgStore struct {
	pool *pgxpool.Pool
}

const claimLeaseDuration = 5 * time.Minute

func New(ctx context.Context, cfg config.DB) (*PgStore, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	if err := Migrate(ctx, poolCfg.ConnConfig); err != nil {
		pool.Close()
		return nil, err
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Close() error {
	s.pool.Close()
	return nil
}

func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
