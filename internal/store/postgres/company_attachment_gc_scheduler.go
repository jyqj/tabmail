package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// One acquired scheduler round returns at most 20 distinct tenant candidates;
// each unchanged inner transaction returns/locks/deletes at most 100 attachment
// candidates. Neither number bounds physical SQL rows inspected. With a stable
// eligible tenant set, a full circle takes ceil(N/20) completed rounds. An
// interrupted round advances only its attempted prefix. Unbounded arrival of
// NEW tenant IDs ahead of the cursor has no fixed-round service guarantee.
func (s *PgStore) sweepCompanyAttachmentTenants(ctx context.Context) (result error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// A separate short-lived session also works with pool MaxConns=1. Never put
	// a session advisory lock back into a shared pool. Cursor commits and every
	// inner transaction use THIS session: losing its backend also aborts its GC.
	cfg := s.pool.Config().ConnConfig.Copy()
	if cfg.ConnectTimeout == 0 || cfg.ConnectTimeout > 5*time.Second {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = make(map[string]string)
	}
	cfg.RuntimeParams["application_name"] = "tabmail-attachment-gc"
	cfg.RuntimeParams["statement_timeout"] = "5000"
	// Bound DNS plus all host fallbacks as ONE connect operation, not a fresh
	// ConnectTimeout allowance per host. A shorter caller/config deadline wins.
	connectCtx, connectCancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	conn, err := pgx.ConnectConfig(connectCtx, cfg)
	connectCancel()
	if err != nil {
		return fmt.Errorf("attachment GC scheduler connect: %w", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := conn.Close(closeCtx); err != nil {
			result = errors.Join(result, fmt.Errorf("attachment GC scheduler close: %w", err))
		}
	}()
	var acquired bool
	// Dedicated two-int namespace ('TMGC', 1), distinct from existing one-int
	// company/reference advisory locks. Busy is a skipped round, not GC proof.
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(1414350659,1)`).Scan(&acquired); err != nil {
		return fmt.Errorf("attachment GC scheduler lock: %w", err)
	}
	if !acquired {
		return nil
	}
	var cursor *uuid.UUID
	if err = conn.QueryRow(ctx, `SELECT last_tenant FROM company_attachment_gc_cursor WHERE singleton`).Scan(&cursor); err != nil {
		return fmt.Errorf("attachment GC scheduler cursor: %w", err)
	}
	// Both halves are bounded, disjoint keysets. An empty tail wraps in this
	// same round; an empty entire set preserves the cursor for future writes.
	rows, err := conn.Query(ctx, `WITH tail AS MATERIALIZED (
 SELECT t.id FROM tenants t WHERE ($1::uuid IS NULL OR t.id>$1)
 AND EXISTS(SELECT 1 FROM mail_attachments a WHERE a.tenant_id=t.id AND a.expires_at<now())
 ORDER BY t.id LIMIT 20
), head AS (
 SELECT t.id FROM tenants t WHERE t.id<=$1
 AND EXISTS(SELECT 1 FROM mail_attachments a WHERE a.tenant_id=t.id AND a.expires_at<now())
 ORDER BY t.id LIMIT (20-(SELECT count(*) FROM tail))
)
SELECT id FROM (SELECT id,0 AS half FROM tail UNION ALL SELECT id,1 AS half FROM head) page ORDER BY half,id`, cursor)
	if err != nil {
		return fmt.Errorf("attachment GC scheduler candidates: %w", err)
	}
	tenants := make([]uuid.UUID, 0, 20)
	for rows.Next() {
		var tenant uuid.UUID
		if err = rows.Scan(&tenant); err != nil {
			rows.Close()
			return fmt.Errorf("attachment GC scheduler candidate: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("attachment GC scheduler candidates: %w", err)
	}
	for _, tenant := range tenants {
		// An acknowledged autocommit must precede the attempt, even if it will
		// find only protected/busy rows or fail. Lost commit acknowledgement is
		// NOT permission to enter GC; a restart resumes whatever PostgreSQL kept.
		tag, err := conn.Exec(ctx, `UPDATE company_attachment_gc_cursor SET last_tenant=$1 WHERE singleton`, tenant)
		if err != nil {
			return errors.Join(result, fmt.Errorf("attachment GC scheduler advance: %w", err))
		}
		if tag.RowsAffected() != 1 {
			return errors.Join(result, errors.New("attachment GC scheduler cursor is missing"))
		}
		if err = sweepCompanyAttachmentsWith(ctx, conn, tenant); err != nil {
			result = errors.Join(result, fmt.Errorf("attachment GC tenant %s: %w", tenant, err))
			if conn.IsClosed() || ctx.Err() != nil {
				return result
			}
		}
	}
	return result
}
