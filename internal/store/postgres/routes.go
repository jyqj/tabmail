package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) CreateRoute(ctx context.Context, r *models.DomainRoute) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	r.CreatedAt = time.Now()
	_, err := s.db(ctx).Exec(ctx, `
		INSERT INTO domain_routes (id,zone_id,route_type,match_value,range_start,range_end,
			auto_create_mailbox,retention_hours_override,access_mode_default,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.ZoneID, r.RouteType, r.MatchValue, r.RangeStart, r.RangeEnd,
		r.AutoCreateMailbox, r.RetentionHoursOverride, r.AccessModeDefault, r.CreatedAt)
	return err
}

func (s *PgStore) GetRoute(ctx context.Context, id uuid.UUID) (*models.DomainRoute, error) {
	r := &models.DomainRoute{}
	err := s.db(ctx).QueryRow(ctx, `
		SELECT id,zone_id,route_type,match_value,range_start,range_end,
		       auto_create_mailbox,retention_hours_override,access_mode_default,created_at
		FROM domain_routes WHERE id=$1`, id).
		Scan(&r.ID, &r.ZoneID, &r.RouteType, &r.MatchValue, &r.RangeStart, &r.RangeEnd,
			&r.AutoCreateMailbox, &r.RetentionHoursOverride, &r.AccessModeDefault, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *PgStore) ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
	rows, err := s.db(ctx).Query(ctx, `
		SELECT id,zone_id,route_type,match_value,range_start,range_end,
		       auto_create_mailbox,retention_hours_override,access_mode_default,created_at
		FROM domain_routes WHERE zone_id=$1 ORDER BY created_at`, zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DomainRoute
	for rows.Next() {
		r := &models.DomainRoute{}
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.RouteType, &r.MatchValue,
			&r.RangeStart, &r.RangeEnd, &r.AutoCreateMailbox,
			&r.RetentionHoursOverride, &r.AccessModeDefault, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PgStore) DeleteRoute(ctx context.Context, id uuid.UUID) error {
	_, err := s.db(ctx).Exec(ctx, `DELETE FROM domain_routes WHERE id=$1`, id)
	return err
}

func (s *PgStore) FindMatchingRoutes(ctx context.Context, domain string, tenantID *uuid.UUID) ([]*models.DomainRoute, error) {
	var rows pgx.Rows
	var err error
	if tenantID != nil {
		rows, err = s.db(ctx).Query(ctx, `
			SELECT r.id,r.zone_id,r.route_type,r.match_value,r.range_start,r.range_end,
			       r.auto_create_mailbox,r.retention_hours_override,r.access_mode_default,r.created_at
			FROM domain_routes r
			JOIN domain_zones z ON z.id = r.zone_id
			WHERE (z.domain = $1 OR $1 LIKE '%.' || z.domain) AND z.tenant_id = $2
			ORDER BY r.created_at`, domain, *tenantID)
	} else {
		rows, err = s.db(ctx).Query(ctx, `
			SELECT r.id,r.zone_id,r.route_type,r.match_value,r.range_start,r.range_end,
			       r.auto_create_mailbox,r.retention_hours_override,r.access_mode_default,r.created_at
			FROM domain_routes r
			JOIN domain_zones z ON z.id = r.zone_id
			WHERE z.domain = $1 OR $1 LIKE '%.' || z.domain
			ORDER BY r.created_at`, domain)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DomainRoute
	for rows.Next() {
		r := &models.DomainRoute{}
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.RouteType, &r.MatchValue,
			&r.RangeStart, &r.RangeEnd, &r.AutoCreateMailbox,
			&r.RetentionHoursOverride, &r.AccessModeDefault, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
