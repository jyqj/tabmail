package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"tabmail/internal/models"
)

func (s *PgStore) CreateAPIKey(ctx context.Context, k *models.TenantAPIKey) error {
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
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tenant_api_keys (id,tenant_id,key_hash,key_prefix,label,scopes,owner_user_id,allowed_zone_ids,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		k.ID, k.TenantID, k.KeyHash, k.KeyPrefix, k.Label, scopesJSON, k.OwnerUserID, zoneIDs, k.ExpiresAt, k.CreatedAt)
	return err
}

func (s *PgStore) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,key_prefix,label,scopes,owner_user_id,allowed_zone_ids,expires_at,created_at,last_used_at,last_used_ip
		FROM tenant_api_keys WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

func (s *PgStore) ListAPIKeysByOwner(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,key_prefix,label,scopes,owner_user_id,allowed_zone_ids,expires_at,created_at,last_used_at,last_used_ip
		FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2 ORDER BY created_at`, tenantID, ownerUserID)
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
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,key_prefix,label,scopes,owner_user_id,allowed_zone_ids,expires_at,created_at,last_used_at,last_used_ip
		FROM tenant_api_keys WHERE id=$1`, id).
		Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Label,
			&scopesJSON, &ownerID, &k.AllowedZoneIDs, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt, &k.LastUsedIP)
	if err == pgx.ErrNoRows {
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
	if err == pgx.ErrNoRows {
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
	if ip == "" {
		_, err := s.pool.Exec(ctx,
			`UPDATE tenant_api_keys SET last_used_at=now() WHERE id=$1`, id)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tenant_api_keys SET last_used_at=now(), last_used_ip=$2 WHERE id=$1`, id, ip)
	return err
}
