package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) GetMailboxGrant(ctx context.Context, tenant, mailbox, user uuid.UUID) (*models.MailboxGrant, error) {
	g := &models.MailboxGrant{}
	err := s.pool.QueryRow(ctx, `SELECT tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only,granted_by,created_at,updated_at
 FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3`, tenant, mailbox, user).Scan(
		&g.TenantID, &g.MailboxID, &g.UserID, &g.CanRead, &g.CanOrganize, &g.CanSend, &g.TemplateOnly, &g.GrantedBy, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// Internal persistence primitive. No new unauthenticated grant-management API
// is exposed in P0. P1 administration must audit and authorize callers.
func (s *PgStore) SetMailboxGrant(ctx context.Context, g *models.MailboxGrant) error {
	if g == nil || g.TenantID == uuid.Nil || g.MailboxID == uuid.Nil || g.UserID == uuid.Nil {
		return errors.New("invalid mailbox grant")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO mailbox_grants(tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only,granted_by)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(mailbox_id,user_id) DO UPDATE SET
 can_read=EXCLUDED.can_read,can_organize=EXCLUDED.can_organize,can_send=EXCLUDED.can_send,
 template_only=EXCLUDED.template_only,granted_by=EXCLUDED.granted_by,updated_at=clock_timestamp()
 WHERE mailbox_grants.tenant_id=EXCLUDED.tenant_id`, g.TenantID, g.MailboxID, g.UserID, g.CanRead, g.CanOrganize, g.CanSend, g.TemplateOnly, g.GrantedBy)
	return err
}
