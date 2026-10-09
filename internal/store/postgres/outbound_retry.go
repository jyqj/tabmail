package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

var _ store.AtomicOutboundRetry = (*PgStore)(nil)

// The app policy uses only the transaction reader, never the connection pool.
// Tenant SHARE precedes dependencies and the job. It protects tenant policy and
// required audit FK while allowing independent retries to share the parent.
func (s *PgStore) RequeueOutboundJobAuthorized(ctx context.Context, a authz.Actor, observed *models.OutboundJob, validate store.OutboundRetryValidator) (out *models.OutboundJob, err error) {
	if validate == nil || observed == nil || observed.ID == uuid.Nil || a.TenantID != observed.TenantID {
		return nil, app.Forbidden("retry validation unavailable")
	}
	defer func() {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "40001") {
			err = app.Conflict("retry authority or state changed; reload before retrying")
		}
		if err != nil {
			out = nil
		}
	}()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var tenant uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR SHARE`, a.TenantID).Scan(&tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, app.NotFound("company not found")
	}
	if err != nil {
		return nil, err
	}
	// A missing mailbox cannot be row-locked. Only verified-identity fallback
	// takes NOWAIT relation protection against mailbox/exact-identity inserts.
	if observed.SenderMailboxID == nil {
		if _, err = tx.Exec(ctx, `LOCK TABLE mailboxes,send_identities IN SHARE MODE NOWAIT`); err != nil {
			return nil, err
		}
	}
	reader := &outboundRetryReader{store: s, tx: tx, tenant: a.TenantID}
	if err = validate(ctx, reader, observed); err != nil {
		return nil, err
	}
	locked, err := scanOutboundJob(tx.QueryRow(ctx, outboundJobSelect+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, observed.ID))
	if err != nil {
		return nil, err
	}
	if locked == nil || locked.State != models.OutboundDead && locked.State != models.OutboundFailed {
		return nil, store.ErrOutboundNotRetryable
	}
	if locked.InFlightDomain != "" {
		return nil, store.ErrOutboundUncertain
	}
	if (locked.SenderMailboxID == nil) != (observed.SenderMailboxID == nil) {
		return nil, store.ErrOutboundNotRetryable
	}
	// Validate the actual locked generation. NOWAIT dependency reads prevent
	// reverse-order waiting if it references different authority than observed.
	if err = validate(ctx, reader, locked); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT state FROM outbound_recipients WHERE tenant_id=$1 AND job_id=$2 ORDER BY address FOR SHARE NOWAIT`, a.TenantID, locked.ID)
	if err != nil {
		return nil, err
	}
	uncertain := false
	for rows.Next() {
		var state string
		if err = rows.Scan(&state); err != nil {
			break
		}
		if state == delivery.Uncertain {
			uncertain = true
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if rowErr != nil {
		return nil, rowErr
	}
	if uncertain {
		return nil, store.ErrOutboundUncertain
	}
	if err = requeueOutboundJob(ctx, tx, locked.ID); err != nil {
		return nil, err
	}
	if err = companyAudit(ctx, tx, a, "outbound.retry", "outbound_job", locked.ID, map[string]any{"previous_state": locked.State, "attempts": locked.Attempts}); err != nil {
		return nil, err
	}
	// UPDATE/audit can wait. Check every observed credential/mailbox deadline
	// against the final database clock; a late failure rolls back all effects.
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	for _, deadline := range reader.deadlines {
		if !deadline.After(now) {
			return nil, app.Forbidden("retry credential or mailbox expired")
		}
	}
	out, err = scanOutboundJob(tx.QueryRow(ctx, outboundJobSelect+` WHERE tenant_id=$1 AND id=$2`, a.TenantID, locked.ID))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

type outboundSQLExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
