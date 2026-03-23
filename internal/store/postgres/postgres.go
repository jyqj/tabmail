package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/config"
	"tabmail/internal/models"
)

type PgStore struct {
	pool *pgxpool.Pool
}

const LatestSchemaVersion = 8

func New(ctx context.Context, cfg config.DB) (*PgStore, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Close() error {
	s.pool.Close()
	return nil
}

func (s *PgStore) CurrentSchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// ================================================================
// Plans
// ================================================================

func (s *PgStore) CreatePlan(ctx context.Context, p *models.Plan) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans (id,name,max_domains,max_mailboxes_per_domain,max_messages_per_mailbox,
		                   max_message_bytes,retention_hours,rpm_limit,daily_quota,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		p.ID, p.Name, p.MaxDomains, p.MaxMailboxesPerDomain, p.MaxMessagesPerMailbox,
		p.MaxMessageBytes, p.RetentionHours, p.RPMLimit, p.DailyQuota, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *PgStore) GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	p := &models.Plan{}
	err := s.pool.QueryRow(ctx, `SELECT id,name,max_domains,max_mailboxes_per_domain,
		max_messages_per_mailbox,max_message_bytes,retention_hours,rpm_limit,daily_quota,
		created_at,updated_at FROM plans WHERE id=$1`, id).Scan(
		&p.ID, &p.Name, &p.MaxDomains, &p.MaxMailboxesPerDomain, &p.MaxMessagesPerMailbox,
		&p.MaxMessageBytes, &p.RetentionHours, &p.RPMLimit, &p.DailyQuota,
		&p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *PgStore) ListPlans(ctx context.Context) ([]*models.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,max_domains,max_mailboxes_per_domain,
		max_messages_per_mailbox,max_message_bytes,retention_hours,rpm_limit,daily_quota,
		created_at,updated_at FROM plans ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Plan
	for rows.Next() {
		p := &models.Plan{}
		if err := rows.Scan(&p.ID, &p.Name, &p.MaxDomains, &p.MaxMailboxesPerDomain,
			&p.MaxMessagesPerMailbox, &p.MaxMessageBytes, &p.RetentionHours, &p.RPMLimit,
			&p.DailyQuota, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PgStore) UpdatePlan(ctx context.Context, p *models.Plan) error {
	p.UpdatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE plans SET name=$2,max_domains=$3,max_mailboxes_per_domain=$4,
		max_messages_per_mailbox=$5,max_message_bytes=$6,retention_hours=$7,
		rpm_limit=$8,daily_quota=$9,updated_at=$10 WHERE id=$1`,
		p.ID, p.Name, p.MaxDomains, p.MaxMailboxesPerDomain, p.MaxMessagesPerMailbox,
		p.MaxMessageBytes, p.RetentionHours, p.RPMLimit, p.DailyQuota, p.UpdatedAt)
	return err
}

func (s *PgStore) DeletePlan(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans WHERE id=$1`, id)
	return err
}

// ================================================================
// Tenants
// ================================================================

func (s *PgStore) CreateTenant(ctx context.Context, t *models.Tenant) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	t.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tenants (id,name,plan_id,is_super,created_at) VALUES ($1,$2,$3,$4,$5)`,
		t.ID, t.Name, t.PlanID, t.IsSuper, t.CreatedAt)
	return err
}

func (s *PgStore) GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	t := &models.Tenant{}
	err := s.pool.QueryRow(ctx,
		`SELECT id,name,plan_id,is_super,created_at FROM tenants WHERE id=$1`, id).
		Scan(&t.ID, &t.Name, &t.PlanID, &t.IsSuper, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *PgStore) ListTenants(ctx context.Context) ([]*models.Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,plan_id,is_super,created_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Tenant
	for rows.Next() {
		t := &models.Tenant{}
		if err := rows.Scan(&t.ID, &t.Name, &t.PlanID, &t.IsSuper, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PgStore) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	return err
}

// ================================================================
// Tenant overrides
// ================================================================

func (s *PgStore) UpsertOverride(ctx context.Context, o *models.TenantOverride) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	o.UpdatedAt = time.Now()
	return s.pool.QueryRow(ctx, `
		INSERT INTO tenant_overrides (id,tenant_id,max_domains,max_mailboxes_per_domain,
			max_messages_per_mailbox,max_message_bytes,retention_hours,rpm_limit,daily_quota,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id) DO UPDATE SET
			max_domains=EXCLUDED.max_domains,
			max_mailboxes_per_domain=EXCLUDED.max_mailboxes_per_domain,
			max_messages_per_mailbox=EXCLUDED.max_messages_per_mailbox,
			max_message_bytes=EXCLUDED.max_message_bytes,
			retention_hours=EXCLUDED.retention_hours,
			rpm_limit=EXCLUDED.rpm_limit,
			daily_quota=EXCLUDED.daily_quota,
			updated_at=EXCLUDED.updated_at
		RETURNING id, updated_at`,
		o.ID, o.TenantID, o.MaxDomains, o.MaxMailboxesPerDomain, o.MaxMessagesPerMailbox,
		o.MaxMessageBytes, o.RetentionHours, o.RPMLimit, o.DailyQuota, o.UpdatedAt).
		Scan(&o.ID, &o.UpdatedAt)
}

func (s *PgStore) GetOverride(ctx context.Context, tenantID uuid.UUID) (*models.TenantOverride, error) {
	o := &models.TenantOverride{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,max_domains,max_mailboxes_per_domain,max_messages_per_mailbox,
		       max_message_bytes,retention_hours,rpm_limit,daily_quota,updated_at
		FROM tenant_overrides WHERE tenant_id=$1`, tenantID).
		Scan(&o.ID, &o.TenantID, &o.MaxDomains, &o.MaxMailboxesPerDomain, &o.MaxMessagesPerMailbox,
			&o.MaxMessageBytes, &o.RetentionHours, &o.RPMLimit, &o.DailyQuota, &o.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return o, err
}

// EffectiveConfig resolves plan + override → flat config.
func (s *PgStore) EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error) {
	ec := &models.EffectiveConfig{}
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(o.max_domains,             p.max_domains),
			COALESCE(o.max_mailboxes_per_domain,p.max_mailboxes_per_domain),
			COALESCE(o.max_messages_per_mailbox, p.max_messages_per_mailbox),
			COALESCE(o.max_message_bytes,        p.max_message_bytes),
			COALESCE(o.retention_hours,          p.retention_hours),
			COALESCE(o.rpm_limit,                p.rpm_limit),
			COALESCE(o.daily_quota,              p.daily_quota)
		FROM tenants t
		JOIN plans p ON p.id = t.plan_id
		LEFT JOIN tenant_overrides o ON o.tenant_id = t.id
		WHERE t.id = $1`, tenantID).
		Scan(&ec.MaxDomains, &ec.MaxMailboxesPerDomain, &ec.MaxMessagesPerMailbox,
			&ec.MaxMessageBytes, &ec.RetentionHours, &ec.RPMLimit, &ec.DailyQuota)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}
	return ec, err
}

// ================================================================
// Tenant API keys
// ================================================================

func (s *PgStore) CreateAPIKey(ctx context.Context, k *models.TenantAPIKey) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	k.CreatedAt = time.Now()
	scopesJSON, err := json.Marshal(k.Scopes)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tenant_api_keys (id,tenant_id,key_hash,key_prefix,label,scopes,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		k.ID, k.TenantID, k.KeyHash, k.KeyPrefix, k.Label, scopesJSON, k.ExpiresAt, k.CreatedAt)
	return err
}

func (s *PgStore) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,key_prefix,label,scopes,expires_at,created_at,last_used_at
		FROM tenant_api_keys WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.TenantAPIKey
	for rows.Next() {
		k := &models.TenantAPIKey{}
		var scopesJSON []byte
		if err := rows.Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Label,
			&scopesJSON, &k.ExpiresAt, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
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

func (s *PgStore) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tenant_api_keys WHERE id=$1`, id)
	return err
}

func (s *PgStore) ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, []string, error) {
	h := hashKey(rawKey)
	t := &models.Tenant{}
	var keyID uuid.UUID
	var scopes []string
	var scopesJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT k.id, k.scopes, t.id, t.name, t.plan_id, t.is_super, t.created_at
		FROM tenant_api_keys k
		JOIN tenants t ON t.id = k.tenant_id
		WHERE k.key_hash = $1
		  AND (k.expires_at IS NULL OR k.expires_at > now())`, h).
		Scan(&keyID, &scopesJSON, &t.ID, &t.Name, &t.PlanID, &t.IsSuper, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(scopesJSON) > 0 {
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return nil, nil, err
		}
	}
	go func() { _ = s.TouchAPIKey(context.Background(), keyID) }()
	return t, scopes, nil
}

func (s *PgStore) TouchAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tenant_api_keys SET last_used_at=now() WHERE id=$1`, id)
	return err
}

// ================================================================
// Domain zones
// ================================================================

func (s *PgStore) CreateZone(ctx context.Context, z *models.DomainZone) error {
	if z.ID == uuid.Nil {
		z.ID = uuid.New()
	}
	z.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO domain_zones (id,tenant_id,domain,is_verified,mx_verified,txt_record,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		z.ID, z.TenantID, z.Domain, z.IsVerified, z.MXVerified, z.TXTRecord, z.CreatedAt)
	return err
}

func (s *PgStore) GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error) {
	z := &models.DomainZone{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,domain,is_verified,mx_verified,txt_record,created_at,verified_at
		FROM domain_zones WHERE id=$1`, id).
		Scan(&z.ID, &z.TenantID, &z.Domain, &z.IsVerified, &z.MXVerified,
			&z.TXTRecord, &z.CreatedAt, &z.VerifiedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return z, err
}

func (s *PgStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	z := &models.DomainZone{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,domain,is_verified,mx_verified,txt_record,created_at,verified_at
		FROM domain_zones WHERE domain=$1`, domain).
		Scan(&z.ID, &z.TenantID, &z.Domain, &z.IsVerified, &z.MXVerified,
			&z.TXTRecord, &z.CreatedAt, &z.VerifiedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return z, err
}

func (s *PgStore) ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,domain,is_verified,mx_verified,txt_record,created_at,verified_at
		FROM domain_zones WHERE tenant_id=$1 ORDER BY domain`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.DomainZone
	for rows.Next() {
		z := &models.DomainZone{}
		if err := rows.Scan(&z.ID, &z.TenantID, &z.Domain, &z.IsVerified, &z.MXVerified,
			&z.TXTRecord, &z.CreatedAt, &z.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *PgStore) UpdateZone(ctx context.Context, z *models.DomainZone) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE domain_zones SET is_verified=$2, mx_verified=$3, txt_record=$4, verified_at=$5
		WHERE id=$1`, z.ID, z.IsVerified, z.MXVerified, z.TXTRecord, z.VerifiedAt)
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
	if err == pgx.ErrNoRows {
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

func (s *PgStore) FindMatchingRoutes(ctx context.Context, domain string) ([]*models.DomainRoute, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id,r.zone_id,r.route_type,r.match_value,r.range_start,r.range_end,
		       r.auto_create_mailbox,r.retention_hours_override,r.access_mode_default,r.created_at
		FROM domain_routes r
		JOIN domain_zones z ON z.id = r.zone_id
		WHERE z.domain = $1 OR $1 LIKE '%.' || z.domain
		ORDER BY r.created_at`, domain)
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

// ================================================================
// SMTP Policy
// ================================================================

func (s *PgStore) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	p := &models.SMTPPolicy{}
	err := s.pool.QueryRow(ctx, `
		SELECT default_accept,accept_domains,reject_domains,default_store,store_domains,discard_domains,reject_origin_domains,updated_at
		FROM smtp_policies WHERE id=TRUE`).
		Scan(&p.DefaultAccept, &p.AcceptDomains, &p.RejectDomains, &p.DefaultStore, &p.StoreDomains, &p.DiscardDomains, &p.RejectOriginDomains, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *PgStore) UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error {
	p.UpdatedAt = time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO smtp_policies (id,default_accept,accept_domains,reject_domains,default_store,store_domains,discard_domains,reject_origin_domains,updated_at)
		VALUES (TRUE,$1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (id) DO UPDATE SET
			default_accept=EXCLUDED.default_accept,
			accept_domains=EXCLUDED.accept_domains,
			reject_domains=EXCLUDED.reject_domains,
			default_store=EXCLUDED.default_store,
			store_domains=EXCLUDED.store_domains,
			discard_domains=EXCLUDED.discard_domains,
			reject_origin_domains=EXCLUDED.reject_origin_domains,
			updated_at=EXCLUDED.updated_at`,
		p.DefaultAccept, p.AcceptDomains, p.RejectDomains, p.DefaultStore, p.StoreDomains, p.DiscardDomains, p.RejectOriginDomains, p.UpdatedAt)
	return err
}

// ================================================================
// Mailboxes
// ================================================================

func (s *PgStore) CreateMailbox(ctx context.Context, m *models.Mailbox) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mailboxes (id,tenant_id,zone_id,route_id,local_part,resolved_domain,
			full_address,access_mode,password_hash,message_count,retention_hours_override,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.ZoneID, m.RouteID, m.LocalPart, m.ResolvedDomain,
		m.FullAddress, m.AccessMode, m.PasswordHash, m.MessageCount, m.RetentionHoursOverride,
		m.ExpiresAt, m.CreatedAt)
	return err
}

func (s *PgStore) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	return s.scanMailbox(s.pool.QueryRow(ctx, mailboxSelect+` WHERE m.id=$1`, id))
}

func (s *PgStore) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	return s.scanMailbox(s.pool.QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1`, addr))
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
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE tenant_id=$1`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
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
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM mailboxes WHERE zone_id=$1`, zoneID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
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

// ================================================================
// Messages
// ================================================================

func (s *PgStore) CreateMessage(ctx context.Context, m *models.Message) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.ReceivedAt = time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE mailboxes SET message_count = message_count + 1 WHERE id=$1`, m.MailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,
			size,seen,raw_object_key,headers_json,received_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.MailboxID, m.ZoneID, m.Sender, m.Recipients, m.Subject,
		m.Size, m.Seen, m.RawObjectKey, m.HeadersJSON, m.ReceivedAt, m.ExpiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int) (bool, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.ReceivedAt = time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE mailboxes
		SET message_count = message_count + 1
		WHERE id=$1 AND ($2 <= 0 OR message_count < $2)`,
		m.MailboxID, maxMessages)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messages (id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,
			size,seen,raw_object_key,headers_json,received_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		m.ID, m.TenantID, m.MailboxID, m.ZoneID, m.Sender, m.Recipients, m.Subject,
		m.Size, m.Seen, m.RawObjectKey, m.HeadersJSON, m.ReceivedAt, m.ExpiresAt); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *PgStore) GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error) {
	m := &models.Message{}
	err := s.pool.QueryRow(ctx, `
		SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,
		       raw_object_key,headers_json,received_at,expires_at
		FROM messages WHERE id=$1`, id).
		Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender, &m.Recipients,
			&m.Subject, &m.Size, &m.Seen, &m.RawObjectKey, &m.HeadersJSON,
			&m.ReceivedAt, &m.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *PgStore) ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error) {
	pg = pg.Normalize()
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE mailbox_id=$1`, mailboxID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,
		       raw_object_key,headers_json,received_at,expires_at
		FROM messages WHERE mailbox_id=$1 ORDER BY received_at DESC LIMIT $2 OFFSET $3`,
		mailboxID, pg.PerPage, pg.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.Message
	for rows.Next() {
		m := &models.Message{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender,
			&m.Recipients, &m.Subject, &m.Size, &m.Seen, &m.RawObjectKey,
			&m.HeadersJSON, &m.ReceivedAt, &m.ExpiresAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *PgStore) MarkSeen(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET seen=TRUE WHERE id=$1`, id)
	return err
}

func (s *PgStore) DeleteMessage(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var mailboxID uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM messages WHERE id=$1 RETURNING mailbox_id`, id).Scan(&mailboxID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE mailboxes SET message_count = GREATEST(message_count - 1, 0) WHERE id=$1`, mailboxID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE mailbox_id=$1`, mailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE mailboxes SET message_count = 0 WHERE id=$1`, mailboxID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PgStore) CountMessages(ctx context.Context, mailboxID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT message_count FROM mailboxes WHERE id=$1`, mailboxID).Scan(&n)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return n, err
}

func (s *PgStore) CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE raw_object_key=$1`, objectKey).Scan(&n)
	return n, err
}

func (s *PgStore) CountAllMessages(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n)
	return n, err
}

func (s *PgStore) CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE tenant_id=$1 AND received_at >= $2`, tenantID, since).Scan(&n)
	return n, err
}

func (s *PgStore) DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH doomed AS (
			SELECT id, mailbox_id
			FROM messages
			WHERE expires_at < $1
			ORDER BY expires_at, id
			LIMIT $2
		),
		deleted AS (
			DELETE FROM messages m
			USING doomed d
			WHERE m.id = d.id
			RETURNING d.mailbox_id
		)
		SELECT mailbox_id, count(*) FROM deleted GROUP BY mailbox_id`, before, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var mailboxID uuid.UUID
		var count int
		if err := rows.Scan(&mailboxID, &count); err != nil {
			return 0, err
		}
		total += count
		if _, err := tx.Exec(ctx, `
			UPDATE mailboxes SET message_count = GREATEST(message_count - $2, 0) WHERE id=$1`,
			mailboxID, count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *PgStore) ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT raw_object_key FROM messages
		WHERE expires_at < $1 AND raw_object_key IS NOT NULL AND raw_object_key != ''
		ORDER BY expires_at, id
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ================================================================
// Audit
// ================================================================

func (s *PgStore) InsertAudit(ctx context.Context, e *models.AuditEntry) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	e.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (id,tenant_id,actor,action,resource_type,resource_id,details,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.TenantID, e.Actor, e.Action, e.ResourceType, e.ResourceID, e.Details, e.CreatedAt)
	return err
}

func (s *PgStore) ListAuditEntries(ctx context.Context, limit int) ([]*models.AuditEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,actor,action,resource_type,resource_id,details,created_at
		FROM audit_log
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.AuditEntry
	for rows.Next() {
		e := &models.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error) {
	pg = pg.Normalize()
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,actor,action,resource_type,resource_id,details,created_at
		FROM audit_log
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`, pg.PerPage, (pg.Page-1)*pg.PerPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.AuditEntry
	for rows.Next() {
		e := &models.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CreateMonitorEvent(ctx context.Context, e *models.MonitorEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO monitor_events (id,type,mailbox,message_id,sender,subject,size,at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.Type, e.Mailbox, e.MessageID, e.Sender, e.Subject, e.Size, e.At)
	return err
}

func (s *PgStore) ListMonitorEvents(ctx context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if eventType != "" {
		add("type =", eventType)
	}
	if mailbox != "" {
		add("mailbox ILIKE", "%"+mailbox+"%")
	}
	if sender != "" {
		add("sender ILIKE", "%"+sender+"%")
	}

	var total int
	countQuery := `SELECT count(*) FROM monitor_events` + where
	if err := s.pool.QueryRow(ctx, countQuery, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(filters, pg.PerPage, pg.Offset())
	query := `SELECT id,type,mailbox,message_id,sender,subject,size,at FROM monitor_events` + where + fmt.Sprintf(" ORDER BY at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.MonitorEvent
	for rows.Next() {
		e := &models.MonitorEvent{}
		if err := rows.Scan(&e.ID, &e.Type, &e.Mailbox, &e.MessageID, &e.Sender, &e.Subject, &e.Size, &e.At); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	now := time.Now().UTC()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = now
	}
	if e.NextAttemptAt.IsZero() {
		e.NextAttemptAt = now
	}
	if e.State == "" {
		e.State = "pending"
	}
	e.CreatedAt = now
	e.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbox_events (id,event_type,payload,occurred_at,state,attempts,last_error,next_attempt_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ID, e.EventType, e.Payload, e.OccurredAt, e.State, e.Attempts, e.LastError, e.NextAttemptAt, e.CreatedAt, e.UpdatedAt)
	return err
}

func (s *PgStore) ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM outbox_events
			WHERE state IN ('pending','retry') AND next_attempt_at <= $1
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events o
		SET state='processing', attempts=o.attempts + 1, updated_at=$1
		FROM cte
		WHERE o.id = cte.id
		RETURNING o.id,o.event_type,o.payload,o.occurred_at,o.state,o.attempts,o.last_error,o.next_attempt_at,o.created_at,o.updated_at`,
		now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.OutboxEvent
	for rows.Next() {
		e := &models.OutboxEvent{}
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.OccurredAt, &e.State, &e.Attempts, &e.LastError, &e.NextAttemptAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkOutboxEventDone(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET state='done', updated_at=$2 WHERE id=$1`, id, time.Now().UTC())
	return err
}

func (s *PgStore) MarkOutboxEventRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox_events
		SET state='retry', last_error=$2, next_attempt_at=$3, updated_at=$4
		WHERE id=$1`, id, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}

func (s *PgStore) CreateWebhookDeliveries(ctx context.Context, event *models.OutboxEvent, urls []string) error {
	if event == nil || len(urls) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	for _, url := range urls {
		if _, err := tx.Exec(ctx, `
			INSERT INTO webhook_deliveries (id,event_id,url,event_type,payload,state,attempts,next_attempt_at,created_at,updated_at)
			VALUES ($1,$2,$3,$4,$5,'pending',0,$6,$6,$6)
			ON CONFLICT (event_id, url) DO NOTHING`,
			uuid.New(), event.ID, url, event.EventType, event.Payload, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PgStore) ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM webhook_deliveries
			WHERE state IN ('pending','retry') AND next_attempt_at <= $1
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_deliveries d
		SET state='processing', attempts=d.attempts + 1, last_tried_at=$1, updated_at=$1
		FROM cte
		WHERE d.id = cte.id
		RETURNING d.id,d.event_id,d.url,d.event_type,d.payload,d.state,d.attempts,d.last_error,d.next_attempt_at,d.last_tried_at,d.delivered_at,d.created_at,d.updated_at`,
		now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.WebhookDelivery
	for rows.Next() {
		d := &models.WebhookDelivery{}
		if err := rows.Scan(&d.ID, &d.EventID, &d.URL, &d.EventType, &d.Payload, &d.State, &d.Attempts, &d.LastError, &d.NextAttemptAt, &d.LastTriedAt, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkWebhookDeliveryDone(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET state='delivered', delivered_at=$2, updated_at=$2
		WHERE id=$1`, id, now)
	return err
}

func (s *PgStore) MarkWebhookDeliveryRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET state=$2, last_error=$3, next_attempt_at=$4, updated_at=$5
		WHERE id=$1`, id, state, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}

func (s *PgStore) ListDeadWebhookDeliveries(ctx context.Context, limit int) ([]models.DeadLetter, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,url,event_type,payload,attempts,last_error,created_at,last_tried_at
		FROM webhook_deliveries
		WHERE state='dead'
		ORDER BY updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.DeadLetter, 0, limit)
	for rows.Next() {
		var dl models.DeadLetter
		var id uuid.UUID
		var lastTriedAt *time.Time
		if err := rows.Scan(&id, &dl.URL, &dl.EventType, &dl.Payload, &dl.Attempts, &dl.LastError, &dl.CreatedAt, &lastTriedAt); err != nil {
			return nil, err
		}
		dl.ID = id.String()
		if lastTriedAt != nil {
			dl.LastTriedAt = *lastTriedAt
		}
		out = append(out, dl)
	}
	return out, rows.Err()
}

func (s *PgStore) CountDeadWebhookDeliveries(ctx context.Context) (int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE state='dead'`).Scan(&total)
	return total, err
}

func (s *PgStore) ListWebhookDeliveries(ctx context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if state != "" {
		add("state =", state)
	}
	if eventType != "" {
		add("event_type =", eventType)
	}
	if url != "" {
		add("url ILIKE", "%"+url+"%")
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`+where, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(filters, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx, `
		SELECT id,event_id,url,event_type,payload,state,attempts,last_error,next_attempt_at,last_tried_at,delivered_at,created_at,updated_at
		FROM webhook_deliveries`+where+fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.WebhookDelivery
	for rows.Next() {
		item := &models.WebhookDelivery{}
		if err := rows.Scan(&item.ID, &item.EventID, &item.URL, &item.EventType, &item.Payload, &item.State, &item.Attempts, &item.LastError, &item.NextAttemptAt, &item.LastTriedAt, &item.DeliveredAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CountWebhookDeliveriesByState(ctx context.Context, states ...string) (int, error) {
	if len(states) == 0 {
		var total int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&total)
		return total, err
	}
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE state = ANY($1)`, states).Scan(&total)
	return total, err
}

func (s *PgStore) CreateIngestJob(ctx context.Context, job *models.IngestJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := time.Now().UTC()
	if job.NextAttemptAt.IsZero() {
		job.NextAttemptAt = now
	}
	if job.State == "" {
		job.State = "pending"
	}
	job.CreatedAt = now
	job.UpdatedAt = now
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ingest_jobs (id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,state,attempts,last_error,next_attempt_at,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		job.ID, job.Source, job.RemoteIP, job.MailFrom, job.Recipients, job.RawObjectKey, job.Metadata, job.State, job.Attempts, job.LastError, job.NextAttemptAt, job.CreatedAt, job.UpdatedAt)
	return err
}

func (s *PgStore) ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		WITH cte AS (
			SELECT id
			FROM ingest_jobs
			WHERE state IN ('pending','retry') AND next_attempt_at <= $1
			ORDER BY created_at
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE ingest_jobs j
		SET state='processing', attempts=j.attempts + 1, updated_at=$1
		FROM cte
		WHERE j.id = cte.id
		RETURNING j.id,j.source,j.remote_ip,j.mail_from,j.recipients,j.raw_object_key,j.metadata,j.state,j.attempts,j.last_error,j.next_attempt_at,j.created_at,j.updated_at`,
		now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.IngestJob
	for rows.Next() {
		job := &models.IngestJob{}
		if err := rows.Scan(&job.ID, &job.Source, &job.RemoteIP, &job.MailFrom, &job.Recipients, &job.RawObjectKey, &job.Metadata, &job.State, &job.Attempts, &job.LastError, &job.NextAttemptAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *PgStore) MarkIngestJobDone(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest_jobs SET state='done', updated_at=$2 WHERE id=$1`, id, time.Now().UTC())
	return err
}

func (s *PgStore) MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	state := "retry"
	if dead {
		state = "dead"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE ingest_jobs
		SET state=$2, last_error=$3, next_attempt_at=$4, updated_at=$5
		WHERE id=$1`, id, state, lastError, nextAttemptAt.UTC(), time.Now().UTC())
	return err
}

func (s *PgStore) ListIngestJobs(ctx context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if state != "" {
		add("state =", state)
	}
	if source != "" {
		add("source =", source)
	}
	if recipient != "" {
		filters = append(filters, recipient)
		where += fmt.Sprintf(" AND $%d = ANY(recipients)", len(filters))
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs`+where, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(filters, pg.PerPage, pg.Offset())
	rows, err := s.pool.Query(ctx, `
		SELECT id,source,remote_ip,mail_from,recipients,raw_object_key,metadata,state,attempts,last_error,next_attempt_at,created_at,updated_at
		FROM ingest_jobs`+where+fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.IngestJob
	for rows.Next() {
		item := &models.IngestJob{}
		if err := rows.Scan(&item.ID, &item.Source, &item.RemoteIP, &item.MailFrom, &item.Recipients, &item.RawObjectKey, &item.Metadata, &item.State, &item.Attempts, &item.LastError, &item.NextAttemptAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CountIngestJobsByState(ctx context.Context, states ...string) (int, error) {
	if len(states) == 0 {
		var total int
		err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs`).Scan(&total)
		return total, err
	}
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest_jobs WHERE state = ANY($1)`, states).Scan(&total)
	return total, err
}
