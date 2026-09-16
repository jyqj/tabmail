package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"tabmail/internal/classify"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/store"
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
	for _, address := range env.Recipients {
		resolved, err := s.resolver.Resolve(ctx, policy.SanitizeAddr(address))
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
	targets, err := ledger.ListIngressTargets(ctx, c.Job.ID)
	if err != nil {
		return err
	}
	rc, readErr := s.obj.Get(ctx, c.Job.RawObjectKey)
	var raw []byte
	if readErr == nil {
		raw, readErr = io.ReadAll(io.LimitReader(rc, c.RawSize+1))
		closeErr := rc.Close()
		if readErr == nil {
			readErr = closeErr
		}
	}
	if readErr == nil && (int64(len(raw)) != c.RawSize || fmt.Sprintf("%x", sha256.Sum256(raw)) != c.RawHash) {
		readErr = permanentIngress("original object size/checksum mismatch; receipt held for recovery")
	}
	var subject string
	var headers json.RawMessage
	if readErr == nil {
		env, _ := enmime.ReadEnvelope(bytes.NewReader(raw))
		if env != nil {
			subject = env.GetHeader("Subject")
			hm := map[string]string{}
			for _, k := range []string{"From", "To", "Cc", "Date", "Message-Id", "In-Reply-To", "References", "Reply-To", "Content-Type"} {
				if v := env.GetHeader(k); v != "" {
					hm[k] = v
				}
			}
			headers, _ = json.Marshal(hm)
		}
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
			return err
		}
		err = readErr
		if err == nil {
			err = s.deliverTarget(ctx, ledger, c, target, raw, subject, headers)
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

func (s *Service) deliverTarget(ctx context.Context, ledger store.IngressLedger, c *store.IngressClaim, t store.IngressTarget, raw []byte, subject string, headers json.RawMessage) error {
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
	if !policy.ShouldStoreDomain(mb.ResolvedDomain, pol.DefaultStore, pol.StoreDomains, pol.DiscardDomains) {
		return permanentIngress("storage policy blocks delivery; accepted bytes retained")
	}
	cfg, err := s.store.EffectiveConfig(ctx, t.TenantID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("tenant configuration unavailable")
	}
	if cfg.MaxMessageBytes > 0 && len(raw) > cfg.MaxMessageBytes {
		return permanentIngress("tenant size limit exceeded; accepted bytes retained")
	}
	var routeRetention *int
	if mb.RouteID != nil {
		route, lookupErr := s.store.GetRoute(ctx, *mb.RouteID)
		if lookupErr != nil {
			return lookupErr
		}
		if route != nil {
			routeRetention = route.RetentionHoursOverride
		}
	}
	retention := resolveRetention(mb.RetentionHoursOverride, routeRetention, &cfg.RetentionHours, s.fallbackRetentionH)
	m := &models.Message{TenantID: t.TenantID, MailboxID: t.MailboxID, ZoneID: t.ZoneID, Sender: c.Job.MailFrom,
		Recipients: []string{t.Address}, Subject: subject, Size: int64(len(raw)), RawObjectKey: c.Job.RawObjectKey, HeadersJSON: headers,
		ExpiresAt: models.MessageExpiry(mb, retention, c.Job.CreatedAt)}
	env, _ := enmime.ReadEnvelope(bytes.NewReader(raw))
	if env != nil {
		otp := classify.OTPFromMessage(classify.Env{Subject: subject, TextBody: env.Text, HTMLBody: env.HTML, From: c.Job.MailFrom})
		if otp.Found {
			m.OTPCode = otp.Code
			m.OTPConfidence = otp.Confidence
		}
	}
	inserted, err := ledger.DeliverIngress(ctx, c, m, cfg.MaxMessagesPerMailbox, cfg.DailyQuota)
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
