package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/credentials"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

// OutboundHandler serves the outbound (send) API endpoints. The submission
// orchestration and job accessibility/redaction rules live in the
// submissions use-case service; the handler only parses requests and maps
// outcomes to HTTP responses.
type OutboundHandler struct {
	outbound *outbound.Service
	store    store.Store
	subs     *submissions.Service
	logger   zerolog.Logger
}

// NewOutboundHandler creates a new OutboundHandler.
func NewOutboundHandler(svc *outbound.Service, st store.Store, logger zerolog.Logger) *OutboundHandler {
	return &OutboundHandler{
		outbound: svc,
		store:    st,
		subs:     submissions.NewService(nil, st, svc, logger),
		logger:   logger.With().Str("handler", "outbound").Logger(),
	}
}

// GetJob handles GET /api/v1/outbound/{id} — get a single outbound job.
func (h *OutboundHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid job id")
		return
	}

	ctx := r.Context()
	job, err := h.subs.AccessibleOutboundJob(ctx, middleware.TenantFromCtx(ctx), middleware.ActorFromContext(ctx), jobID)
	if err != nil {
		writeOutboundJobAccessError(w, h.logger, err, "getting outbound job")
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
		writeOutboundJobAccessError(w, h.logger, err, "listing outbound jobs")
		return
	}
	for i, job := range items {
		view, viewErr := h.subs.RedactOutboundJob(ctx, middleware.ActorFromContext(ctx), job)
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
	job, err := h.subs.AccessibleOutboundJob(ctx, middleware.TenantFromCtx(ctx), middleware.ActorFromContext(ctx), jobID)
	if err != nil {
		writeOutboundJobAccessError(w, h.logger, err, "getting outbound job for retry")
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
			errConflictReason(w, err.Error(), "delivery_uncertain")
		} else if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
		} else {
			errInternal(w)
		}
		return
	}
	if err := h.store.RequeueOutboundJob(ctx, jobID); err != nil {
		if errors.Is(err, store.ErrOutboundNotRetryable) {
			errConflictReason(w, err.Error(), "state_changed")
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
	job, err := h.subs.AccessibleOutboundJob(ctx, middleware.TenantFromCtx(ctx), middleware.ActorFromContext(ctx), jobID)
	if err != nil {
		writeOutboundJobAccessError(w, h.logger, err, "getting outbound job for attempts")
		return
	}
	attempts, err := h.store.ListOutboundAttempts(ctx, jobID)
	if err != nil {
		h.logger.Err(err).Msg("listing outbound attempts")
		errInternal(w)
		return
	}
	// The restricted-view decision and copy rules live in the submissions
	// service; the handler only maps the result to the response envelope.
	attempts, err = h.subs.RedactOutboundAttempts(ctx, middleware.ActorFromContext(ctx), job, attempts)
	if err != nil {
		errInternal(w)
		return
	}
	if attempts == nil {
		attempts = []*models.OutboundAttempt{}
	}
	ok(w, attempts)
}

// requireSuppressionAdmin is the authoritative boundary for the suppression
// management surface. RequireScopes passes JWT modes straight through, so an
// ordinary employee JWT would reach these handlers unchecked; here we inspect
// the resolved actor and demand tenant-admin authority from interactive users.
// API keys are deliberately skipped — the scope middleware is their gate.
// Modeled on the company_domains.go guard.
func (h *OutboundHandler) requireSuppressionAdmin(w http.ResponseWriter, r *http.Request) bool {
	actor := middleware.ActorFromContext(r.Context())
	if actor.Type != authz.PrincipalUser || actor.IsTenantAdmin() {
		return true
	}
	errForbidden(w, "tenant administrator required")
	return false
}

// ListSuppressions handles GET /api/v1/suppression — list suppressed addresses.
func (h *OutboundHandler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	if !h.requireSuppressionAdmin(w, r) {
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

// DeleteSuppression handles DELETE /api/v1/suppression/{id} — remove a
// suppressed address. The removal is destructive for future deliveries, so it
// demands a normalized 8-1000 byte reason and lands with the audit row in one
// transaction; a failed audit fails the whole request.
func (h *OutboundHandler) DeleteSuppression(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	if !h.requireSuppressionAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "reason is required")
		return
	}
	reason, reasonErr := credentials.AuditReason(body.Reason)
	if reasonErr != nil {
		errBadRequest(w, reasonErr.Error())
		return
	}
	actor := middleware.ActorFromContext(ctx)
	entry := models.AuditEntry{
		TenantID:     &tenant.ID,
		Actor:        actor.AuditLabel(),
		Action:       "suppression.delete",
		ResourceType: "suppression",
		ResourceID:   &id,
		Details: app.MustJSON(map[string]any{
			"suppression_id": id.String(),
			"reason":         reason,
		}),
	}
	if err := h.store.DeleteSuppressionAudited(ctx, tenant.ID, id, entry); err != nil {
		h.logger.Err(err).Msg("deleting suppression")
		errInternal(w)
		return
	}
	noContent(w)
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

// writeOutboundJobAccessError maps the submissions service's job-visibility
// sentinels to app errors and replays them through respondAppError. The
// auth-required 403 and the existence-hiding 404 distinction is intentional
// and must survive any refactor.
func writeOutboundJobAccessError(w http.ResponseWriter, logger zerolog.Logger, err error, logMsg string) {
	var appErr error
	switch {
	case errors.Is(err, submissions.ErrOutboundJobAuthRequired):
		appErr = app.Forbidden("authentication required")
	case errors.Is(err, submissions.ErrOutboundJobNotFound):
		appErr = app.NotFound("outbound job not found")
	default:
		logger.Err(err).Msg(logMsg)
		appErr = app.Internal(err)
	}
	respondAppError(w, logger, appErr)
}
