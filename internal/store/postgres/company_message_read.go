package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// GetWorkMessage is the company content authorization boundary. It uses the
// requested immutable mailbox ID, not an address that could have been reused,
// and projects the same per-user seen/starred state as ListWorkMessages.
// RawObjectKey is internal: application services must remove it from DTOs.
func (s *PgStore) GetWorkMessage(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*models.Message, error) {
	var out *models.Message
	err := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, current authz.Actor) error {
		rights, err := s.mailboxAccessTx(ctx, tx, current, mailbox)
		if err != nil {
			return err
		}
		if !rights.CanRead {
			return app.Forbidden("mailbox read permission required")
		}
		viewer := uuid.Nil
		if uid := current.EffectiveUserID(); uid != nil {
			viewer = *uid
		}
		out, err = scanWorkMessage(tx.QueryRow(ctx, workMessageSelect("$4")+
			` WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.id=$3`, current.TenantID, mailbox, id, viewer))
		if errors.Is(err, pgx.ErrNoRows) {
			return app.NotFound("message not found")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
