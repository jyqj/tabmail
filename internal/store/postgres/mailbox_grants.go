package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

// ================================================================
// Mailbox grants
// ================================================================

func (s *PgStore) CreateMailboxGrant(ctx context.Context, g *models.MailboxGrant) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mailbox_grants (id, tenant_id, mailbox_id, principal_type, principal_id, role)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		g.ID, g.TenantID, g.MailboxID, g.PrincipalType, g.PrincipalID, g.Role)
	return err
}

func (s *PgStore) DeleteMailboxGrant(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mailbox_grants WHERE id = $1`, id)
	return err
}

func (s *PgStore) DeleteMailboxGrantScoped(ctx context.Context, id uuid.UUID, mailboxID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mailbox_grants WHERE id = $1 AND mailbox_id = $2`, id, mailboxID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *PgStore) ListMailboxGrants(ctx context.Context, mailboxID uuid.UUID) ([]*models.MailboxGrant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, mailbox_id, principal_type, principal_id, role, created_at
		FROM mailbox_grants WHERE mailbox_id = $1 ORDER BY created_at`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grants []*models.MailboxGrant
	for rows.Next() {
		g := &models.MailboxGrant{}
		if err := rows.Scan(&g.ID, &g.TenantID, &g.MailboxID, &g.PrincipalType, &g.PrincipalID, &g.Role, &g.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *PgStore) GetMailboxGrant(ctx context.Context, mailboxID uuid.UUID, principalType string, principalID uuid.UUID) (*models.MailboxGrant, error) {
	g := &models.MailboxGrant{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, mailbox_id, principal_type, principal_id, role, created_at
		FROM mailbox_grants WHERE mailbox_id = $1 AND principal_type = $2 AND principal_id = $3`,
		mailboxID, principalType, principalID).
		Scan(&g.ID, &g.TenantID, &g.MailboxID, &g.PrincipalType, &g.PrincipalID, &g.Role, &g.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return g, err
}

func (s *PgStore) ListGrantedMailboxIDs(ctx context.Context, principalType string, principalID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mailbox_id FROM mailbox_grants WHERE principal_type = $1 AND principal_id = $2`,
		principalType, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
