package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ================================================================
// Mailboxes
// ================================================================

func (s *PgStore) CreateMailbox(ctx context.Context, m *models.Mailbox) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	if m.Kind == "" {
		m.Kind = "legacy"
		if m.OwnerUserID != nil {
			m.Kind = "personal"
		}
	}
	m.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mailboxes (id,tenant_id,zone_id,route_id,local_part,resolved_domain,
			full_address,access_mode,password_hash,message_count,retention_hours_override,expires_at,created_at,owner_user_id,mailbox_kind)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		m.ID, m.TenantID, m.ZoneID, m.RouteID, m.LocalPart, m.ResolvedDomain,
		m.FullAddress, m.AccessMode, m.PasswordHash, m.MessageCount, m.RetentionHoursOverride,
		m.ExpiresAt, m.CreatedAt, m.OwnerUserID, m.Kind)
	return err
}

func (s *PgStore) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	return s.scanMailbox(s.pool.QueryRow(ctx, mailboxSelect+` WHERE m.id=$1`, id))
}

func (s *PgStore) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	return s.scanMailbox(s.pool.QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1`, addr))
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
	return v.store.scanMailbox(v.store.pool.QueryRow(ctx, mailboxSelect+` WHERE m.id=$1 AND m.tenant_id=$2`, id, v.tenantID))
}

func (v *pgTenantView) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	return v.store.scanMailbox(v.store.pool.QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1 AND m.tenant_id=$2`, addr, v.tenantID))
}

const mailboxSelect = `SELECT m.id,m.tenant_id,m.zone_id,m.route_id,m.local_part,
	m.resolved_domain,m.full_address,m.access_mode,m.password_hash,m.message_count,
	m.retention_hours_override,m.expires_at,m.created_at,m.owner_user_id,m.mailbox_kind
	FROM mailboxes m`

func (s *PgStore) scanMailbox(row pgx.Row) (*models.Mailbox, error) {
	m := &models.Mailbox{}
	err := row.Scan(&m.ID, &m.TenantID, &m.ZoneID, &m.RouteID, &m.LocalPart,
		&m.ResolvedDomain, &m.FullAddress, &m.AccessMode, &m.PasswordHash, &m.MessageCount,
		&m.RetentionHoursOverride, &m.ExpiresAt, &m.CreatedAt, &m.OwnerUserID, &m.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// ListMailboxesScoped applies the ZoneListFilter in SQL, mirroring the previous
// in-memory mailboxes.List (accessibleZoneIDs ∩ ZoneAllowed). The mailbox
// owner dimension is expressed via zone membership: the caller resolves owned
// zone IDs into scope.ZoneIDs for the regular-user path, so the same
// tenant_id + zone_id=ANY + owner_user_id clauses enforce everything. An empty
// ZoneIDs with AllZones=false returns an empty result with no fallback.
func (s *PgStore) ListMailboxesScoped(ctx context.Context, scope authz.ZoneListFilter, pg models.Page) ([]*models.Mailbox, int, error) {
	pg = pg.Normalize()
	if !scope.AllZones && len(scope.ZoneIDs) == 0 {
		return []*models.Mailbox{}, 0, nil
	}
	args := []any{scope.TenantID}
	clauses := []string{"m.tenant_id=$1"}
	n := 1
	if !scope.AllZones {
		n++
		clauses = append(clauses, "m.zone_id=ANY($"+strconv.Itoa(n)+")")
		args = append(args, scope.ZoneIDs)
	}
	if scope.OwnerUserID != nil || scope.GrantedUserID != nil {
		rights := []string{}
		if scope.OwnerUserID != nil {
			n++
			args = append(args, *scope.OwnerUserID)
			rights = append(rights, "m.zone_id IN (SELECT z.id FROM domain_zones z WHERE z.tenant_id=m.tenant_id AND z.owner_user_id=$"+strconv.Itoa(n)+")")
		}
		if scope.GrantedUserID != nil {
			n++
			args = append(args, *scope.GrantedUserID)
			u := "$" + strconv.Itoa(n)
			rights = append(rights, `((m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND (m.owner_user_id=`+u+` OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=`+u+` AND g.can_read)))`)
		}
		clauses = append(clauses, "("+strings.Join(rights, " OR ")+")")
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM mailboxes m WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx, mailboxSelect+" WHERE "+where+" ORDER BY m.created_at DESC,m.id LIMIT $"+strconv.Itoa(n+1)+" OFFSET $"+strconv.Itoa(n+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*models.Mailbox{}
	for rows.Next() {
		m, err := s.scanMailbox(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) DeleteMailbox(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, id)
	return err
}

func (s *PgStore) CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE zone_id=$1`, zoneID).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllMailboxes(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mailboxes`).Scan(&n)
	return n, err
}

func (s *PgStore) ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
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
	rows, err := s.pool.Query(ctx, `
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
