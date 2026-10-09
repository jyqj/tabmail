package postgres

import (
	"context"

	"github.com/google/uuid"
	"tabmail/internal/app"
)

// SentRecipientSnapshotBackfillV1Result is an explicit resumable maintenance
// result. Persist AfterAssetID after success; nil means no candidate was scanned.
// Scanned counts unknown assets, not just reliable matches, so an unrecoverable
// prefix cannot starve later assets. Restarting at nil is safe and has no effect
// on already-complete snapshots. It is not a receipt or delivery-ledger marker.
type SentRecipientSnapshotBackfillV1Result struct {
	Scanned      int
	Completed    int
	AfterAssetID *uuid.UUID
}

// BackfillSentRecipientSnapshotsV1 is a tenant-bounded, versioned keyset command,
// not a GET side effect. Only exact original structural source can complete a
// legacy asset. Missing/mismatched sources remain legacy_unknown indefinitely.
// An operator should exhaust this cursor before deleting legacy delivery jobs.
func (s *PgStore) BackfillSentRecipientSnapshotsV1(ctx context.Context, tenant uuid.UUID, after *uuid.UUID, limit int) (SentRecipientSnapshotBackfillV1Result, error) {
	result := SentRecipientSnapshotBackfillV1Result{}
	if tenant == uuid.Nil || limit < 1 || limit > 1000 {
		return result, app.BadRequest("tenant and batch size 1..1000 required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	// Both complete capture and source matching occur in one statement/transaction.
	// SHARE fences source jobs against cleanup or mutation while matching. Asset
	// UPDATE rechecks unknown state, and the immutable trigger independently checks
	// source provenance and forbids any change to original content columns.
	const query = `WITH candidates AS MATERIALIZED(
  SELECT id FROM sent_mail_assets
   WHERE tenant_id=$1 AND recipient_completeness='legacy_unknown'
    AND ($2::uuid IS NULL OR id>$2) ORDER BY id LIMIT $3
 ), reliable AS MATERIALIZED(
  SELECT a.id,j.bcc_addrs FROM candidates c
   JOIN sent_mail_assets a ON a.tenant_id=$1 AND a.id=c.id
   JOIN outbound_jobs j ON j.tenant_id=a.tenant_id AND j.id=a.id
   WHERE sent_recipient_source_matches_v1(a,j) FOR SHARE OF j
 ), completed AS(
  UPDATE sent_mail_assets a SET bcc_addrs=r.bcc_addrs,
   recipient_completeness='complete',recipient_snapshot_version=1
   FROM reliable r WHERE a.tenant_id=$1 AND a.id=r.id
    AND a.recipient_completeness='legacy_unknown' RETURNING a.id
 ) SELECT (SELECT count(*) FROM candidates),(SELECT count(*) FROM completed),
  (SELECT id FROM candidates ORDER BY id DESC LIMIT 1)`
	if err = tx.QueryRow(ctx, query, tenant, after, limit).Scan(&result.Scanned, &result.Completed, &result.AfterAssetID); err != nil {
		return SentRecipientSnapshotBackfillV1Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SentRecipientSnapshotBackfillV1Result{}, err
	}
	return result, nil
}
