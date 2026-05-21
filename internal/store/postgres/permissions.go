package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

// ================================================================
// Permission profiles
// ================================================================

func (s *PgStore) CreatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO permission_profiles (id, name, description, can_send, daily_send_quota, daily_receive_quota,
			max_mailboxes, max_domains, allowed_zone_ids, can_create_domains, can_create_routes,
			can_create_api_keys, is_system, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		p.ID, p.Name, p.Description, p.CanSend, p.DailySendQuota, p.DailyReceiveQuota,
		p.MaxMailboxes, p.MaxDomains, uuidSliceParam(p.AllowedZoneIDs), p.CanCreateDomains, p.CanCreateRoutes,
		p.CanCreateAPIKeys, p.IsSystem, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *PgStore) GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error) {
	return scanPermProfile(s.pool.QueryRow(ctx, permProfileSelect+` WHERE id=$1`, id))
}

func (s *PgStore) GetPermissionProfileByName(ctx context.Context, name string) (*models.PermissionProfile, error) {
	return scanPermProfile(s.pool.QueryRow(ctx, permProfileSelect+` WHERE name=$1`, name))
}

func (s *PgStore) ListPermissionProfiles(ctx context.Context) ([]*models.PermissionProfile, error) {
	rows, err := s.pool.Query(ctx, permProfileSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.PermissionProfile
	for rows.Next() {
		p, err := scanPermProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PgStore) UpdatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error {
	p.UpdatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE permission_profiles SET name=$2, description=$3, can_send=$4, daily_send_quota=$5,
			daily_receive_quota=$6, max_mailboxes=$7, max_domains=$8, allowed_zone_ids=$9,
			can_create_domains=$10, can_create_routes=$11, can_create_api_keys=$12, updated_at=$13
		WHERE id=$1`,
		p.ID, p.Name, p.Description, p.CanSend, p.DailySendQuota, p.DailyReceiveQuota,
		p.MaxMailboxes, p.MaxDomains, uuidSliceParam(p.AllowedZoneIDs), p.CanCreateDomains, p.CanCreateRoutes,
		p.CanCreateAPIKeys, p.UpdatedAt)
	return err
}

func (s *PgStore) DeletePermissionProfile(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM permission_profiles WHERE id=$1`, id)
	return err
}

const permProfileSelect = `SELECT id, name, description, can_send, daily_send_quota, daily_receive_quota,
	max_mailboxes, max_domains, allowed_zone_ids, can_create_domains, can_create_routes,
	can_create_api_keys, is_system, created_at, updated_at
	FROM permission_profiles`

func scanPermProfile(row pgx.Row) (*models.PermissionProfile, error) {
	p := &models.PermissionProfile{}
	var allowedZones []uuid.UUID
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.CanSend, &p.DailySendQuota, &p.DailyReceiveQuota,
		&p.MaxMailboxes, &p.MaxDomains, &allowedZones, &p.CanCreateDomains, &p.CanCreateRoutes,
		&p.CanCreateAPIKeys, &p.IsSystem, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.AllowedZoneIDs = allowedZones
	return p, nil
}

// ================================================================
// User permission overrides
// ================================================================

func (s *PgStore) UpsertUserPermissionOverride(ctx context.Context, o *models.UserPermissionOverride) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	o.UpdatedAt = time.Now()
	return s.pool.QueryRow(ctx, `
		INSERT INTO user_permission_overrides (id, user_id, can_send, daily_send_quota, daily_receive_quota,
			max_mailboxes, max_domains, allowed_zone_ids, can_create_domains, can_create_routes,
			can_create_api_keys, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (user_id) DO UPDATE SET
			can_send=EXCLUDED.can_send,
			daily_send_quota=EXCLUDED.daily_send_quota,
			daily_receive_quota=EXCLUDED.daily_receive_quota,
			max_mailboxes=EXCLUDED.max_mailboxes,
			max_domains=EXCLUDED.max_domains,
			allowed_zone_ids=EXCLUDED.allowed_zone_ids,
			can_create_domains=EXCLUDED.can_create_domains,
			can_create_routes=EXCLUDED.can_create_routes,
			can_create_api_keys=EXCLUDED.can_create_api_keys,
			updated_at=EXCLUDED.updated_at
		RETURNING id, updated_at`,
		o.ID, o.UserID, o.CanSend, o.DailySendQuota, o.DailyReceiveQuota,
		o.MaxMailboxes, o.MaxDomains, uuidSliceParam(o.AllowedZoneIDs), o.CanCreateDomains, o.CanCreateRoutes,
		o.CanCreateAPIKeys, o.UpdatedAt).
		Scan(&o.ID, &o.UpdatedAt)
}

func (s *PgStore) GetUserPermissionOverride(ctx context.Context, userID uuid.UUID) (*models.UserPermissionOverride, error) {
	o := &models.UserPermissionOverride{}
	var allowedZones []uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, can_send, daily_send_quota, daily_receive_quota,
			max_mailboxes, max_domains, allowed_zone_ids, can_create_domains, can_create_routes,
			can_create_api_keys, updated_at
		FROM user_permission_overrides WHERE user_id=$1`, userID).
		Scan(&o.ID, &o.UserID, &o.CanSend, &o.DailySendQuota, &o.DailyReceiveQuota,
			&o.MaxMailboxes, &o.MaxDomains, &allowedZones, &o.CanCreateDomains, &o.CanCreateRoutes,
			&o.CanCreateAPIKeys, &o.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.AllowedZoneIDs = allowedZones
	return o, nil
}

func (s *PgStore) DeleteUserPermissionOverride(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_permission_overrides WHERE user_id=$1`, userID)
	return err
}

func (s *PgStore) EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	ep := &models.EffectivePermission{}
	var allowedZones []uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(o.can_send,            p.can_send,            FALSE),
			COALESCE(o.daily_send_quota,    p.daily_send_quota,    0),
			COALESCE(o.daily_receive_quota, p.daily_receive_quota, 1000),
			COALESCE(o.max_mailboxes,       p.max_mailboxes,       10),
			COALESCE(o.max_domains,         p.max_domains,         1),
			COALESCE(o.allowed_zone_ids,    p.allowed_zone_ids),
			COALESCE(o.can_create_domains,  p.can_create_domains,  FALSE),
			COALESCE(o.can_create_routes,   p.can_create_routes,   FALSE),
			COALESCE(o.can_create_api_keys, p.can_create_api_keys, TRUE)
		FROM users u
		LEFT JOIN permission_profiles p ON p.id = u.permission_profile_id
		LEFT JOIN user_permission_overrides o ON o.user_id = u.id
		WHERE u.id = $1`, userID).
		Scan(&ep.CanSend, &ep.DailySendQuota, &ep.DailyReceiveQuota,
			&ep.MaxMailboxes, &ep.MaxDomains, &allowedZones,
			&ep.CanCreateDomains, &ep.CanCreateRoutes, &ep.CanCreateAPIKeys)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("user %s not found", userID)
	}
	if err != nil {
		return nil, err
	}
	ep.AllowedZoneIDs = allowedZones
	return ep, nil
}

// uuidSliceParam converts a []uuid.UUID to a value suitable for pgx UUID array parameters.
// When the slice is nil, it returns nil so the column receives SQL NULL.
func uuidSliceParam(ids []uuid.UUID) any {
	if ids == nil {
		return nil
	}
	return ids
}
