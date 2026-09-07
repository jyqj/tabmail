package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/store"
)

var _ store.IngressInspector = (*PgStore)(nil)

// A read-only repeatable snapshot prevents combining a pre-retry job state with
// post-retry targets (ordinary READ COMMITTED starts a new snapshot per query).
func (s *PgStore) InspectIngress(ctx context.Context, id uuid.UUID) (*store.IngressInspection, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	result := &store.IngressInspection{Targets: []store.IngressTarget{}}
	err = tx.QueryRow(ctx, `SELECT id,state,recovery_managed,attempts,COALESCE(last_error,''),created_at,updated_at,next_attempt_at,raw_size
 FROM ingest_jobs WHERE id=$1`, id).Scan(&result.ID, &result.State, &result.RecoveryManaged, &result.Attempts, &result.LastError, &result.CreatedAt, &result.UpdatedAt, &result.NextAttemptAt, &result.ExpectedBytes)
	if err == pgx.ErrNoRows {
		return nil, app.NotFound("receipt not found")
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT job_id,mailbox_id,tenant_id,zone_id,address,state,message_id,attempts,last_error
 FROM ingress_targets WHERE job_id=$1 ORDER BY address,mailbox_id`, id)
	if err != nil {
		return nil, err
	}
	held := 0
	for rows.Next() {
		var target store.IngressTarget
		if err = rows.Scan(&target.JobID, &target.MailboxID, &target.TenantID, &target.ZoneID, &target.Address, &target.State, &target.MessageID, &target.Attempts, &target.LastError); err != nil {
			rows.Close()
			return nil, err
		}
		if target.State == "held" {
			held++
		}
		result.Targets = append(result.Targets, target)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	switch {
	case !result.RecoveryManaged:
		result.RetryBlockReason = "legacy_receipt"
	case len(result.Targets) == 0:
		result.RetryBlockReason = "missing_ledger"
	case result.State != "dead":
		result.RetryBlockReason = "not_held"
	case held == 0:
		result.RetryBlockReason = "no_held_targets"
	default:
		result.CanRetry = true
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PgStore) RetryReviewedIngress(ctx context.Context, id uuid.UUID, actor, reason string, observed time.Time) error {
	if observed.IsZero() {
		return app.BadRequest("observed_updated_at is required")
	}
	return s.retryIngress(ctx, id, actor, reason, &observed)
}
