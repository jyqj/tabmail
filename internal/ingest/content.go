package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"

	"tabmail/internal/classify"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

// ingestHeaders is the canonical header allowlist extracted once per envelope
// and stored as HeadersJSON for every recipient. Both delivery paths (immediate
// deliverResolved and durable processReceipt) share this list so the two paths
// store identical header sets; In-Reply-To and References keep reply threading
// queryable on the read side.
var ingestHeaders = []string{
	"From", "To", "Cc", "Date", "Message-Id",
	"In-Reply-To", "References", "Reply-To", "Content-Type",
}

// envelopeContent is the per-envelope parse product shared by every recipient
// of one delivery attempt: the bounded-parsed envelope (nil when MIME was
// unusable) plus the subject and allowlisted headers extracted from it.
type envelopeContent struct {
	env     *enmime.Envelope
	subject string
	headers json.RawMessage
}

// parseEnvelopeContent runs the shared bounded MIME parse (mailcontent limits:
// 25 MiB raw, 512 parts) followed by the shared header extraction. A parse
// failure is degraded, never fatal: both paths still store the raw bytes and
// deliver the message, only the extracted fields are omitted — the historical
// tolerant behavior of each path, now defined in exactly one place.
func parseEnvelopeContent(logger zerolog.Logger, raw []byte) envelopeContent {
	env, err := mailcontent.ParseBounded(raw)
	if err != nil {
		logger.Warn().Err(err).Msg("bounded MIME parse failed (storing raw only)")
		return envelopeContent{}
	}
	c := envelopeContent{env: env, subject: env.GetHeader("Subject")}
	hm := make(map[string]string)
	for _, key := range ingestHeaders {
		if v := env.GetHeader(key); v != "" {
			hm[key] = v
		}
	}
	c.headers, _ = json.Marshal(hm)
	return c
}

// applyOTP fills the OTP fields on msg from the already-parsed envelope (no
// extra decode). Fields stay zero-value when MIME was unusable or no code was
// found, preserving the omitempty JSON behavior of both delivery paths.
func applyOTP(msg *models.Message, c envelopeContent, from string) {
	if c.env == nil {
		return
	}
	otp := classify.OTPFromMessage(classify.Env{
		Subject:  c.subject,
		TextBody: c.env.Text,
		HTMLBody: c.env.HTML,
		From:     from,
	})
	if otp.Found {
		msg.OTPCode = otp.Code
		msg.OTPConfidence = otp.Confidence
	}
}

// storePolicyAllows is the shared store/discard decision for both delivery
// paths. The durable path re-runs it per replay target on purpose: accepted
// bytes are held for review when the policy later flips, never silently
// discarded.
func storePolicyAllows(pol *models.SMTPPolicy, mb *models.Mailbox) bool {
	return policy.ShouldStoreDomain(mb.ResolvedDomain, pol.DefaultStore, pol.StoreDomains, pol.DiscardDomains)
}

// Terminal rejection codes produced by the shared delivery kernel. The
// immediate shell surfaces them verbatim as RecipientOutcome.Reason; the
// durable shell maps them to permanentIngress holds so accepted bytes are
// retained for review instead of silently dropped.
const (
	rejectStorePolicyDiscard = "store_policy_discard"
	rejectMaxMessageBytes    = "max_message_bytes"
)

// Transient stage names produced by the shared delivery kernel. These are
// retryable infrastructure failures, never permission to discard accepted
// bytes; the immediate shell surfaces them as RecipientOutcome.Reason on a
// RecipientError, the durable shell returns the wrapped error for retry.
const (
	stageTenantConfig = "tenant_config"
	stageRouteLoad    = "route_error"
)

// deliveryPlan is the shared prepare-stage product both shells consume: the
// tenant-effective config that still gates quota at persistence time, plus the
// fully constructed Message (subject, allowlisted headers, OTP fields,
// retention expiry) ready for the shell's own persistence call.
type deliveryPlan struct {
	cfg *models.EffectiveConfig
	msg *models.Message
}

// deliveryFailure is the single prepare-stage failure shape. A terminal failure
// (code set) is a permanent per-recipient policy/size rejection; a transient
// failure (stage set) is retryable. Terminal-vs-transient is decided here —
// once — so the two shells cannot drift in classifying the same input.
type deliveryFailure struct {
	code  string
	stage string
	err   error
}

func (f *deliveryFailure) terminal() bool { return f.code != "" }

// deliveryInput bundles one recipient's resolved delivery inputs for the shared
// kernel. Destination/zone resolution and identity re-verification stay in the
// shells on purpose: the immediate path resolves through the resolver (zone
// gate = CanReceiveMessage), the durable path re-verifies the frozen
// IngressTarget identity against current store state (IsVerified && MXVerified).
type deliveryInput struct {
	pol     *models.SMTPPolicy
	mb      *models.Mailbox
	content envelopeContent
	raw     []byte
	objKey  string
	// rcpt is the envelope-side recipient address stored on the Message. The
	// immediate shell passes the sanitized SMTP recipient (which may carry a
	// plus-extension), the durable shell the frozen target address; the
	// historical paths stored exactly these values, so the kernel must not
	// substitute mb.FullAddress.
	rcpt string
	from string
	at   time.Time
	// cfgCache optionally memoizes EffectiveConfig per tenant across the
	// recipients of one envelope (immediate path). Nil — durable replay —
	// loads fresh per target so re-verification observes current state.
	cfgCache map[uuid.UUID]*models.EffectiveConfig
	// routeFn resolves the route retention-override source. It is consulted
	// only after the size gate, so a size-rejected target never pays a route
	// lookup, preserving both shells' original ordering: the immediate shell
	// returns the resolver-matched route, the durable shell re-fetches by
	// mailbox RouteID from current store state.
	routeFn func(context.Context) (*models.DomainRoute, error)
}

// prepareDelivery is the shared per-recipient delivery kernel for both shells
// (immediate deliverResolved and durable deliverTarget). It runs the identical
// gate sequence both paths historically duplicated — store policy → tenant
// EffectiveConfig → tenant size limit → mailbox > route > tenant > fallback
// retention — and returns either a plan (effective config + fully constructed
// Message, OTP included) or one classified failure. Its only I/O is
// EffectiveConfig and routeFn. Quota reservation, the persistence transaction
// (raw reference + message [+ outbox] vs DeliverIngress), raw admission and
// events remain shell-owned: a durable ledger transaction and a non-durable
// reservation+insert are not equivalent and must not be collapsed.
func (s *Service) prepareDelivery(ctx context.Context, in deliveryInput) (*deliveryPlan, *deliveryFailure) {
	if !storePolicyAllows(in.pol, in.mb) {
		s.logger.Info().Str("mailbox", in.mb.FullAddress).Msg("message accepted but discarded by store policy")
		return nil, &deliveryFailure{code: rejectStorePolicyDiscard}
	}
	var cfg *models.EffectiveConfig
	if in.cfgCache != nil {
		cfg = in.cfgCache[in.mb.TenantID]
	}
	if cfg == nil {
		var err error
		cfg, err = s.store.EffectiveConfig(ctx, in.mb.TenantID)
		if err != nil {
			s.logger.Warn().Err(err).Str("mailbox", in.mb.FullAddress).Msg("load tenant config")
			return nil, &deliveryFailure{stage: stageTenantConfig, err: err}
		}
		if cfg == nil {
			s.logger.Warn().Str("mailbox", in.mb.FullAddress).Msg("load tenant config")
			return nil, &deliveryFailure{stage: stageTenantConfig, err: errors.New("tenant configuration unavailable")}
		}
		if in.cfgCache != nil {
			in.cfgCache[in.mb.TenantID] = cfg
		}
	}
	if cfg.MaxMessageBytes > 0 && len(in.raw) > cfg.MaxMessageBytes {
		s.logger.Warn().
			Str("mailbox", in.mb.FullAddress).
			Int("limit", cfg.MaxMessageBytes).
			Int("size", len(in.raw)).
			Msg("tenant max message bytes exceeded")
		return nil, &deliveryFailure{code: rejectMaxMessageBytes}
	}
	route, err := in.routeFn(ctx)
	if err != nil {
		return nil, &deliveryFailure{stage: stageRouteLoad, err: err}
	}
	mbRetention, routeRetention, tenantRetention := retentionOf(in.mb, route, cfg)
	retH := resolveRetention(mbRetention, routeRetention, tenantRetention, s.fallbackRetentionH)
	msg := &models.Message{
		TenantID:     in.mb.TenantID,
		MailboxID:    in.mb.ID,
		ZoneID:       in.mb.ZoneID,
		Sender:       in.from,
		Recipients:   []string{in.rcpt},
		Subject:      in.content.subject,
		Size:         int64(len(in.raw)),
		RawObjectKey: in.objKey,
		HeadersJSON:  in.content.headers,
		ExpiresAt:    models.MessageExpiry(in.mb, retH, in.at),
	}
	// OTP extraction reuses the shared parsed envelope (no extra decode).
	// OTPCode/OTPConfidence stay zero-value when MIME was unusable or nothing
	// is found, so the omitempty JSON path and existing tests are unaffected.
	applyOTP(msg, in.content, in.from)
	return &deliveryPlan{cfg: cfg, msg: msg}, nil
}
