package outbound

import (
	"context"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/models"
)

// DeliveryResult captures the outcome of a single delivery attempt.
type DeliveryResult struct {
	Adapter      string
	SMTPCode     int
	SMTPResponse string
	RemoteHost   string
	StartedAt    time.Time
	FinishedAt   time.Time
	Error        string
}

// DeliveryAdapter is the abstraction for outbound email delivery.
// Implementations handle the transport (direct MX, relay, SES, etc.).
type DeliveryAdapter interface {
	Name() string
	Deliver(ctx context.Context, job *models.OutboundJob, mime []byte) (*DeliveryResult, error)
}

// directAdapter delivers by resolving MX records for each recipient domain.
type directAdapter struct {
	requireTLS bool
}

func NewDirectAdapter(requireTLS bool) DeliveryAdapter {
	return &directAdapter{requireTLS: requireTLS}
}

func (a *directAdapter) Name() string { return "direct_mx" }

func (a *directAdapter) Deliver(ctx context.Context, job *models.OutboundJob, mime []byte) (*DeliveryResult, error) {
	result := &DeliveryResult{
		Adapter:   a.Name(),
		StartedAt: time.Now(),
	}
	err := DeliverDirect(ctx, job.MailFrom, job.RcptTo, mime, a.requireTLS)
	result.FinishedAt = time.Now()
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.SMTPCode = 250
	result.SMTPResponse = "OK"
	return result, nil
}

// relayAdapter delivers through a configured SMTP relay.
type relayAdapter struct {
	cfg config.Outbound
}

func NewRelayAdapter(cfg config.Outbound) DeliveryAdapter {
	return &relayAdapter{cfg: cfg}
}

func (a *relayAdapter) Name() string { return "smtp_relay" }

func (a *relayAdapter) Deliver(ctx context.Context, job *models.OutboundJob, mime []byte) (*DeliveryResult, error) {
	result := &DeliveryResult{
		Adapter:    a.Name(),
		RemoteHost: a.cfg.RelayHost,
		StartedAt:  time.Now(),
	}
	err := DeliverRelay(ctx, a.cfg, job.MailFrom, job.RcptTo, mime)
	result.FinishedAt = time.Now()
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.SMTPCode = 250
	result.SMTPResponse = "OK"
	return result, nil
}
