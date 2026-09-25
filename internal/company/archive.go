package company

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"time"
)

// ArchivedMail is one mailbox-owned immutable sent asset. DeliveryAvailable
// says only whether an operational receipt still exists, not delivery success.
// BCC and queue-internal diagnostics are intentionally absent.
type ArchivedMail struct {
	ID                uuid.UUID `json:"id"`
	MailboxID         uuid.UUID `json:"mailbox_id"`
	MailFrom          string    `json:"from"`
	To                []string  `json:"to"`
	Subject           string    `json:"subject"`
	CreatedAt         time.Time `json:"created_at"`
	AttachmentCount   int       `json:"attachment_count"`
	Revision          int64     `json:"revision"`
	DeliveryAvailable bool      `json:"delivery_available"`
}
type SentArchive interface {
	ListArchivedMail(context.Context, authz.Actor, uuid.UUID, string, string, models.Page) ([]ArchivedMail, int, error)
	MutateArchivedMail(context.Context, authz.Actor, uuid.UUID, uuid.UUID, int64, string) error
}
