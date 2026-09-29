package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ store.OutboundContentAuthority = (*PgStore)(nil)

// CanReadOutboundContent is the legacy job/attempt/recipient projection gate.
// The query reads no body: it uses the same sentContentFrom and content scope as
// canonical archive reads. A retained queue job cannot resurrect a purged item.
func (s *PgStore) CanReadOutboundContent(ctx context.Context, a authz.Actor, observed *models.OutboundJob) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if observed == nil || observed.ID == uuid.Nil || observed.TenantID != a.TenantID || observed.SenderMailboxID == nil || !a.Permission.AllowsZone(observed.ZoneID) {
		return false, nil
	}
	job := *observed
	mailbox := *job.SenderMailboxID
	allowed := false
	check := func(tx pgx.Tx, current authz.Actor, keyExpiry *time.Time) error {
		if err := lockMailboxAuthorization(ctx, tx, current.TenantID, mailbox); err != nil {
			return err
		}
		where, args := submissionContentScope(current, 2)
		args = append([]any{job.ID}, args...)
		n := len(args)
		where += fmt.Sprintf(" AND s.id=$1 AND s.zone_id=$%d AND s.sender_mailbox_id=$%d AND ($%d::timestamptz IS NULL OR $%d>clock_timestamp())", n+1, n+2, n+3, n+3)
		args = append(args, job.ZoneID, mailbox, keyExpiry)
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1`+sentContentFrom+` WHERE `+where+`)`, args...).Scan(&allowed)
	}
	var err error
	switch a.Type {
	case authz.PrincipalUser:
		err = s.companyReadTx(ctx, a, false, func(tx pgx.Tx, current authz.Actor) error { return check(tx, current, nil) })
	case authz.PrincipalAPIKey:
		if a.OwnerUserID == nil {
			return false, nil
		}
		err = s.legacyKeyContentTx(ctx, a, &job, check)
	default:
		return false, nil
	}
	if err != nil {
		if v, ok := app.As(app.FromAuthz(err)); ok && (v.Kind == app.KindForbidden || v.Kind == app.KindNotFound) {
			return false, nil
		}
		return false, err
	}
	return allowed, nil
}

// Owner-before-key is compatible with member suspension. The old DeleteUser
// path can instead delete keys first, so key SHARE uses NOWAIT: fail closed
// rather than introduce a user/key lock cycle. This remains a key principal;
// the private user-shaped selector below is only for canonical mailbox scope,
// never for granting interactive administration or bypassing key scope checks.
func (s *PgStore) legacyKeyContentTx(ctx context.Context, a authz.Actor, job *models.OutboundJob, check func(pgx.Tx, authz.Actor, *time.Time) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	uid := *a.OwnerUserID
	u, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2 FOR SHARE`, uid, a.TenantID))
	if err != nil {
		return err
	}
	if u == nil || !u.IsActive {
		return app.Forbidden("key owner unavailable")
	}
	k := &models.TenantAPIKey{}
	var raw json.RawMessage
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,owner_user_id,scopes,allowed_zone_ids,expires_at FROM tenant_api_keys WHERE id=$1 AND tenant_id=$2 FOR SHARE NOWAIT`, a.ID, a.TenantID).Scan(&k.ID, &k.TenantID, &k.OwnerUserID, &raw, &k.AllowedZoneIDs, &k.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Forbidden("key unavailable")
	}
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "55P03" {
			return app.Conflict("key is changing; reload before retrying")
		}
		return err
	}
	if err = json.Unmarshal(raw, &k.Scopes); err != nil {
		return err
	}
	if !authz.OutboundContentKeyMatches(a, k, job) {
		return app.Forbidden("key content authority unavailable")
	}
	permission, err := effectivePermissionSnapshot(ctx, tx, uid)
	if err != nil {
		return err
	}
	current := authz.Actor{Type: authz.PrincipalUser, ID: uid, TenantID: a.TenantID, Permission: permission}
	if err = check(tx, current, k.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
