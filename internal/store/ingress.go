package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// IngressTarget freezes the destination at SMTP acceptance, not at retry time.
// One local delivery is recorded per receipt and canonical mailbox. The original
// envelope recipients (including plus tags) remain on the receipt's job.
type IngressTarget struct {
	JobID     uuid.UUID  `json:"job_id"`
	MailboxID uuid.UUID  `json:"mailbox_id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	ZoneID    uuid.UUID  `json:"zone_id"`
	Address   string     `json:"address"`
	State     string     `json:"state"`
	MessageID *uuid.UUID `json:"message_id,omitempty"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error"`
}

type IngressClaim struct {
	Job     *models.IngestJob
	Token   uuid.UUID
	RawHash string
	RawSize int64
}

var ErrIngressClaim = errors.New("ingress claim expired or replaced")
var ErrIngressQuota = errors.New("ingress mailbox or tenant quota exceeded; raw message retained")

// IngressLedger is required for durable acceptance. An adapter without this
// contract must reject durable DATA, never silently fall back to best effort.
type IngressLedger interface {
	CreateIngress(context.Context, *models.IngestJob, []IngressTarget, string, int64) error
	ClaimIngress(context.Context) (*IngressClaim, error)
	ListIngressTargets(context.Context, uuid.UUID) ([]IngressTarget, error)
	DeliverIngress(context.Context, *IngressClaim, *models.Message, int, int) (bool, error)
	FailIngressTarget(context.Context, *IngressClaim, uuid.UUID, string) error
	FinishIngress(context.Context, *IngressClaim, int, time.Time) error
	RetryIngress(context.Context, uuid.UUID, string, string) error
}
