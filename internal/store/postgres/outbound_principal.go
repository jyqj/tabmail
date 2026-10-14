package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// outboundPrincipalTx protects the current identity shared by legacy receipt
// and content readers. User-owned keys stay keys, including when their owner is
// an administrator. Ownerless keys can have receipts but never mailbox content.
func (s *PgStore) outboundPrincipalTx(ctx context.Context, a authz.Actor, scopes []string, read func(pgx.Tx, authz.Actor, *models.TenantAPIKey) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Type == authz.PrincipalUser {
		return s.companyReadTx(ctx, a, false, func(tx pgx.Tx, current authz.Actor) error { return read(tx, current, nil) })
	}
	if a.Type != authz.PrincipalAPIKey {
		return app.Forbidden("current outbound principal required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	current := authz.Actor{Type: authz.PrincipalAPIKey, ID: a.ID, TenantID: a.TenantID, OwnerUserID: a.OwnerUserID, TenantWide: a.OwnerUserID == nil}
	if a.OwnerUserID != nil {
		u, e := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2 FOR SHARE`, *a.OwnerUserID, a.TenantID))
		if e != nil {
			return e
		}
		if u == nil || !u.IsActive {
			return app.Forbidden("key owner unavailable")
		}
	}
	key := &models.TenantAPIKey{}
	var raw json.RawMessage
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,owner_user_id,scopes,allowed_zone_ids,expires_at FROM tenant_api_keys WHERE id=$1 AND tenant_id=$2 FOR SHARE NOWAIT`, a.ID, a.TenantID).Scan(&key.ID, &key.TenantID, &key.OwnerUserID, &raw, &key.AllowedZoneIDs, &key.ExpiresAt)
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
	if err = json.Unmarshal(raw, &key.Scopes); err != nil {
		return err
	}
	if !authz.OutboundKeyIdentityMatches(a, key) || !authz.OutboundKeyHasScope(key, scopes...) {
		return app.Forbidden("current key scope or identity unavailable")
	}
	if current.OwnerUserID != nil {
		current.Permission, err = effectivePermissionSnapshot(ctx, tx, *current.OwnerUserID)
		if err != nil {
			return err
		}
	}
	if err = read(tx, current, key); err != nil {
		return err
	}
	// A statement can wait after authentication. Do not publish even an empty
	// successful receipt page under an already-expired credential.
	if key.ExpiresAt != nil {
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if !key.ExpiresAt.After(now) {
			return app.Forbidden("key expired")
		}
	}
	return tx.Commit(ctx)
}
