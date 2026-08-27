package postgres

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/config"
)

type PgStore struct {
	pool *pgxpool.Pool
}

// schemaSQL is the current database schema snapshot. TabMail is not online yet,
// so we intentionally avoid versioned database migrations and initialize the
// expected schema directly when the store starts.
//
//go:embed schema.sql
var schemaSQL string

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
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: initialize schema: %w", err)
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
