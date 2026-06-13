package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) CreateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (id,tenant_id,url,secret,event_types,is_active,created_by,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ep.ID, ep.TenantID, ep.URL, ep.Secret, ep.EventTypes, ep.IsActive, ep.CreatedBy, ep.CreatedAt, ep.UpdatedAt)
	return err
}

func (s *PgStore) ListWebhookEndpoints(ctx context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,url,event_types,is_active,created_by,created_at,updated_at
		FROM webhook_endpoints WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.WebhookEndpoint
	for rows.Next() {
		ep := &models.WebhookEndpoint{}
		if err := rows.Scan(&ep.ID, &ep.TenantID, &ep.URL, &ep.EventTypes, &ep.IsActive, &ep.CreatedBy, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *PgStore) GetWebhookEndpoint(ctx context.Context, id uuid.UUID) (*models.WebhookEndpoint, error) {
	ep := &models.WebhookEndpoint{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,url,event_types,is_active,created_by,created_at,updated_at
		FROM webhook_endpoints WHERE id=$1`, id).
		Scan(&ep.ID, &ep.TenantID, &ep.URL, &ep.EventTypes, &ep.IsActive, &ep.CreatedBy, &ep.CreatedAt, &ep.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return ep, err
}

func (s *PgStore) UpdateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_endpoints SET url=$2, event_types=$3, is_active=$4, updated_at=now()
		WHERE id=$1`, ep.ID, ep.URL, ep.EventTypes, ep.IsActive)
	return err
}

func (s *PgStore) DeleteWebhookEndpoint(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id=$1`, id)
	return err
}
