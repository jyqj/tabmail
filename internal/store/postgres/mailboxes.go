package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func (s *PgStore) CreateMailbox(ctx context.Context, m *models.Mailbox) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = time.Now()
	_, err := s.db(ctx).Exec(ctx, `
		INSERT INTO mailboxes (id,tenant_id,zone_id,route_id,local_part,resolved_domain,
			full_address,access_mode,password_hash,message_count,retention_hours_override,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.ZoneID, m.RouteID, m.LocalPart, m.ResolvedDomain,
		m.FullAddress, m.AccessMode, m.PasswordHash, m.MessageCount, m.RetentionHoursOverride,
		m.ExpiresAt, m.CreatedAt)
	return err
}

func (s *PgStore) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	return s.scanMailbox(s.db(ctx).QueryRow(ctx, mailboxSelect+` WHERE m.id=$1`, id))
}

func (s *PgStore) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	return s.scanMailbox(s.db(ctx).QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1`, addr))
}

// ForTenant returns a read view whose lookups are filtered to tenantID; rows
// belonging to another tenant read as not found (nil, nil).
func (s *PgStore) ForTenant(tenantID uuid.UUID) store.TenantScoped {
	return &pgTenantView{store: s, tenantID: tenantID}
}

// pgTenantView implements store.TenantScoped by appending a tenant_id filter
// to the unscoped point-lookup queries.
type pgTenantView struct {
	store    *PgStore
	tenantID uuid.UUID
}

func (v *pgTenantView) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	return v.store.scanMailbox(v.store.db(ctx).QueryRow(ctx, mailboxSelect+` WHERE m.id=$1 AND m.tenant_id=$2`, id, v.tenantID))
}

func (v *pgTenantView) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	return v.store.scanMailbox(v.store.db(ctx).QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1 AND m.tenant_id=$2`, addr, v.tenantID))
}

const mailboxSelect = `SELECT m.id,m.tenant_id,m.zone_id,m.route_id,m.local_part,
	m.resolved_domain,m.full_address,m.access_mode,m.password_hash,m.message_count,
	m.retention_hours_override,m.expires_at,m.created_at
	FROM mailboxes m`

func (s *PgStore) scanMailbox(row pgx.Row) (*models.Mailbox, error) {
	m := &models.Mailbox{}
	err := row.Scan(&m.ID, &m.TenantID, &m.ZoneID, &m.RouteID, &m.LocalPart,
		&m.ResolvedDomain, &m.FullAddress, &m.AccessMode, &m.PasswordHash, &m.MessageCount,
		&m.RetentionHoursOverride, &m.ExpiresAt, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *PgStore) ListMailboxes(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE tenant_id=$1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx,
		mailboxSelect+` WHERE m.tenant_id=$1 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`,
		tenantID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Mailbox
	for rows.Next() {
		m := &models.Mailbox{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.ZoneID, &m.RouteID, &m.LocalPart,
			&m.ResolvedDomain, &m.FullAddress, &m.AccessMode, &m.PasswordHash, &m.MessageCount,
			&m.RetentionHoursOverride, &m.ExpiresAt, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) ListMailboxesByZone(ctx context.Context, zoneID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE zone_id=$1`, zoneID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx,
		mailboxSelect+` WHERE m.zone_id=$1 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`,
		zoneID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Mailbox
	for rows.Next() {
		m := &models.Mailbox{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.ZoneID, &m.RouteID, &m.LocalPart,
			&m.ResolvedDomain, &m.FullAddress, &m.AccessMode, &m.PasswordHash, &m.MessageCount,
			&m.RetentionHoursOverride, &m.ExpiresAt, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) ListMailboxesByZones(ctx context.Context, tenantID uuid.UUID, zoneIDs []uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error) {
	pg = pg.Normalize()
	if len(zoneIDs) == 0 {
		return []*models.Mailbox{}, 0, nil
	}
	var total int
	if err := s.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE tenant_id=$1 AND zone_id=ANY($2)`, tenantID, zoneIDs).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db(ctx).Query(ctx,
		mailboxSelect+` WHERE m.tenant_id=$1 AND m.zone_id=ANY($2) ORDER BY m.created_at DESC LIMIT $3 OFFSET $4`,
		tenantID, zoneIDs, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Mailbox
	for rows.Next() {
		m := &models.Mailbox{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.ZoneID, &m.RouteID, &m.LocalPart,
			&m.ResolvedDomain, &m.FullAddress, &m.AccessMode, &m.PasswordHash, &m.MessageCount,
			&m.RetentionHoursOverride, &m.ExpiresAt, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) DeleteMailbox(ctx context.Context, id uuid.UUID) error {
	_, err := s.db(ctx).Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, id)
	return err
}

func (s *PgStore) CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error) {
	var n int
	err := s.db(ctx).QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE zone_id=$1`, zoneID).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllMailboxes(ctx context.Context) (int, error) {
	var n int
	err := s.db(ctx).QueryRow(ctx, `SELECT count(*) FROM mailboxes`).Scan(&n)
	return n, err
}

func (s *PgStore) ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error) {
	rows, err := s.db(ctx).Query(ctx, `
		SELECT raw_object_key
		FROM messages
		WHERE mailbox_id=$1 AND raw_object_key IS NOT NULL AND raw_object_key != ''`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (s *PgStore) ListZoneObjectKeys(ctx context.Context, zoneID uuid.UUID) ([]string, error) {
	rows, err := s.db(ctx).Query(ctx, `
		SELECT DISTINCT m.raw_object_key
		FROM messages m
		JOIN mailboxes mb ON mb.id = m.mailbox_id
		WHERE mb.zone_id = $1 AND m.raw_object_key IS NOT NULL AND m.raw_object_key != ''`, zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}
