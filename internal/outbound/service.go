package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
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
	workerMu   sync.Mutex
	cfg        config.Outbound
	store      store.Store
	adapter    DeliveryAdapter
	logger     zerolog.Logger
	template   *template.Service
	objects    store.ObjectStore
	governance TemplateGovernance
	// worker drives the background delivery loop. Built lazily in StartWorker
	// so a disabled service stays a no-op.
	worker *workqueue.Worker[*outboundJob]
}

// SetTemplateService wires the optional template renderer. Pass nil (or skip
// the call) to keep the legacy bare-string send path byte-for-byte unchanged;
// Submit only consults templates when req.TemplateName is non-nil AND a
// service is set.
func (s *Service) SetTemplateService(ts *template.Service) { s.template = ts }

// NewService creates a new outbound service. gov is the published-template
// governance dependency (the production Postgres store implements it); a nil
// gov fails construction instead of degrading at send time.
func NewService(cfg config.Outbound, st store.Store, gov TemplateGovernance, logger zerolog.Logger) *Service {
	if gov == nil {
		panic("outbound: template governance dependency is required (pass the store that implements TemplateForSend)")
	}
	var adapter DeliveryAdapter
	switch cfg.Mode {
	case "direct":
		adapter = NewDirectAdapter(cfg.RequireTLS)
	default:
		adapter = NewRelayAdapter(cfg)
	}
	return &Service{
		cfg:        cfg,
		store:      st,
		adapter:    adapter,
		governance: gov,
		logger:     logger.With().Str("component", "outbound").Logger(),
	}
}

// SendRequest is the validated input for submitting an outbound email.
type SendRequest struct {
	SenderMailboxID   *uuid.UUID
	TenantID          uuid.UUID
	UserID            *uuid.UUID
	APIKeyID          *uuid.UUID
	ZoneID            uuid.UUID
	From              string
	To                []string
	CC                []string
	BCC               []string
	Subject           string
	TextBody          string
	HTMLBody          string
	Headers           map[string]string
	TemplateVersionID *uuid.UUID
	AttachmentIDs     []uuid.UUID
	IdempotencyKey    string
	TemplateName      *string           // optional; nil keeps the legacy bare-string path
	TemplateVars      map[string]string // used only when TemplateName is non-nil
	Quota             store.OutboundQuotaReservation
	// Draft, when non-nil, makes the enqueue transaction also consume (delete)
	// this exact mail-draft revision atomically; a concurrent or repeated
	// consumption rolls the whole enqueue back with store.ErrDraftAlreadyConsumed.
	Draft *store.DraftConsumption
}

// Submit enqueues an outbound email job after validation.
func (s *Service) Submit(ctx context.Context, req SendRequest) (*models.OutboundJob, error) {
	job, _, err := s.SubmitWithReplay(ctx, req)
	return job, err
}

// ConsumedDraftSubmission classifies a missing draft for the submit endpoint.
// When a job consumed this draft it returns that job and whether the current
// request (same submit actor and idempotency key) is a replay of it; a draft
// that was never submitted yields (nil, false, nil).
func (s *Service) ConsumedDraftSubmission(ctx context.Context, tenantID, draftID uuid.UUID, userID *uuid.UUID, key string) (*models.OutboundJob, bool, error) {
	repo, ok := s.store.(interface {
		FindOutboundJobByDraft(context.Context, uuid.UUID, uuid.UUID) (*models.OutboundJob, error)
	})
	if !ok {
		return nil, false, nil
	}
	job, err := repo.FindOutboundJobByDraft(ctx, tenantID, draftID)
	if err != nil || job == nil {
		return nil, false, err
	}
	return job, job.SubmitActor == submissionActor(userID, nil) && job.IdempotencyKey == key, nil
}

// SubmitWithReplay is Submit with an idempotent-replay signal: replayed is true when the
// (submit_actor, idempotency_key) pair already owned a job with the same
// request hash and that original job was returned unchanged. HTTP callers use
// it to answer 201 for a fresh submission and 200 for a replay.
func (s *Service) SubmitWithReplay(ctx context.Context, req SendRequest) (*models.OutboundJob, bool, error) {
	req.To = append([]string{}, req.To...)
	req.CC = append([]string{}, req.CC...)
	req.BCC = append([]string{}, req.BCC...)
	if !s.cfg.Enabled {
		return nil, false, fmt.Errorf("outbound sending is disabled")
	}

	canonical, err := authz.CanonicalSender(req.From)
	if err != nil {
		return nil, false, err
	}
	req.From = canonical
	for _, group := range [][]string{req.To, req.CC, req.BCC} {
		for i, a := range group {
			parsed, parseErr := mail.ParseAddress(a)
			if parseErr != nil {
				return nil, false, parseErr
			}
			group[i] = strings.ToLower(parsed.Address)
		}
	}
	if len(req.IdempotencyKey) > 128 || strings.ContainsAny(req.IdempotencyKey, "\r\n") {
		return nil, false, app.BadRequest("idempotency key must be at most 128 bytes")
	}
	actor, hash := submissionActor(req.UserID, req.APIKeyID), requestDigest(req)
	if req.IdempotencyKey != "" {
		repo, ok := s.store.(interface {
			FindOutboundSubmission(context.Context, uuid.UUID, string, string, string) (*models.OutboundJob, error)
		})
		if !ok {
			return nil, false, app.BadRequest("idempotent submission unavailable")
		}
		old, e := repo.FindOutboundSubmission(ctx, req.TenantID, actor, req.IdempotencyKey, hash)
		if e != nil {
			return nil, false, e
		}
		if old != nil {
			return old, true, nil
		}
	}
	if req.TemplateVersionID != nil {
		if req.TemplateName != nil {
			return nil, false, app.BadRequest("select a published version or legacy name, not both")
		}
		if req.SenderMailboxID == nil {
			return nil, false, app.BadRequest("published templates require an employee mailbox")
		}
		if s.governance == nil {
			return nil, false, app.BadRequest("published templates unavailable")
		}
		v, employee, name, e := s.governance.TemplateForSend(ctx, req.TenantID, req.UserID, req.APIKeyID, *req.SenderMailboxID, *req.TemplateVersionID)
		if e != nil {
			return nil, false, e
		}
		req.Subject, req.TextBody, req.HTMLBody, e = company.Render(v.Snapshot, req.TemplateVars, employee, name, req.From)
		if e != nil {
			return nil, false, e
		}
	}
	// Template path (opt-in). When TemplateName is set the caller wants the
	// Subject/Text/HTML populated by rendering a tenant template; we do that
	// up front so the existing validators below run against the rendered
	// values. A nil template service with a non-nil TemplateName is a
	// configuration error, not a silent fall-through to bare strings.
	if req.TemplateName != nil && *req.TemplateName != "" {
		if s.template == nil {
			return nil, false, fmt.Errorf("template support is not configured")
		}
		rendered, err := s.template.Render(template.RenderInput{
			TenantID: req.TenantID,
			Name:     *req.TemplateName,
			Vars:     req.TemplateVars,
		})
		if err != nil {
			return nil, false, fmt.Errorf("render template %q: %w", *req.TemplateName, err)
		}
		req.Subject = rendered.Subject
		req.TextBody = rendered.TextBody
		req.HTMLBody = rendered.HTMLBody
	}

	// Validate all email addresses using RFC 5322 parsing.
	if _, err := mail.ParseAddress(req.From); err != nil {
		return nil, false, fmt.Errorf("invalid from address %q: %w", req.From, err)
	}
	for _, addr := range req.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, false, fmt.Errorf("invalid to address %q: %w", addr, err)
		}
	}
	for _, addr := range req.CC {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, false, fmt.Errorf("invalid cc address %q: %w", addr, err)
		}
	}
	for _, addr := range req.BCC {
		if _, err := mail.ParseAddress(addr); err != nil {
			return nil, false, fmt.Errorf("invalid bcc address %q: %w", addr, err)
		}
	}

	// Merge all recipients.
	allRcpt := make([]string, 0, len(req.To)+len(req.CC)+len(req.BCC))
	allRcpt = append(allRcpt, req.To...)
	allRcpt = append(allRcpt, req.CC...)
	allRcpt = append(allRcpt, req.BCC...)

	if len(allRcpt) == 0 {
		return nil, false, fmt.Errorf("at least one recipient required")
	}
	if len(allRcpt) > 50 {
		return nil, false, fmt.Errorf("too many recipients (max 50)")
	}
	if req.Subject == "" {
		return nil, false, fmt.Errorf("subject is required")
	}
	if len(req.Subject) > 998 {
		return nil, false, fmt.Errorf("subject too long (max 998 chars)")
	}
	if req.TextBody == "" && req.HTMLBody == "" {
		return nil, false, fmt.Errorf("text_body or html_body required")
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
			return nil, false, fmt.Errorf("invalid headers: %w", err)
		}
		headersJSON = b
	}

	now := time.Now().UTC()
	var draftID *uuid.UUID
	if req.Draft != nil {
		id := req.Draft.ID
		draftID = &id
	}
	job := &models.OutboundJob{
		SenderUserID:      req.UserID,
		SenderKeyID:       req.APIKeyID,
		SenderMailboxID:   req.SenderMailboxID,
		TemplateName:      req.TemplateName,
		TemplateVersionID: req.TemplateVersionID, AttachmentIDs: append([]uuid.UUID(nil), req.AttachmentIDs...), IdempotencyKey: req.IdempotencyKey, SubmitActor: actor, RequestHash: hash, DraftID: draftID,
		DeliveredDomains: []string{},
		ID:               uuid.New(),
		TenantID:         req.TenantID,
		UserID:           req.UserID,
		APIKeyID:         req.APIKeyID,
		MailFrom:         req.From,
		RcptTo:           allRcpt,
		To:               req.To,
		CC:               req.CC,
		BCC:              req.BCC,
		Subject:          req.Subject,
		TextBody:         req.TextBody,
		HTMLBody:         req.HTMLBody,
		HeadersJSON:      headersJSON,
		ZoneID:           req.ZoneID,
		State:            models.OutboundPending,
		MaxAttempts:      s.cfg.MaxRetries,
		MessageIDHeader:  msgID,
		CreatedAt:        now,
		UpdatedAt:        now,
		NextAttemptAt:    now,
	}

	if _, ok := s.store.(recipientStore); ok {
		job.RecipientLedger = true
	}
	job.ContentDigest = contentDigest(job)
	if err := s.ValidateJobAuthorization(ctx, job); err != nil {
		return nil, false, err
	}
	replayed, err := s.createOutboundJob(ctx, job, req.Quota, req.Draft)
	if err != nil {
		return nil, false, fmt.Errorf("enqueue outbound job: %w", err)
	}

	s.logger.Info().
		Str("job_id", job.ID.String()).
		Str("from", job.MailFrom).
		Int("rcpt_count", len(allRcpt)).
		Msg("outbound job enqueued")

	return job, replayed, nil
}

func (s *Service) createOutboundJob(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation, draft *store.DraftConsumption) (bool, error) {
	if draft != nil {
		repo, ok := s.store.(interface {
			CreateOutboundJobConsumeDraft(context.Context, *models.OutboundJob, store.OutboundQuotaReservation, store.DraftConsumption) (bool, error)
		})
		if !ok {
			return false, app.BadRequest("draft submission unavailable")
		}
		return repo.CreateOutboundJobConsumeDraft(ctx, job, quota, *draft)
	}
	if quota.HasLimits() {
		return false, s.store.CreateOutboundJobWithQuota(ctx, job, quota)
	}
	return false, s.store.CreateOutboundJob(ctx, job)
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
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.worker != nil {
		return s.worker
	}
	policy := workqueue.ExponentialCappedBackoff[*outboundJob]{
		Base:        s.cfg.RetryDelay,
		Cap:         maxRetryDelay,
		MaxAttempts: func(j *workqueue.Job[*outboundJob]) int { return j.Payload.MaxAttempts },
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
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out := job.Payload.OutboundJob
	log := s.logger.With().Str("job_id", out.ID.String()).Logger()
	if err := s.ValidateJobAuthorization(ctx, out); err != nil {
		if authz.IsAuthzError(err) {
			return s.store.MarkOutboundJobFailed(ctx, out.ID, job.Lease.Token, "authorization revoked: "+err.Error(), false)
		}
		return err // transient database failures remain retryable, without sending
	}
	// Build MIME message from the structurally-stored recipients.
	mime, err := s.buildQueuedMIME(ctx, out)
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

	if out.RecipientLedger {
		return s.deliverRecipients(ctx, out, job.Lease.Token, mime)
	}
	return s.deliverDomains(ctx, out, job.Lease.Token, mime)
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
