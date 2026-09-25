package company

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// DraftQuery pages only drafts that are currently editable. Implementations
// must authorize before applying LIMIT so revoked drafts cannot hide live ones.
type DraftQuery interface {
	ListMailDraftPage(context.Context, authz.Actor, models.Page) ([]Draft, int, error)
}

type MailboxIndexReader interface {
	ContentIndexStatus(context.Context, authz.Actor, uuid.UUID) (*ContentIndexStatus, error)
	ListMessageConversation(context.Context, authz.Actor, uuid.UUID, uuid.UUID, models.Page) ([]*models.Message, int, error)
}
