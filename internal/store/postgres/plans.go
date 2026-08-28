package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) CreatePlan(ctx context.Context, p *models.Plan) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db(ctx).Exec(ctx, `
		INSERT INTO plans (id,name,max_domains,max_mailboxes_per_domain,max_messages_per_mailbox,
		                   max_message_bytes,retention_hours,rpm_limit,daily_quota,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		p.ID, p.Name, p.MaxDomains, p.MaxMailboxesPerDomain, p.MaxMessagesPerMailbox,
		p.MaxMessageBytes, p.RetentionHours, p.RPMLimit, p.DailyQuota, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *PgStore) GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	p := &models.Plan{}
	err := s.db(ctx).QueryRow(ctx, `SELECT id,name,max_domains,max_mailboxes_per_domain,
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
	rows, err := s.db(ctx).Query(ctx, `SELECT id,name,max_domains,max_mailboxes_per_domain,
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
	_, err := s.db(ctx).Exec(ctx, `
		UPDATE plans SET name=$2,max_domains=$3,max_mailboxes_per_domain=$4,
		max_messages_per_mailbox=$5,max_message_bytes=$6,retention_hours=$7,
		rpm_limit=$8,daily_quota=$9,updated_at=$10 WHERE id=$1`,
		p.ID, p.Name, p.MaxDomains, p.MaxMailboxesPerDomain, p.MaxMessagesPerMailbox,
		p.MaxMessageBytes, p.RetentionHours, p.RPMLimit, p.DailyQuota, p.UpdatedAt)
	return err
}

func (s *PgStore) DeletePlan(ctx context.Context, id uuid.UUID) error {
	_, err := s.db(ctx).Exec(ctx, `DELETE FROM plans WHERE id=$1`, id)
	return err
}
