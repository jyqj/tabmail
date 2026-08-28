package postgres

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/config"
)

type PgStore struct {
	pool *pgxpool.Pool
}

// dbRunner is the common subset implemented by *pgxpool.Pool and pgx.Tx.
// Repository methods resolve it from the request context, which lets the same
// narrow store interfaces participate in a Unit of Work without exposing pgx
// types to the application layer.
type dbRunner interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type transactionContextKey struct{}

// schemaSQL is the current database schema snapshot. TabMail is not online yet,
// so we intentionally avoid versioned database migrations and initialize the
// expected schema directly when the store starts.
//
//go:embed schema.sql
var schemaSQL string

const (
	claimLeaseDuration = 5 * time.Minute
	rollbackTimeout    = 5 * time.Second
)

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

// WithinTx executes fn in a PostgreSQL transaction. The transaction is carried
// only by the supplied context; repository methods called with another context
// deliberately fall back to the pool and therefore do not accidentally join
// the Unit of Work.
//
// Nested calls are supported: pgx implements nested transactions as savepoints.
// A panic or error rolls back the current transaction/savepoint before being
// propagated. Commit errors are returned to the caller so application services
// never report success before the database has durably committed.
func (s *PgStore) WithinTx(ctx context.Context, fn func(context.Context) error) (err error) {
	if s == nil || s.pool == nil {
		return errors.New("postgres: transaction store is not initialized")
	}
	if fn == nil {
		return errors.New("postgres: transaction callback is required")
	}

	tx, err := s.db(ctx).Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}
	txCtx := context.WithValue(ctx, transactionContextKey{}, tx)

	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
		if recovered := recover(); recovered != nil {
			panic(recovered)
		}
	}()

	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *PgStore) db(ctx context.Context) dbRunner {
	if ctx != nil {
		if tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx); ok && tx != nil {
			return tx
		}
	}
	return s.pool
}

func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
