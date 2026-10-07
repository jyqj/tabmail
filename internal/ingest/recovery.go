package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/store"

	"github.com/google/uuid"
)

func (s *Service) acceptDurable(ctx context.Context, env Envelope, raw []byte) (AcceptResult, error) {
	ledger, ok := s.store.(store.IngressLedger)
	if !ok {
		return AcceptResult{}, fmt.Errorf("durable ingress requires a transactional destination ledger")
	}
	// Freeze destination IDs before acknowledgement. A subsequent rename or
	// delete/recreate must never send accepted mail into a different mailbox.
	targets := []store.IngressTarget{}
	seen := map[uuid.UUID]bool{}
	seenAddresses := map[string]bool{}
	for _, address := range env.Recipients {
		address = policy.SanitizeAddr(address)
		// Resolve each canonical envelope recipient once. Otherwise duplicate
		// spellings can observe routing changes within one accept and freeze
		// multiple identities for the same recipient. Distinct aliases below
		// still deduplicate by the resolved mailbox ID.
		if seenAddresses[address] {
			continue
		}
		seenAddresses[address] = true
		resolved, err := s.resolver.Resolve(ctx, address)
		if err != nil {
			return AcceptResult{}, err
		}
		if resolved == nil || resolved.Mailbox == nil || resolved.Zone == nil || !resolved.Zone.IsVerified || !resolved.Zone.MXVerified {
			return AcceptResult{}, fmt.Errorf("destination unavailable before durable acceptance")
		}
		mb := resolved.Mailbox
		if seen[mb.ID] {
			continue
		}
		seen[mb.ID] = true
		targets = append(targets, store.IngressTarget{MailboxID: mb.ID, TenantID: mb.TenantID, ZoneID: mb.ZoneID, Address: mb.FullAddress})
	}
	j := &models.IngestJob{ID: uuid.New(), Source: env.Source, RemoteIP: env.RemoteIP, MailFrom: env.MailFrom, Recipients: append([]string(nil), env.Recipients...), Metadata: env.Metadata}
	// Per-receipt immutable keys avoid sharing a reclaimable content-addressed
	// object with another accept. On uncertain commit failure leave an orphan;
	// deleting here could erase bytes from a committed but unacknowledged receipt.
	j.RawObjectKey = "ingress-" + j.ID.String() + ".eml"
	if err := s.obj.Put(ctx, j.RawObjectKey, bytes.NewReader(raw), int64(len(raw))); err != nil {
		return AcceptResult{}, err
	}
	if err := ledger.CreateIngress(ctx, j, targets, fmt.Sprintf("%x", sha256.Sum256(raw)), int64(len(raw))); err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{Queued: true}, nil
}

func (s *Service) processReceipt(ctx context.Context, ledger store.IngressLedger, c *store.IngressClaim) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targets, err := ledger.ListIngressTargets(ctx, c.Job.ID)
	if err != nil {
		return err
	}
	raw, readErr := s.readReceiptOriginal(ctx, c.Job.RawObjectKey, c.RawSize)
	if readErr == nil && (int64(len(raw)) != c.RawSize || fmt.Sprintf("%x", sha256.Sum256(raw)) != c.RawHash) {
		readErr = permanentIngress("original object size/checksum mismatch; receipt held for recovery")
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, readErr)
	}
	// One shared bounded parse per receipt: every replayed target sees the same
	// extracted subject/headers/OTP as the immediate path. The parse result is
	// never a substitute for re-verification — destinations, zone verification,
	// store policy, tenant config, retention and quota are all re-checked per
	// target below against current state.
	var content envelopeContent
	if readErr == nil {
		content = parseEnvelopeContent(s.logger, raw)
	}
	var failures []error
	for _, target := range targets {
		if target.State == "delivered" {
			continue
		}
		if target.State == "held" {
			failures = append(failures, errors.New("destination held for review"))
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(err, readErr)
		}
		err = readErr
		if err == nil {
			err = s.deliverTarget(ctx, ledger, c, target, raw, content)
		}
		if err != nil {
			failures = append(failures, err)
			metrics.MailboxRecipientRejected(target.Address)
			var permanent permanentIngress
			var persistErr error
			if errors.As(err, &permanent) {
				persistErr = ledger.HoldIngressTarget(ctx, c, target.MailboxID, err.Error())
			} else {
				persistErr = ledger.FailIngressTarget(ctx, c, target.MailboxID, err.Error())
			}
			if persistErr != nil {
				return persistErr
			}
		}
	}
	// A partial failure must not appear as a successful handler result. Marks
	// below are fenced and atomically inspect the durable target ledger.
	return errors.Join(failures...)
}

type permanentIngress string

func (e permanentIngress) Error() string { return string(e) }

// deliverTarget replays one accepted target. The shells own every re-verified
// mutable precondition that depends on the frozen identity (mailbox identity,
// zone verification, route re-fetch); the shared prepareDelivery kernel runs
// store policy, tenant config, size limit, retention and Message construction
// against current state. The shared content only carries the immutable parse
// product (subject/headers/OTP). Terminal kernel rejections map to
// permanentIngress so accepted bytes are held for review, never discarded.
func (s *Service) deliverTarget(ctx context.Context, ledger store.IngressLedger, c *store.IngressClaim, t store.IngressTarget, raw []byte, content envelopeContent) error {
	mb, err := s.store.GetMailbox(ctx, t.MailboxID)
	if err != nil {
		return err
	}
	if mb == nil || mb.TenantID != t.TenantID || mb.ZoneID != t.ZoneID || mb.FullAddress != t.Address {
		return permanentIngress("accepted destination changed; retained for review")
	}
	zone, err := s.store.GetZone(ctx, t.ZoneID)
	if err != nil {
		return err
	}
	if zone == nil || zone.TenantID != t.TenantID || !zone.IsVerified || !zone.MXVerified {
		return permanentIngress("destination no longer verified; retained for review")
	}
	pol, err := s.currentPolicy(ctx)
	if err != nil {
		return err
	}
	plan, pf := s.prepareDelivery(ctx, deliveryInput{
		pol: pol, mb: mb, content: content, raw: raw, objKey: c.Job.RawObjectKey,
		rcpt: t.Address, from: c.Job.MailFrom, at: c.Job.CreatedAt,
		routeFn: func(ctx context.Context) (*models.DomainRoute, error) {
			if mb.RouteID == nil {
				return nil, nil
			}
			return s.store.GetRoute(ctx, *mb.RouteID)
		},
	})
	if pf != nil {
		if pf.terminal() {
			switch pf.code {
			case rejectStorePolicyDiscard:
				return permanentIngress("storage policy blocks delivery; accepted bytes retained")
			case rejectMaxMessageBytes:
				return permanentIngress("tenant size limit exceeded; accepted bytes retained")
			default:
				return permanentIngress(pf.code)
			}
		}
		return pf.err
	}
	m := plan.msg
	inserted, err := ledger.DeliverIngress(ctx, c, m, plan.cfg.MaxMessagesPerMailbox, plan.cfg.DailyQuota)
	if err != nil {
		return err
	}
	if inserted {
		metrics.SMTPDeliverySucceeded(t.TenantID.String(), t.Address)
		if s.hub != nil {
			s.hub.Publish(realtime.Event{Type: realtime.EventMessage, Mailbox: t.Address, MessageID: m.ID.String(), Sender: m.Sender, Subject: m.Subject, Size: m.Size})
		}
		// The durable outbox is inserted inside DeliverIngress. Do not Publish the
		// webhook a second time; realtime remains best effort (clients can poll).
	}
	return nil
}
