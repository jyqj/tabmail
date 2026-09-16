package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

var (
	errOutboundJobAuthRequired = errors.New("authentication required")
	errOutboundJobNotFound     = errors.New("outbound job not found")
)

// OutboundHandler serves the outbound (send) API endpoints.
type OutboundHandler struct {
	outbound *outbound.Service
	store    store.Store
	az       *authz.Authorizer
	logger   zerolog.Logger
}

// NewOutboundHandler creates a new OutboundHandler.
func NewOutboundHandler(svc *outbound.Service, st store.Store, logger zerolog.Logger) *OutboundHandler {
	return &OutboundHandler{
		outbound: svc,
		store:    st,
		az:       authz.New(st),
		logger:   logger.With().Str("handler", "outbound").Logger(),
	}
}

// sendRequest is the JSON body for POST /api/v1/send.
type sendRequest struct {
	From              string            `json:"from"`
	To                []string          `json:"to"`
	CC                []string          `json:"cc"`
	BCC               []string          `json:"bcc"`
	Subject           string            `json:"subject"`
	TextBody          string            `json:"text_body"`
	HTMLBody          string            `json:"html_body"`
	Headers           map[string]string `json:"headers"`
	TemplateVersionID *uuid.UUID        `json:"template_version_id"`
	AttachmentIDs     []uuid.UUID       `json:"attachment_ids"`
	TemplateName      *string           `json:"template_name"`
	TemplateVars      map[string]string `json:"template_vars"`
}

// maxSendBodyBytes limits the JSON request body for outbound send to 2 MB.
const maxSendBodyBytes = 2 * 1024 * 1024

// Send handles POST /api/v1/send — submit an outbound email.
func (h *OutboundHandler) Send(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSendBodyBytes)
	var body sendRequest
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}

	if body.From == "" {
		errBadRequest(w, "from is required")
		return
	}
	if len(body.To) == 0 {
		errBadRequest(w, "at least one recipient in to is required")
		return
	}

	job, replayed, ok := h.submitAuthorized(w, r, outboundSubmitInput{
		From:              body.From,
		To:                body.To,
		CC:                body.CC,
		BCC:               body.BCC,
		Subject:           body.Subject,
		TextBody:          body.TextBody,
		HTMLBody:          body.HTMLBody,
		Headers:           body.Headers,
		TemplateVersionID: body.TemplateVersionID,
		TemplateName:      body.TemplateName,
		TemplateVars:      body.TemplateVars,
		AttachmentIDs:     body.AttachmentIDs,
		IdempotencyKey:    strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if !ok {
		return
	}
	// Legacy /send contract: always 201 (replays included). The draft submit
	// endpoint distinguishes 201 fresh vs 200 replay via the same helper.
	_ = replayed
	created(w, job)
}

// ConsumedDraftSubmission classifies an already-missing draft (see
// outbound.Service.ConsumedDraftSubmission) for the company draft submit
// endpoint.
func (h *OutboundHandler) ConsumedDraftSubmission(ctx context.Context, tenantID, draftID uuid.UUID, userID *uuid.UUID, key string) (*models.OutboundJob, bool, error) {
	return h.outbound.ConsumedDraftSubmission(ctx, tenantID, draftID, userID, key)
}

// outboundSubmitInput carries the caller-supplied message fields shared by the
// legacy /send endpoint and the company draft submit endpoint.
type outboundSubmitInput struct {
	From              string
	To                []string
	CC                []string
	BCC               []string
	Subject           string
	TextBody          string
	HTMLBody          string
	Headers           map[string]string
	TemplateVersionID *uuid.UUID
	TemplateName      *string
	TemplateVars      map[string]string
	AttachmentIDs     []uuid.UUID
	IdempotencyKey    string
	// Draft pins the mail draft this submission consumes atomically; nil for
	// the legacy /send path.
	Draft *store.DraftConsumption
}

// submitAuthorized runs the full send authorization chain (zone lookup,
// ActionSendFrom, verified/MX, DKIM policy, ResolveSendAuthorization, quota,
// suppression) and enqueues the job. It writes the HTTP error response itself
// and returns ok=false on failure. On success it returns the job and whether
// the response is an idempotent replay of an earlier submission.
func (h *OutboundHandler) submitAuthorized(w http.ResponseWriter, r *http.Request, in outboundSubmitInput) (*models.OutboundJob, bool, bool) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return nil, false, false
	}
	actor := middleware.ActorFromContext(ctx)

	// Resolve caller identity for job attribution and quota tracking.
	// actor.Permission is populated by middleware.PermissionLoader for JWT
	// users and by the auth middleware for API keys (owner permission or
	// zone-restricted synthetic permission).
	var apiKeyID *uuid.UUID
	if actor.Type == authz.PrincipalAPIKey {
		keyID := actor.ID
		apiKeyID = &keyID
	}
	userID := actor.EffectiveUserID()

	canonical, addressErr := authz.CanonicalSender(in.From)
	if addressErr != nil {
		errBadRequest(w, addressErr.Error())
		return nil, false, false
	}
	in.From = canonical
	// Validate the from address domain belongs to this tenant and is verified.
	fromDomain := extractDomainFromAddress(in.From)
	if fromDomain == "" {
		errBadRequest(w, "invalid from address")
		return nil, false, false
	}

	zone, err := h.store.GetZoneByDomain(ctx, fromDomain)
	if err != nil {
		h.logger.Err(err).Str("domain", fromDomain).Msg("looking up zone by domain")
		errInternal(w)
		return nil, false, false
	}
	if zone == nil {
		errBadRequest(w, "from domain is not registered")
		return nil, false, false
	}

	// Authorize sending from this zone through the authz seam: tenant
	// isolation, CanSend flag, and zone allowlist. OwnerUserID is
	// intentionally NOT set — sending must not require zone ownership,
	// so tenant users can send from shared zones.
	if err := h.az.Authorize(ctx, actor, authz.ActionSendFrom, authz.Resource{
		Type:     "zone",
		ID:       zone.ID,
		TenantID: zone.TenantID,
		ZoneID:   zone.ID,
	}); err != nil {
		if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
		} else {
			errInternal(w)
		}
		return nil, false, false
	}

	if !zone.IsVerified {
		errBadRequest(w, "from domain is not verified")
		return nil, false, false
	}

	if !zone.MXVerified {
		errBadRequest(w, "from domain MX is not verified")
		return nil, false, false
	}

	// Reject synchronously when the zone's DKIM policy cannot be satisfied,
	// rather than accepting a job that would only ever be driven to dead.
	if reason := h.outbound.DKIMSendBlockReason(zone); reason != "" {
		errBadRequest(w, reason)
		return nil, false, false
	}

	quota := store.OutboundQuotaReservation{}
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	// The From address must be a real mailbox in this tenant or a verified
	// send identity — the two authorized send-as paths. Inbound domain routes
	// govern ingress delivery, not outbound From authorization, so they no
	// longer gate sending. This makes the send_identities feature (manual
	// exact identities, the auto-created *@domain wildcard, and its Verified
	// flag) actually enforce send-as.
	//
	// The decision tree lives in outbound.ResolveSendAuthorization, the same
	// function ValidateJobAuthorization re-runs before every delivery attempt;
	// only the error precedence and wording below are HTTP-side.
	res, err := outbound.ResolveSendAuthorization(ctx, h.store, actor, tenant.ID, in.From, in.TemplateVersionID != nil)
	if err != nil {
		h.logger.Err(err).Str("from", in.From).Msg("resolving send authorization")
		errInternal(w)
		return nil, false, false
	}
	switch {
	case res.TenantWideBlocked:
		errForbidden(w, "company mailbox sends require an employee-owned credential")
		return nil, false, false
	case res.MailboxSenderErr != nil:
		if authz.IsAuthzError(res.MailboxSenderErr) {
			errForbidden(w, res.MailboxSenderErr.Error())
		} else {
			errInternal(w)
		}
		return nil, false, false
	case res.IdentityUnverified:
		errBadRequest(w, "from address is not an authorized mailbox or verified send identity")
		return nil, false, false
	case res.MailboxExpired:
		errForbidden(w, "sender mailbox expired")
		return nil, false, false
	}
	mailbox := res.Mailbox

	// Reserve user daily quota atomically with job creation.
	if actor.Permission != nil && actor.Permission.DailySendQuota > 0 {
		quota.UserDaily = &store.OutboundUserDailyQuota{
			UserID: userID,
			Since:  todayStart,
			Limit:  actor.Permission.DailySendQuota,
		}
	}

	// Check suppression list — block sending to suppressed addresses.
	for _, rcpt := range append(append(in.To, in.CC...), in.BCC...) {
		suppressed, err := h.store.IsSuppressed(ctx, tenant.ID, rcpt)
		if err != nil {
			h.logger.Err(err).Str("address", rcpt).Msg("checking suppression list")
			errInternal(w)
			return nil, false, false
		}
		if suppressed {
			errBadRequest(w, "recipient "+rcpt+" is suppressed (hard bounce); remove from suppression list to retry")
			return nil, false, false
		}
	}

	// Build and submit the outbound job.
	job, replayed, err := h.outbound.SubmitWithReplay(ctx, outbound.SendRequest{
		TenantID:          tenant.ID,
		SenderMailboxID:   mailboxID(mailbox),
		UserID:            userID,
		APIKeyID:          apiKeyID,
		ZoneID:            zone.ID,
		From:              in.From,
		To:                in.To,
		CC:                in.CC,
		BCC:               in.BCC,
		Subject:           in.Subject,
		TextBody:          in.TextBody,
		HTMLBody:          in.HTMLBody,
		Headers:           in.Headers,
		TemplateName:      in.TemplateName,
		TemplateVersionID: in.TemplateVersionID, AttachmentIDs: in.AttachmentIDs, IdempotencyKey: in.IdempotencyKey,
		TemplateVars: in.TemplateVars,
		Quota:        quota,
		Draft:        in.Draft,
	})
	if err != nil {
		if errors.Is(err, store.ErrSendAsDailyQuotaExceeded) {
			writeJSON(w, http.StatusTooManyRequests, envelope{
				Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "send-as daily quota exceeded"},
			})
			return nil, false, false
		}
		if errors.Is(err, store.ErrOutboundDailyQuotaExceeded) {
			writeJSON(w, http.StatusTooManyRequests, envelope{
				Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "daily send quota exceeded"},
			})
			return nil, false, false
		}
		if errors.Is(err, store.ErrDraftAlreadyConsumed) {
			errConflict(w, "draft was already submitted or consumed in another window; check send status instead of retrying")
			return nil, false, false
		}
		if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
			return nil, false, false
		}
		if _, ok := app.As(err); ok {
			respondAppError(w, h.logger, err)
			return nil, false, false
		}
		h.logger.Err(err).Msg("submitting outbound job")
		errBadRequest(w, err.Error())
		return nil, false, false
	}

	return job, replayed, true
}

// GetJob handles GET /api/v1/outbound/{id} — get a single outbound job.
func (h *OutboundHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid job id")
		return
	}

	ctx := r.Context()
	job, err := h.getAccessibleOutboundJob(ctx, jobID)
	if err != nil {
		h.writeOutboundJobAccessError(w, err, "getting outbound job")
		return
	}

	h.writeOutboundView(w, r, job)
}

// ListJobs handles GET /api/v1/outbound — list outbound jobs for the tenant.
// Non-admin users only see their own outbound jobs.
func (h *OutboundHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	pg := pageFromReq(r)

	items, total, err := h.listAccessibleOutboundJobs(ctx, tenant.ID, pg)
	if err != nil {
		h.writeOutboundJobAccessError(w, err, "listing outbound jobs")
		return
	}
	for i, job := range items {
		view, viewErr := h.redactOutboundJob(ctx, middleware.ActorFromContext(ctx), job)
		if viewErr != nil {
			errInternal(w)
			return
		}
		items[i] = view
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

// RetryJob handles POST /api/v1/outbound/{id}/retry — re-enqueue a dead/failed job.
func (h *OutboundHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid job id")
		return
	}
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	job, err := h.getAccessibleOutboundJob(ctx, jobID)
	if err != nil {
		h.writeOutboundJobAccessError(w, err, "getting outbound job for retry")
		return
	}
	if job.State != models.OutboundDead && job.State != models.OutboundFailed {
		errBadRequest(w, "only dead or failed jobs can be retried")
		return
	}
	if h.outbound == nil {
		errInternal(w)
		return
	}
	if err := h.authorizeOutboundRetry(ctx, job); err != nil {
		if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
		} else {
			errInternal(w)
		}
		return
	}
	if err := h.outbound.ValidateJobAuthorization(ctx, job); err != nil {
		if errors.Is(err, store.ErrOutboundUncertain) {
			errConflict(w, err.Error())
		} else if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
		} else {
			errInternal(w)
		}
		return
	}
	if err := h.store.RequeueOutboundJob(ctx, jobID); err != nil {
		if errors.Is(err, store.ErrOutboundNotRetryable) {
			errConflict(w, err.Error())
			return
		}
		h.logger.Err(err).Str("job_id", jobID.String()).Msg("requeue outbound job")
		errInternal(w)
		return
	}
	updatedJob, _ := h.store.GetOutboundJob(ctx, jobID)
	if updatedJob != nil {
		h.writeOutboundView(w, r, updatedJob)
	} else {
		ok(w, map[string]string{"status": "requeued"})
	}
}

// ListAttempts handles GET /api/v1/outbound/{id}/attempts — list delivery attempts for a job.
func (h *OutboundHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid job id")
		return
	}
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	job, err := h.getAccessibleOutboundJob(ctx, jobID)
	if err != nil {
		h.writeOutboundJobAccessError(w, err, "getting outbound job for attempts")
		return
	}
	attempts, err := h.store.ListOutboundAttempts(ctx, jobID)
	if err != nil {
		h.logger.Err(err).Msg("listing outbound attempts")
		errInternal(w)
		return
	}
	if attempts == nil {
		attempts = []*models.OutboundAttempt{}
	}
	allowed, err := h.outboundContentAllowed(ctx, middleware.ActorFromContext(ctx), job)
	if err != nil {
		errInternal(w)
		return
	}
	if !allowed {
		for i, a := range attempts {
			cp := *a
			if cp.Error != "" {
				cp.Error = "Delivery details restricted"
			}
			if cp.SMTPResponse != "" {
				cp.SMTPResponse = "Protocol response restricted"
			}
			attempts[i] = &cp
		}
	}
	ok(w, attempts)
}

// ListSuppressions handles GET /api/v1/suppression — list suppressed addresses.
func (h *OutboundHandler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	pg := pageFromReq(r)
	items, total, err := h.store.ListSuppressions(ctx, tenant.ID, pg)
	if err != nil {
		h.logger.Err(err).Msg("listing suppressions")
		errInternal(w)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

// DeleteSuppression handles DELETE /api/v1/suppression/{id} — remove a suppressed address.
func (h *OutboundHandler) DeleteSuppression(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.store.DeleteSuppression(ctx, tenant.ID, id); err != nil {
		h.logger.Err(err).Msg("deleting suppression")
		errInternal(w)
		return
	}
	noContent(w)
}

func (h *OutboundHandler) getAccessibleOutboundJob(ctx context.Context, jobID uuid.UUID) (*models.OutboundJob, error) {
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		return nil, errOutboundJobAuthRequired
	}

	job, err := h.store.GetOutboundJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job != nil && !middleware.ActorFromContext(ctx).Permission.AllowsZone(job.ZoneID) {
		return nil, errOutboundJobNotFound
	}
	if !canAccessOutboundJob(ctx, tenant.ID, job) {
		allowed, err := h.outboundContentAllowed(ctx, middleware.ActorFromContext(ctx), job)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, errOutboundJobNotFound
		}
	}
	return job, nil
}

func (h *OutboundHandler) listAccessibleOutboundJobs(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	// ActionOutboundRead defers row scope to the query level; the authz seam
	// resolves which owned rows this actor may see. The OwnerListFilter pins
	// TenantID and the mutually-exclusive owner dimension, so tenant isolation
	// and the owner rule are both enforced in SQL.
	scope := authz.OwnerListScope(middleware.ActorFromContext(ctx), tenantID)
	actor := middleware.ActorFromContext(ctx)
	scope.ReaderUserID = actor.EffectiveUserID()
	if actor.Permission != nil {
		scope.AllowedZoneIDs = actor.Permission.AllowedZoneIDs
	}
	return h.store.ListOutboundJobsScoped(ctx, scope, pg)
}

func canAccessOutboundJob(ctx context.Context, tenantID uuid.UUID, job *models.OutboundJob) bool {
	if job == nil || job.TenantID != tenantID {
		return false
	}
	// Same owner rule as listAccessibleOutboundJobs, via the single authz seam.
	return authz.CanAccessOwned(middleware.ActorFromContext(ctx), job.UserID, job.APIKeyID)
}

func (h *OutboundHandler) writeOutboundJobAccessError(w http.ResponseWriter, err error, logMsg string) {
	switch {
	case errors.Is(err, errOutboundJobAuthRequired):
		errForbidden(w, "authentication required")
	case errors.Is(err, errOutboundJobNotFound):
		errNotFound(w, "outbound job not found")
	default:
		h.logger.Err(err).Msg(logMsg)
		errInternal(w)
	}
}

// extractDomainFromAddress extracts the domain part from an email address.
func extractDomainFromAddress(addr string) string {
	idx := strings.LastIndex(addr, "@")
	if idx < 0 || idx == len(addr)-1 {
		return ""
	}
	return addr[idx+1:]
}

func mailboxID(m *models.Mailbox) *uuid.UUID {
	if m == nil {
		return nil
	}
	id := m.ID
	return &id
}
