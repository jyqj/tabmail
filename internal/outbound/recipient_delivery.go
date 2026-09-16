package outbound

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/textproto"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type recipientStore interface {
	ListOutboundRecipients(context.Context, uuid.UUID, uuid.UUID) ([]company.Recipient, error)
	BeginOutboundRecipient(context.Context, uuid.UUID, *uuid.UUID, string) (bool, error)
	CompleteOutboundRecipient(context.Context, uuid.UUID, *uuid.UUID, string, string, int, string) error
}

// A transaction per envelope recipient is deliberate: a rejected RCPT cannot
// poison valid recipients at that domain. It costs more relay connections than
// batching, but keeps every accepted/permanent/uncertain result independently
// fenced. The full To/CC headers stay intact and BCC stays envelope-only.
func (s *Service) deliverRecipients(ctx context.Context, j *models.OutboundJob, token *uuid.UUID, mime []byte) error {
	st, ok := s.store.(recipientStore)
	if !ok {
		return fmt.Errorf("per-recipient ledger unavailable")
	}
	recipients, e := st.ListOutboundRecipients(ctx, j.TenantID, j.ID)
	if e != nil {
		return e
	}
	if len(recipients) == 0 {
		return fmt.Errorf("recipient ledger is empty")
	}
	temporary, permanent := 0, 0
	for _, rcpt := range recipients {
		switch rcpt.State {
		case "accepted":
			continue
		case "permanent":
			permanent++
			continue
		case "uncertain":
			return s.store.MarkOutboundJobFailed(ctx, j.ID, token, "Recipient acceptance is uncertain; operator review required", false)
		}
		if err := s.ValidateJobAuthorization(ctx, j); err != nil {
			if authz.IsAuthzError(err) {
				return s.store.MarkOutboundJobFailed(ctx, j.ID, token, "authorization revoked: "+err.Error(), false)
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		started, err := st.BeginOutboundRecipient(ctx, j.ID, token, rcpt.Address)
		if err != nil {
			return err
		}
		if !started {
			return store.ErrOutboundUncertain
		}
		attempt := *j
		attempt.RcptTo = []string{rcpt.Address}
		var result *DeliveryResult
		var deliveryErr error
		suppressed, suppressErr := s.store.IsSuppressed(ctx, j.TenantID, rcpt.Address)
		if suppressErr != nil {
			deliveryErr = suppressErr
		} else if suppressed {
			deliveryErr = &textproto.Error{Code: 550, Msg: "Recipient suppressed"}
		} else {
			result, deliveryErr = s.adapter.Deliver(ctx, &attempt, mime)
		}
		if result != nil {
			// Protocol diagnostics are also retained on the recipient in the fenced
			// completion transaction. Attempt telemetry failure cannot erase progress.
			err = s.store.CreateOutboundAttempt(ctx, &models.OutboundAttempt{ID: uuid.New(), JobID: j.ID, TenantID: j.TenantID, Adapter: result.Adapter, Attempt: j.Attempts, SMTPCode: result.SMTPCode, SMTPResponse: result.SMTPResponse, RemoteHost: result.RemoteHost, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt, Error: result.Error})
			if err != nil {
				s.logger.Warn().Err(err).Msg("recording delivery telemetry")
			}
		}
		if errors.Is(deliveryErr, store.ErrOutboundUncertain) {
			return s.store.MarkOutboundJobFailed(ctx, j.ID, token, "Recipient acceptance uncertain: "+rcpt.Address, false)
		}
		state, code, diagnostic := "accepted", 250, "Accepted by next hop"
		if deliveryErr != nil {
			state = "temporary"
			code = 0
			diagnostic = deliveryErr.Error()
			var reply *textproto.Error
			if errors.As(deliveryErr, &reply) {
				code = reply.Code
				if code >= 500 && code < 600 {
					state = "permanent"
				}
			}
		}
		if err = st.CompleteOutboundRecipient(ctx, j.ID, token, rcpt.Address, state, code, diagnostic); err != nil {
			return fmt.Errorf("%w: recipient checkpoint: %v", store.ErrOutboundUncertain, err)
		}
		if state == "temporary" {
			temporary++
		}
		if state == "permanent" {
			permanent++
		}
	}
	if temporary > 0 {
		return fmt.Errorf("%d recipient(s) temporarily failed; accepted recipients will not be resent", temporary)
	}
	if permanent > 0 {
		return s.store.MarkOutboundJobFailed(ctx, j.ID, token, fmt.Sprintf("%d recipient(s) permanently rejected; remaining recipients accepted", permanent), false)
	}
	return s.store.MarkOutboundJobSent(ctx, j.ID, token, 250, "Accepted by next hop", j.MessageIDHeader)
}
