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
	From     string            `json:"from"`
	To       []string          `json:"to"`
	CC       []string          `json:"cc"`
	BCC      []string          `json:"bcc"`
	Subject  string            `json:"subject"`
	TextBody string            `json:"text_body"`
	HTMLBody string            `json:"html_body"`
	Headers  map[string]string `json:"headers"`
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

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	actor := authz.ActorFromContext(ctx)

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

	// Validate the from address domain belongs to this tenant and is verified.
	fromDomain := extractDomainFromAddress(body.From)
	if fromDomain == "" {
		errBadRequest(w, "invalid from address")
		return
	}

	zone, err := h.store.GetZoneByDomain(ctx, fromDomain)
	if err != nil {
		h.logger.Err(err).Str("domain", fromDomain).Msg("looking up zone by domain")
		errInternal(w)
		return
	}
	if zone == nil {
		errBadRequest(w, "from domain is not registered")
		return
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
		return
	}

	if !zone.IsVerified {
		errBadRequest(w, "from domain is not verified")
		return
	}

	if !zone.MXVerified {
		errBadRequest(w, "from domain MX is not verified")
		return
	}

	quota := store.OutboundQuotaReservation{}
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	// Verify the From address corresponds to an existing mailbox or
	// a matching route in the zone.
	mailbox, err := h.store.ForTenant(tenant.ID).GetMailboxByAddress(ctx, body.From)
	if err != nil {
		h.logger.Err(err).Str("from", body.From).Msg("looking up mailbox by address")
		errInternal(w)
		return
	}
	if mailbox == nil {
		routes, err := h.store.FindMatchingRoutes(ctx, fromDomain, &tenant.ID)
		if err != nil {
			h.logger.Err(err).Str("domain", fromDomain).Msg("finding matching routes for send-as")
			errInternal(w)
			return
		}
		if len(routes) == 0 {
			errBadRequest(w, "from address does not exist as a mailbox")
			return
		}
	}

	// Reserve user daily quota atomically with job creation.
	if actor.Permission != nil && actor.Permission.DailySendQuota > 0 {
		quota.UserDaily = &store.OutboundUserDailyQuota{
			UserID: userID,
			Since:  todayStart,
			Limit:  actor.Permission.DailySendQuota,
		}
	}

	// Check suppression list — block sending to suppressed addresses.
	for _, rcpt := range append(append(body.To, body.CC...), body.BCC...) {
		suppressed, err := h.store.IsSuppressed(ctx, tenant.ID, rcpt)
		if err != nil {
			h.logger.Err(err).Str("address", rcpt).Msg("checking suppression list")
			errInternal(w)
			return
		}
		if suppressed {
			errBadRequest(w, "recipient "+rcpt+" is suppressed (hard bounce); remove from suppression list to retry")
			return
		}
	}

	// Build and submit the outbound job.
	job, err := h.outbound.Submit(ctx, outbound.SendRequest{
		TenantID: tenant.ID,
		UserID:   userID,
		APIKeyID: apiKeyID,
		ZoneID:   zone.ID,
		From:     body.From,
		To:       body.To,
		CC:       body.CC,
		BCC:      body.BCC,
		Subject:  body.Subject,
		TextBody: body.TextBody,
		HTMLBody: body.HTMLBody,
		Headers:  body.Headers,
		Quota:    quota,
	})
	if err != nil {
		if errors.Is(err, store.ErrSendAsDailyQuotaExceeded) {
			writeJSON(w, http.StatusTooManyRequests, envelope{
				Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "send-as daily quota exceeded"},
			})
			return
		}
		if errors.Is(err, store.ErrOutboundDailyQuotaExceeded) {
			writeJSON(w, http.StatusTooManyRequests, envelope{
				Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "daily send quota exceeded"},
			})
			return
		}
		h.logger.Err(err).Msg("submitting outbound job")
		errBadRequest(w, err.Error())
		return
	}

	created(w, job)
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

	ok(w, job)
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
	if err := h.store.RequeueOutboundJob(ctx, jobID); err != nil {
		h.logger.Err(err).Str("job_id", jobID.String()).Msg("requeue outbound job")
		errInternal(w)
		return
	}
	updatedJob, _ := h.store.GetOutboundJob(ctx, jobID)
	if updatedJob != nil {
		ok(w, updatedJob)
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
	if _, err := h.getAccessibleOutboundJob(ctx, jobID); err != nil {
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
	if !canAccessOutboundJob(ctx, tenant.ID, job) {
		return nil, errOutboundJobNotFound
	}
	return job, nil
}

func (h *OutboundHandler) listAccessibleOutboundJobs(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	actor := authz.ActorFromContext(ctx)
	if actor.IsSuperAdmin || actor.IsAdmin {
		return h.store.ListOutboundJobs(ctx, tenantID, pg)
	}

	switch actor.Type {
	case authz.PrincipalUser:
		return h.store.ListOutboundJobsByUser(ctx, tenantID, actor.ID, pg)
	case authz.PrincipalAPIKey:
		return h.store.ListOutboundJobsByAPIKey(ctx, tenantID, actor.ID, pg)
	default:
		return nil, 0, errOutboundJobAuthRequired
	}
}

func canAccessOutboundJob(ctx context.Context, tenantID uuid.UUID, job *models.OutboundJob) bool {
	if job == nil || job.TenantID != tenantID {
		return false
	}

	actor := authz.ActorFromContext(ctx)
	if actor.IsSuperAdmin || actor.IsAdmin {
		return true
	}

	switch actor.Type {
	case authz.PrincipalUser:
		return job.UserID != nil && *job.UserID == actor.ID
	case authz.PrincipalAPIKey:
		return job.APIKeyID != nil && *job.APIKeyID == actor.ID
	default:
		return false
	}
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
