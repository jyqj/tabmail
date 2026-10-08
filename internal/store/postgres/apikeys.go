package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ================================================================
// Tenant API keys
// ================================================================

// CreateAPIKey is the trusted seed/import entry point. HTTP credential issuers
// use CreateAPIKeyAuthorized, which fences current authority and required audit.
func (s *PgStore) CreateAPIKey(ctx context.Context, k *models.TenantAPIKey) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = insertAPIKeyRows(ctx, tx, k); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertAPIKeyRows(ctx context.Context, tx pgx.Tx, k *models.TenantAPIKey) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	k.CreatedAt = time.Now()
	scopesJSON, err := json.Marshal(k.Scopes)
	if err != nil {
		return err
	}
	var zoneIDs []uuid.UUID
	if len(k.AllowedZoneIDs) > 0 {
		zoneIDs = k.AllowedZoneIDs
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO tenant_api_keys (id,tenant_id,key_hash,key_prefix,label,scopes,owner_user_id,allowed_zone_ids,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		k.ID, k.TenantID, k.KeyHash, k.KeyPrefix, k.Label, scopesJSON, k.OwnerUserID, zoneIDs, k.ExpiresAt, k.CreatedAt)
	if err != nil {
		return err
	}
	// Provision once in authority -> usage order. Touch never creates a missing
	// usage row, including when a detached observation outlives key deletion.
	if _, err = tx.Exec(ctx, `INSERT INTO tenant_api_key_usage(api_key_id) VALUES($1)`, k.ID); err != nil {
		return err
	}
	return nil
}

// CreateAPIKeyAuthorized linearizes issuance with freeze, role/session changes
// and permission writers. Acquire the target tenant reference before the user
// fence, matching tenant-first member administration. A foreign super admin is
// still fenced by their actual user/home identity, never by a target substitute.
func (s *PgStore) CreateAPIKeyAuthorized(ctx context.Context, issuer authz.APIKeyIssuer, key *models.TenantAPIKey) (err error) {
	defer func() {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "55P03" || pg.Code == "40001") {
			err = &app.Error{Kind: app.KindConflict, Message: "API key authority is changing; reload before retrying", Err: err}
		}
	}()
	if key == nil || issuer.Actor.Type != authz.PrincipalUser || issuer.Actor.SessionVersion == nil || issuer.HomeTenantID == uuid.Nil {
		return authz.ErrForbidden("current interactive JWT issuer required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var tenant uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE`, key.TenantID).Scan(&tenant); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.NotFound("tenant not found")
		}
		return err
	}
	current, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 FOR SHARE`, issuer.Actor.ID))
	if err != nil {
		return err
	}
	actor, valid := issuer.Refresh(current, key.TenantID)
	if !valid {
		return authz.ErrForbidden("API key issuer no longer matches the authenticated session")
	}
	if !actor.IsTenantAdmin() {
		actor.Permission, err = effectivePermissionSnapshot(ctx, tx, actor.ID)
		if err != nil {
			return err
		}
	}
	candidate := *key
	candidate.Scopes = append([]string(nil), key.Scopes...)
	candidate.AllowedZoneIDs = append([]uuid.UUID(nil), key.AllowedZoneIDs...)
	if err = authz.ConfigureIssuedAPIKey(actor, &candidate); err != nil {
		return err
	}
	// Profile/zone writers may hold the zone before a user; never wait back on
	// these references while holding that user's authority fence.
	zones := append([]uuid.UUID(nil), candidate.AllowedZoneIDs...)
	sort.Slice(zones, func(i, j int) bool { return zones[i].String() < zones[j].String() })
	for i, id := range zones {
		if i > 0 && zones[i-1] == id {
			continue
		}
		var owner uuid.UUID
		if err = tx.QueryRow(ctx, `SELECT tenant_id FROM domain_zones WHERE id=$1 FOR SHARE NOWAIT`, id).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.BadRequest("zone " + id.String() + " not found")
			}
			return err
		}
		if owner != key.TenantID {
			return authz.ErrForbidden("zone " + id.String() + " does not belong to tenant")
		}
	}
	if err = insertAPIKeyRows(ctx, tx, &candidate); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"label": candidate.Label, "key_prefix": candidate.KeyPrefix, "scopes": candidate.Scopes, "owner_user_id": candidate.OwnerUserID})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,'api_key.create','tenant_api_key',$3,$4)`, candidate.TenantID, actor.AuditLabel(), candidate.ID, details); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	*key = candidate
	return nil
}

// Metadata readers expose a host address string, not PostgreSQL's binary inet
// representation. Keep all three reads on one projection, preserving SQL NULL.
// Select a whole observation, not two independent COALESCEs: NULL IP in an
// existing usage row means this observation's IP is unknown. Legacy columns
// are display-only fallback for a missing usage row, never an authority or a
// reason to recreate usage. Normal create/backfill provisions every row.
const apiKeyMetadataSelect = `SELECT k.id,k.tenant_id,k.key_prefix,k.label,k.scopes,k.owner_user_id,k.allowed_zone_ids,k.expires_at,k.created_at,
 CASE WHEN u.api_key_id IS NULL THEN k.last_used_at ELSE u.last_used_at END AS last_used_at,
 host(CASE WHEN u.api_key_id IS NULL THEN k.last_used_ip ELSE u.last_used_ip END) AS last_used_ip
 FROM tenant_api_keys k LEFT JOIN tenant_api_key_usage u ON u.api_key_id=k.id`

func (s *PgStore) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error) {
	rows, err := s.pool.Query(ctx, apiKeyMetadataSelect+` WHERE k.tenant_id=$1 ORDER BY k.created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

func (s *PgStore) ListAPIKeysByOwner(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error) {
	rows, err := s.pool.Query(ctx, apiKeyMetadataSelect+` WHERE k.tenant_id=$1 AND k.owner_user_id=$2 ORDER BY k.created_at`, tenantID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

func scanAPIKeys(rows pgx.Rows) ([]*models.TenantAPIKey, error) {
	var out []*models.TenantAPIKey
	for rows.Next() {
		k := &models.TenantAPIKey{}
		var scopesJSON []byte
		var ownerID pgtype.UUID
		if err := rows.Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Label,
			&scopesJSON, &ownerID, &k.AllowedZoneIDs, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.LastUsedIP); err != nil {
			return nil, err
		}
		if ownerID.Valid {
			id := uuid.UUID(ownerID.Bytes)
			k.OwnerUserID = &id
		}
		if len(scopesJSON) > 0 {
			if err := json.Unmarshal(scopesJSON, &k.Scopes); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *PgStore) GetAPIKey(ctx context.Context, id uuid.UUID) (*models.TenantAPIKey, error) {
	k := &models.TenantAPIKey{}
	var scopesJSON []byte
	var ownerID pgtype.UUID
	err := s.pool.QueryRow(ctx, apiKeyMetadataSelect+` WHERE k.id=$1`, id).
		Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Label,
			&scopesJSON, &ownerID, &k.AllowedZoneIDs, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.LastUsedIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		uid := uuid.UUID(ownerID.Bytes)
		k.OwnerUserID = &uid
	}
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &k.Scopes); err != nil {
			return nil, err
		}
	}
	return k, nil
}

func (s *PgStore) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tenant_api_keys WHERE id=$1`, id)
	return err
}

func (s *PgStore) ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	h := hashKey(rawKey)
	t := &models.Tenant{}
	var keyID uuid.UUID
	var scopes []string
	var scopesJSON []byte
	var allowedZoneIDs []uuid.UUID
	var ownerUserID pgtype.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT k.id, k.scopes, k.allowed_zone_ids, k.owner_user_id, t.id, t.name, t.plan_id, t.is_super, t.created_at
		FROM tenant_api_keys k
		JOIN tenants t ON t.id = k.tenant_id
		WHERE k.key_hash = $1
		  AND (k.expires_at IS NULL OR k.expires_at > now())`, h).
		Scan(&keyID, &scopesJSON, &allowedZoneIDs, &ownerUserID, &t.ID, &t.Name, &t.PlanID, &t.IsSuper, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, nil, nil, nil, nil, err
		}
	}
	var ownerPtr *uuid.UUID
	if ownerUserID.Valid {
		uid := uuid.UUID(ownerUserID.Bytes)
		ownerPtr = &uid
	}
	return t, &keyID, scopes, allowedZoneIDs, ownerPtr, nil
}

func (s *PgStore) TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error {
	// Observe exactly once on the database clock BEFORE waiting for usage.
	// This is DB observation order, not HTTP start/goroutine scheduling order.
	// Keeping the sample as a parameter also prevents a lock wait or EvalPlanQual
	// retry from making an old observation appear new. No authority row is read.
	var observed time.Time
	if err := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		return err
	}
	return s.touchAPIKeyObservation(ctx, id, observed, ip)
}

const apiKeyUsageUpdateSQL = `UPDATE tenant_api_key_usage SET last_used_at=$2,last_used_ip=$3 WHERE api_key_id=$1 AND (last_used_at IS NULL OR last_used_at<$2)`

func (s *PgStore) touchAPIKeyObservation(ctx context.Context, id uuid.UUID, observed time.Time, ip string) error {
	var address any
	if ip != "" {
		address = ip
	}
	// A tie preserves the existing entire pair. Empty IP replaces old IP with
	// NULL; carrying it forward would splice two observations. Clock rollback
	// samples are ignored, not clamped into fabricated timestamp/IP pairs.
	// UPDATE-only means deleted/missing usage cannot be resurrected. The FK key
	// is never changed; steady-state Touch has no reverse authority lock edge.
	_, err := s.pool.Exec(ctx, apiKeyUsageUpdateSQL, id, observed, address)
	return err
}
