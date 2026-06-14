package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/template"
	"tabmail/internal/workqueue"
)

const maxRetryDelay = 1 * time.Hour

// Service manages outbound email submission and background delivery.
type Service struct {
	cfg      config.Outbound
	store    store.Store
	adapter  DeliveryAdapter
	logger   zerolog.Logger
	template *template.Service
	// worker drives the background delivery loop. Built lazily in StartWorker
	// so a disabled service stays a no-op.
	worker *workqueue.Worker[*outboundJob]
}

// SetTemplateService wires the optional template renderer. Pass nil (or skip
// the call) to keep the legacy bare-string send path byte-for-byte unchanged;
// Submit only consults templates when req.TemplateName is non-nil AND a
// service is set.
func (s *Service) SetTemplateService(ts *template.Service) { s.template = ts }

// NewService creates a new outbound service.
func NewService(cfg config.Outbound, st store.Store, logger zerolog.Logger) *Service {
	var adapter DeliveryAdapter
	switch cfg.Mode {
	case "direct":
		adapter = NewDirectAdapter(cfg.RequireTLS)
	default:
		adapter = NewRelayAdapter(cfg)
	}
	return &Service{
		cfg:     cfg,
		store:   st,
		adapter: adapter,
		logger:  logger.With().Str("component", "outbound").Logger(),
	}
}

// SendRequest is the validated input for submitting an outbound email.
type SendRequest struct {
	TenantID      uuid.UUID
	UserID        *uuid.UUID
	APIKeyID      *uuid.UUID
	ZoneID        uuid.UUID
	From          string
	To            []string
	CC            []string
	BCC           []string
	Subject       string
	TextBody      string
	HTMLBody      string
	Headers       map[string]string
	TemplateName  *string           // optional; nil keeps the legacy bare-string path
	TemplateVars  map[string]string // used only when TemplateName is non-nil
	Quota         store.OutboundQuotaReservation
}

// Submit enqueues an outbound email job after validation.
func (s *Service) Submit(ctx context.Context, req SendRequest) (*models.OutboundJob, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("outbound sending is disabled")
	}

	// Template path (opt-in). When TemplateName is set the caller wants the
	// Subject/Text/HTML populated by rendering a tenant template; we do that
	// up front so the existing validators below run against the rendered
	// values. A nil template service with a non-nil TemplateName is a
	// configuration error, not a silent fall-through to bare strings.
	if req.TemplateName != nil && *req.TemplateName != "" {
		if s.template == nil {
			return nil, fmt.Errorf("template support is not configured")
		}
		rendered, err := s.template.Render(template.RenderInput{
			TenantID: req.TenantID,
			Name:     *req.TemplateName,
			Vars:     req.TemplateVars,
		})
		if err != nil {
			return nil, fmt.Errorf("render template %q: %w", *req.TemplateName, err)
		}
		req.Subject = rendered.Subject
		req.TextBody = rendered.TextBody
		req.HTMLBody = rendered.HTMLBody
	}

	// Validate all email addresses using RFC 5322 parsing.
	if _, err := mail.ParseAddress(req.From); err != nil {
		return nil, fmt.Errorf("invalid from address %q: %w", req.From, err)
	}
	for _, addr := range req.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, fmt.Errorf("invalid to address %q: %w", addr, err)
		}
	}
	for _, addr := range req.CC {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, fmt.Errorf("invalid cc address %q: %w", addr, err)
		}
	}
	for _, addr := range req.BCC {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, fmt.Errorf("invalid bcc address %q: %w", addr, err)
		}
	}

	// Merge all recipients.
	allRcpt := make([]string, 0, len(req.To)+len(req.CC)+len(req.BCC))
	allRcpt = append(allRcpt, req.To...)
	allRcpt = append(allRcpt, req.CC...)
	allRcpt = append(allRcpt, req.BCC...)

	if len(allRcpt) == 0 {
		return nil, fmt.Errorf("at least one recipient required")
	}
	if len(allRcpt) > 50 {
		return nil, fmt.Errorf("too many recipients (max 50)")
	}
	if req.Subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if len(req.Subject) > 998 {
		return nil, fmt.Errorf("subject too long (max 998 chars)")
	}
	if req.TextBody == "" && req.HTMLBody == "" {
		return nil, fmt.Errorf("text_body or html_body required")
	}

	// Build Message-ID header.
	msgID := fmt.Sprintf("<%s@%s>", uuid.New().String(), extractDomain(req.From))

	// Persist only the caller-supplied custom headers. Recipients are stored
	// structurally (To/CC/BCC below), not flattened into the header blob, so the
	// builder never reverse-engineers them and BCC cannot leak into a header.
	var headersJSON json.RawMessage
	if len(req.Headers) > 0 {
		b, err := json.Marshal(req.Headers)
		if err != nil {
			return nil, fmt.Errorf("invalid headers: %w", err)
		}
		headersJSON = b
	}

	now := time.Now().UTC()
	job := &models.OutboundJob{
		ID:              uuid.New(),
		TenantID:        req.TenantID,
		UserID:          req.UserID,
		APIKeyID:        req.APIKeyID,
		MailFrom:        req.From,
		RcptTo:          allRcpt,
		To:              req.To,
		CC:              req.CC,
		BCC:             req.BCC,
		Subject:         req.Subject,
		TextBody:        req.TextBody,
		HTMLBody:        req.HTMLBody,
		HeadersJSON:     headersJSON,
		ZoneID:          req.ZoneID,
		State:           models.OutboundPending,
		MaxAttempts:     s.cfg.MaxRetries,
		MessageIDHeader: msgID,
		CreatedAt:       now,
		UpdatedAt:       now,
		NextAttemptAt:   now,
	}

	if err := s.createOutboundJob(ctx, job, req.Quota); err != nil {
		return nil, fmt.Errorf("enqueue outbound job: %w", err)
	}

	s.logger.Info().
		Str("job_id", job.ID.String()).
		Str("from", job.MailFrom).
		Int("rcpt_count", len(allRcpt)).
		Msg("outbound job enqueued")

	return job, nil
}

func (s *Service) createOutboundJob(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation) error {
	if quota.HasLimits() {
		return s.store.CreateOutboundJobWithQuota(ctx, job, quota)
	}
	return s.store.CreateOutboundJob(ctx, job)
}

// StartWorker begins the background delivery worker loop. It is the
// goroutine-shape entry point (outbound's legacy form); Stop drains it.
func (s *Service) StartWorker(ctx context.Context) {
	if !s.cfg.Enabled {
		s.logger.Info().Msg("outbound disabled, worker not started")
		return
	}
	s.ensureWorker().Start(ctx)
	s.logger.Info().Msg("outbound worker started")
}

// Stop drains a StartWorker-launched goroutine, waiting for the in-flight
// batch to finish. Absorbs the legacy Shutdown() semantics.
func (s *Service) Stop() {
	if s.worker == nil {
		return
	}
	s.worker.Stop()
}

// Shutdown is retained for the main goroutine's existing call site; it
// forwards to Stop.
func (s *Service) Shutdown() { s.Stop() }

// ensureWorker builds the workqueue.Worker once. Idempotent.
func (s *Service) ensureWorker() *workqueue.Worker[*outboundJob] {
	if s.worker != nil {
		return s.worker
	}
	policy := workqueue.ExponentialCappedBackoff[*outboundJob]{
		Base:         s.cfg.RetryDelay,
		Cap:          maxRetryDelay,
		MaxAttempts:  func(j *workqueue.Job[*outboundJob]) int { return j.Payload.MaxAttempts },
	}
	s.worker = workqueue.NewWorker[*outboundJob](
		newOutboundStore(s.store),
		s.processOne,
		policy,
		nil,
		5*time.Minute,
		s.cfg.PollInterval,
		s.cfg.BatchSize,
		s.logger,
	)
	return s.worker
}

// processOne is the workqueue.Handler for outbound jobs. It performs the full
// delivery sequence (MIME build, zone load, DKIM sign, adapter deliver,
// attempt record, mark sent) and returns nil on success or an error to drive
// retry/dead through the ExponentialCappedBackoff policy. A delivery-token
// mismatch on the success mark is logged and swallowed (the job was
// re-claimed), matching the legacy skip-on-mismatch behavior.
func (s *Service) processOne(ctx context.Context, job *workqueue.Job[*outboundJob]) error {
	if job == nil || job.Payload == nil || job.Payload.OutboundJob == nil {
		return nil
	}
	out := job.Payload.OutboundJob
	log := s.logger.With().Str("job_id", out.ID.String()).Logger()

	// Build MIME message from the structurally-stored recipients.
	mime, err := Build(messageFromJob(out))
	if err != nil {
		log.Error().Err(err).Msg("building MIME")
		return fmt.Errorf("mime build: %s", err)
	}

	zone, zoneErr := s.store.GetZone(ctx, out.ZoneID)
	if zoneErr != nil {
		if s.dkimFailClosed() {
			log.Error().Err(zoneErr).Msg("loading zone for DKIM, delivery blocked by policy")
			return fmt.Errorf("load zone for dkim: %s", zoneErr)
		}
		log.Warn().Err(zoneErr).Msg("loading zone for DKIM")
	}
	if zone != nil && zone.DKIMRequiredForSend && !s.cfg.DKIMSign {
		log.Error().Msg("zone requires DKIM for send but global DKIM signing is disabled")
		return fmt.Errorf("zone requires DKIM but global signing is disabled")
	}

	// DKIM sign if enabled for this zone.
	if s.cfg.DKIMSign {
		if zone != nil && zone.DKIMEnabled && zone.DKIMPrivateKeyPEM != nil {
			selector := strings.TrimSpace(zone.DKIMSelector)
			if selector == "" {
				selector = tabdkim.DefaultSelector
			}
			signed, signErr := tabdkim.SignMessage(mime, zone.Domain, selector, *zone.DKIMPrivateKeyPEM)
			if signErr != nil {
				if s.dkimFailClosed() || zone.DKIMRequiredForSend {
					log.Error().Err(signErr).Msg("DKIM signing failed, delivery blocked by policy")
					return fmt.Errorf("dkim sign: %s", signErr)
				}
				log.Warn().Err(signErr).Msg("DKIM signing failed, delivering unsigned (fail_open)")
			} else {
				mime = signed
			}
		} else if zone != nil && zone.DKIMRequiredForSend {
			log.Error().Msg("zone requires DKIM for send but DKIM is not enabled")
			return fmt.Errorf("zone requires DKIM but signing is not configured")
		}
	}

	// Deliver via configured adapter.
	result, deliverErr := s.adapter.Deliver(ctx, out, mime)

	// Record the attempt.
	if result != nil {
		attempt := &models.OutboundAttempt{
			ID:           uuid.New(),
			JobID:        out.ID,
			TenantID:     out.TenantID,
			Adapter:      result.Adapter,
			Attempt:      out.Attempts,
			SMTPCode:     result.SMTPCode,
			SMTPResponse: result.SMTPResponse,
			RemoteHost:   result.RemoteHost,
			StartedAt:    result.StartedAt,
			FinishedAt:   result.FinishedAt,
			Error:        result.Error,
		}
		if storeErr := s.store.CreateOutboundAttempt(ctx, attempt); storeErr != nil {
			log.Warn().Err(storeErr).Msg("recording delivery attempt")
		}
	}

	if deliverErr != nil {
		log.Warn().Err(deliverErr).Int("attempt", out.Attempts+1).Msg("delivery failed")
		// Hard bounce (5xx) → add recipients to suppression list.
		if result != nil && result.SMTPCode >= 500 && result.SMTPCode < 600 {
			for _, rcpt := range out.RcptTo {
				_ = s.store.AddSuppression(ctx, &models.SuppressionEntry{
					ID:          uuid.New(),
					TenantID:    out.TenantID,
					Address:     rcpt,
					Reason:      "hard_bounce",
					SourceJobID: &out.ID,
					CreatedAt:   time.Now(),
				})
			}
			log.Info().Int("smtp_code", result.SMTPCode).Msg("hard bounce: recipients added to suppression list")
		}
		return fmt.Errorf("%s", deliverErr.Error())
	}

	smtpCode := 250
	smtpResponse := "OK"
	if result != nil && result.SMTPCode > 0 {
		smtpCode = result.SMTPCode
		smtpResponse = result.SMTPResponse
	}
	if err := s.store.MarkOutboundJobSent(ctx, out.ID, job.Lease.Token, smtpCode, smtpResponse, out.MessageIDHeader); err != nil {
		if isTokenMismatch(err) {
			log.Warn().Msg("delivery token mismatch on mark-sent; job was re-claimed by another worker, skipping")
			return nil
		}
		log.Error().Err(err).Msg("marking job sent")
		return nil
	}
	log.Info().Msg("outbound delivered")
	return nil
}

func (s *Service) dkimFailClosed() bool {
	return strings.ToLower(strings.TrimSpace(s.cfg.DKIMFailPolicy)) != config.DKIMFailOpen
}

// DKIMSendBlockReason returns a non-empty reason when the zone's DKIM policy
// makes a send impossible to satisfy with the current configuration. Callers use
// it to reject synchronously at submit time instead of accepting a job that the
// delivery worker can only ever drive to dead (deliverJob keeps the same checks
// as defense in depth).
func (s *Service) DKIMSendBlockReason(zone *models.DomainZone) string {
	if zone == nil || !zone.DKIMRequiredForSend {
		return ""
	}
	if !s.cfg.DKIMSign {
		return "zone requires DKIM for send but outbound DKIM signing is disabled"
	}
	if !zone.DKIMEnabled || zone.DKIMPrivateKeyPEM == nil {
		return "zone requires DKIM for send but no DKIM key is configured for the zone"
	}
	return ""
}

// isTokenMismatch checks if the error is a delivery token mismatch sentinel.
func isTokenMismatch(err error) bool {
	return err != nil && err.Error() == "delivery token mismatch: job was re-claimed"
}

func extractDomain(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			return addr[i+1:]
		}
	}
	return "localhost"
}
