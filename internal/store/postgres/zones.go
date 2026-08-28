package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"tabmail/internal/models"
)

func (s *PgStore) CreateZone(ctx context.Context, z *models.DomainZone) error {
	if z.ID == uuid.Nil {
		z.ID = uuid.New()
	}
	if z.Visibility == "" {
		z.Visibility = models.VisibilityPrivate
	}
	z.CreatedAt = time.Now()
	_, err := s.db(ctx).Exec(ctx, `
		INSERT INTO domain_zones (id,tenant_id,owner_user_id,parent_zone_id,domain,visibility,
			allow_random_subdomains,is_verified,mx_verified,txt_record,dkim_private_key_pem,dkim_selector,dkim_enabled,dkim_required_for_send,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		z.ID, z.TenantID, z.OwnerUserID, z.ParentZoneID, z.Domain, z.Visibility,
		z.AllowRandomSubdomains, z.IsVerified, z.MXVerified, z.TXTRecord, z.DKIMPrivateKeyPEM, z.DKIMSelector, z.DKIMEnabled, z.DKIMRequiredForSend, z.CreatedAt)
	return err
}

const zoneSelect = `SELECT id,tenant_id,owner_user_id,parent_zone_id,domain,visibility,
	allow_random_subdomains,is_verified,mx_verified,txt_record,dkim_private_key_pem,dkim_selector,dkim_enabled,dkim_required_for_send,created_at,verified_at
	FROM domain_zones`

func scanZone(row pgx.Row) (*models.DomainZone, error) {
	z := &models.DomainZone{}
	var ownerID pgtype.UUID
	var parentID pgtype.UUID
	err := row.Scan(&z.ID, &z.TenantID, &ownerID, &parentID, &z.Domain, &z.Visibility,
		&z.AllowRandomSubdomains, &z.IsVerified, &z.MXVerified, &z.TXTRecord, &z.DKIMPrivateKeyPEM, &z.DKIMSelector, &z.DKIMEnabled, &z.DKIMRequiredForSend, &z.CreatedAt, &z.VerifiedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		id := uuid.UUID(ownerID.Bytes)
		z.OwnerUserID = &id
	}
	if parentID.Valid {
		id := uuid.UUID(parentID.Bytes)
		z.ParentZoneID = &id
	}
	if z.Visibility == "" {
		z.Visibility = models.VisibilityPrivate
	}
	return z, nil
}

func scanZones(rows pgx.Rows) ([]*models.DomainZone, error) {
	defer rows.Close()
	var out []*models.DomainZone
	for rows.Next() {
		z, err := scanZone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *PgStore) GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error) {
	return scanZone(s.db(ctx).QueryRow(ctx, zoneSelect+` WHERE id=$1`, id))
}

func (s *PgStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	return scanZone(s.db(ctx).QueryRow(ctx, zoneSelect+` WHERE domain=$1`, domain))
}

func (s *PgStore) ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	rows, err := s.db(ctx).Query(ctx, zoneSelect+` WHERE tenant_id=$1 ORDER BY domain`, tenantID)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) ListAllZones(ctx context.Context) ([]*models.DomainZone, error) {
	rows, err := s.db(ctx).Query(ctx, zoneSelect+` ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) ListPublicZones(ctx context.Context) ([]*models.DomainZone, error) {
	rows, err := s.db(ctx).Query(ctx, zoneSelect+` WHERE visibility='public' ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) ListZonesByVisibilities(ctx context.Context, visibilities []models.ResourceVisibility) ([]*models.DomainZone, error) {
	if len(visibilities) == 0 {
		return nil, nil
	}
	vals := make([]string, len(visibilities))
	for i, v := range visibilities {
		vals[i] = string(v)
	}
	rows, err := s.db(ctx).Query(ctx, zoneSelect+` WHERE visibility = ANY($1) ORDER BY domain`, vals)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) UpdateZone(ctx context.Context, z *models.DomainZone) error {
	if z.Visibility == "" {
		z.Visibility = models.VisibilityPrivate
	}
	_, err := s.db(ctx).Exec(ctx, `
		UPDATE domain_zones SET owner_user_id=$2, parent_zone_id=$3, visibility=$4,
			allow_random_subdomains=$5, is_verified=$6, mx_verified=$7, txt_record=$8, verified_at=$9,
			dkim_enabled=$10, dkim_required_for_send=$11
		WHERE id=$1`, z.ID, z.OwnerUserID, z.ParentZoneID, z.Visibility,
		z.AllowRandomSubdomains, z.IsVerified, z.MXVerified, z.TXTRecord, z.VerifiedAt,
		z.DKIMEnabled, z.DKIMRequiredForSend)
	return err
}

func (s *PgStore) DeleteZone(ctx context.Context, id uuid.UUID) error {
	_, err := s.db(ctx).Exec(ctx, `DELETE FROM domain_zones WHERE id=$1`, id)
	return err
}

func (s *PgStore) CountZones(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM domain_zones WHERE tenant_id=$1`, tenantID).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllZones(ctx context.Context) (int, error) {
	var n int
	err := s.db(ctx).QueryRow(ctx, `SELECT count(*) FROM domain_zones`).Scan(&n)
	return n, err
}
