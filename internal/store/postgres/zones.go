package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"tabmail/internal/authz"
	"tabmail/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ================================================================
// Domain zones
// ================================================================

func (s *PgStore) CreateZone(ctx context.Context, z *models.DomainZone) error {
	if z.ID == uuid.Nil {
		z.ID = uuid.New()
	}
	if z.Visibility == "" {
		z.Visibility = models.VisibilityPrivate
	}
	z.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
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
	if errors.Is(err, pgx.ErrNoRows) {
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
	return scanZone(s.pool.QueryRow(ctx, zoneSelect+` WHERE id=$1`, id))
}

func (s *PgStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	return scanZone(s.pool.QueryRow(ctx, zoneSelect+` WHERE domain=$1`, domain))
}

func (s *PgStore) ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	rows, err := s.pool.Query(ctx, zoneSelect+` WHERE tenant_id=$1 ORDER BY domain`, tenantID)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

// ListZonesScoped applies the ZoneListFilter in SQL. The WHERE clauses mirror
// the previous in-memory filterAccessibleZones (CanManageZone ∧ ZoneAllowed):
//
//   - tenant_id is always pinned (tenant isolation in SQL, never in-memory).
//   - When scope.AllZones is false, zone_id = ANY($n) restricts to the allowlist
//     (or the caller-resolved owned-zone set). An empty ZoneIDs with AllZones=false
//     yields no rows — there is no AllZones fallback.
//   - When scope.OwnerUserID is non-nil, owner_user_id = $n restricts to the
//     owner's zones (regular users / user-owned API keys). Admins and
//     tenant-wide keys get no owner filter.
func (s *PgStore) ListZonesScoped(ctx context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error) {
	var (
		where = []string{"tenant_id=$1"}
		args  = []any{scope.TenantID}
		n     = 1
	)
	if !scope.AllZones {
		if len(scope.ZoneIDs) == 0 {
			// No visible zone: return empty without hitting the DB.
			return []*models.DomainZone{}, nil
		}
		n++
		where = append(where, "id = ANY($"+strconv.Itoa(n)+")")
		args = append(args, scope.ZoneIDs)
	}
	if scope.OwnerUserID != nil {
		n++
		where = append(where, "owner_user_id = $"+strconv.Itoa(n))
		args = append(args, *scope.OwnerUserID)
	}
	q := zoneSelect + " WHERE " + strings.Join(where, " AND ") + " ORDER BY domain"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) ListAllZones(ctx context.Context) ([]*models.DomainZone, error) {
	rows, err := s.pool.Query(ctx, zoneSelect+` ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) ListPublicZones(ctx context.Context) ([]*models.DomainZone, error) {
	rows, err := s.pool.Query(ctx, zoneSelect+` WHERE visibility='public' ORDER BY domain`)
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
	rows, err := s.pool.Query(ctx, zoneSelect+` WHERE visibility = ANY($1) ORDER BY domain`, vals)
	if err != nil {
		return nil, err
	}
	return scanZones(rows)
}

func (s *PgStore) UpdateZone(ctx context.Context, z *models.DomainZone) error {
	if z.Visibility == "" {
		z.Visibility = models.VisibilityPrivate
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE domain_zones SET owner_user_id=$2, parent_zone_id=$3, visibility=$4,
			allow_random_subdomains=$5, is_verified=$6, mx_verified=$7, txt_record=$8, verified_at=$9,
			dkim_enabled=$10, dkim_required_for_send=$11
		WHERE id=$1`, z.ID, z.OwnerUserID, z.ParentZoneID, z.Visibility,
		z.AllowRandomSubdomains, z.IsVerified, z.MXVerified, z.TXTRecord, z.VerifiedAt,
		z.DKIMEnabled, z.DKIMRequiredForSend)
	return err
}

func (s *PgStore) DeleteZone(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM domain_zones WHERE id=$1`, id)
	return err
}

func (s *PgStore) CountZones(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM domain_zones WHERE tenant_id=$1`, tenantID).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllZones(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM domain_zones`).Scan(&n)
	return n, err
}

// ================================================================
// Domain routes
// ================================================================

func (s *PgStore) CreateRoute(ctx context.Context, r *models.DomainRoute) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	r.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO domain_routes (id,zone_id,route_type,match_value,range_start,range_end,
			auto_create_mailbox,retention_hours_override,access_mode_default,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.ZoneID, r.RouteType, r.MatchValue, r.RangeStart, r.RangeEnd,
		r.AutoCreateMailbox, r.RetentionHoursOverride, r.AccessModeDefault, r.CreatedAt)
	return err
}

func (s *PgStore) GetRoute(ctx context.Context, id uuid.UUID) (*models.DomainRoute, error) {
	r := &models.DomainRoute{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,zone_id,route_type,match_value,range_start,range_end,
		       auto_create_mailbox,retention_hours_override,access_mode_default,created_at
		FROM domain_routes WHERE id=$1`, id).
		Scan(&r.ID, &r.ZoneID, &r.RouteType, &r.MatchValue, &r.RangeStart, &r.RangeEnd,
			&r.AutoCreateMailbox, &r.RetentionHoursOverride, &r.AccessModeDefault, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (s *PgStore) ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
	rows, err := s.pool.Query(ctx, `
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
	_, err := s.pool.Exec(ctx, `DELETE FROM domain_routes WHERE id=$1`, id)
	return err
}

func (s *PgStore) FindMatchingRoutes(ctx context.Context, domain string, tenantID *uuid.UUID) ([]*models.DomainRoute, error) {
	var rows pgx.Rows
	var err error
	if tenantID != nil {
		rows, err = s.pool.Query(ctx, `
			SELECT r.id,r.zone_id,r.route_type,r.match_value,r.range_start,r.range_end,
			       r.auto_create_mailbox,r.retention_hours_override,r.access_mode_default,r.created_at
			FROM domain_routes r
			JOIN domain_zones z ON z.id = r.zone_id
			WHERE (z.domain = $1 OR $1 LIKE '%.' || z.domain) AND z.tenant_id = $2
			ORDER BY r.created_at`, domain, *tenantID)
	} else {
		rows, err = s.pool.Query(ctx, `
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
