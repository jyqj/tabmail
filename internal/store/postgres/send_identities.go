package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error {
	if si.ID == uuid.Nil {
		si.ID = uuid.New()
	}
	si.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO send_identities (id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		si.ID, si.TenantID, si.ZoneID, si.MailboxID, si.Address, si.IdentityType, si.Verified, si.CreatedAt)
	return err
}

func (s *PgStore) GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error) {
	si := &models.SendIdentity{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at
		FROM send_identities WHERE id = $1`, id).
		Scan(&si.ID, &si.TenantID, &si.ZoneID, &si.MailboxID, &si.Address, &si.IdentityType, &si.Verified, &si.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return si, err
}

func (s *PgStore) ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at
		FROM send_identities WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SendIdentity
	for rows.Next() {
		si := &models.SendIdentity{}
		if err := rows.Scan(&si.ID, &si.TenantID, &si.ZoneID, &si.MailboxID, &si.Address, &si.IdentityType, &si.Verified, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *PgStore) ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at
		FROM send_identities WHERE zone_id = $1 ORDER BY created_at`, zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SendIdentity
	for rows.Next() {
		si := &models.SendIdentity{}
		if err := rows.Scan(&si.ID, &si.TenantID, &si.ZoneID, &si.MailboxID, &si.Address, &si.IdentityType, &si.Verified, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *PgStore) FindSendIdentityForAddress(ctx context.Context, tenantID uuid.UUID, address string) (*models.SendIdentity, error) {
	// Try exact match first.
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at
		FROM send_identities
		WHERE tenant_id = $1 AND address = $2 AND identity_type = 'exact'`, tenantID, address)
	si := &models.SendIdentity{}
	err := row.Scan(&si.ID, &si.TenantID, &si.ZoneID, &si.MailboxID, &si.Address, &si.IdentityType, &si.Verified, &si.CreatedAt)
	if err == nil {
		return si, nil
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}
	// Try domain_wildcard: extract domain from address.
	idx := strings.LastIndex(address, "@")
	if idx < 0 {
		return nil, nil
	}
	domain := address[idx+1:]
	wildcardAddr := "*@" + domain
	row = s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, zone_id, mailbox_id, address, identity_type, verified, created_at
		FROM send_identities
		WHERE tenant_id = $1 AND address = $2 AND identity_type = 'domain_wildcard'`, tenantID, wildcardAddr)
	si = &models.SendIdentity{}
	err = row.Scan(&si.ID, &si.TenantID, &si.ZoneID, &si.MailboxID, &si.Address, &si.IdentityType, &si.Verified, &si.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return si, nil
}

func (s *PgStore) UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE send_identities SET verified = $1 WHERE zone_id = $2`, verified, zoneID)
	return err
}

func (s *PgStore) DeleteSendIdentity(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM send_identities WHERE id = $1`, id)
	return err
}
