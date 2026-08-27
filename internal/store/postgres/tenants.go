package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

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
