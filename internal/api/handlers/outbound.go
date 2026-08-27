package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
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

	fromAddress, err := normalizeEnvelopeAddress(body.From)
	if err != nil {
		errBadRequest(w, "invalid from address")
		return
	}
	to, err := normalizeEnvelopeAddresses(body.To)
	if err != nil || len(to) == 0 {
		errBadRequest(w, "at least one valid recipient in to is required")
		return
	}
	cc, err := normalizeEnvelopeAddresses(body.CC)
	if err != nil {
		errBadRequest(w, "invalid cc address")
		return
	}
	bcc, err := normalizeEnvelopeAddresses(body.BCC)
	if err != nil {
		errBadRequest(w, "invalid bcc address")
		return
	}
	to, cc, bcc = dedupeRecipientGroups(to, cc, bcc)

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}
	actor := middleware.ActorFromContext(ctx)

	// Resolve caller identity for job attribution and quota tracking.
	var apiKeyID *uuid.UUID
	if actor.Type == authz.PrincipalAPIKey {
		keyID := actor.ID
		apiKeyID = &keyID
	}
	userID := actor.EffectiveUserID()

	// Validate the from address domain belongs to this tenant and is verified.
	fromDomain := extractDomainFromAddress(fromAddress)
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
	// isolation, CanSend flag, and zone allowlist. Sending from a shared zone
	// does not require zone ownership, but it does require a verified send
	// identity below.
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

	// SendIdentity is the authoritative send-as asset. A mailbox or inbound
	// route no longer implicitly grants outbound authority; administrators
	// explicitly create exact or domain-wildcard identities, and domain
	// verification keeps their verified state in sync.
	identity, err := h.store.FindSendIdentityForAddress(ctx, tenant.ID, fromAddress)
	if err != nil {
		h.logger.Err(err).Str("from", fromAddress).Msg("looking up send identity")
		errInternal(w)
		return
	}
	if identity == nil || identity.TenantID != tenant.ID || identity.ZoneID != zone.ID {
		errForbidden(w, "from address is not an authorized send identity")
		return
	}
	if !identity.Verified {
		errBadRequest(w, "send identity is not verified")
		return
	}

	quota := store.OutboundQuotaReservation{}
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	if actor.Permission != nil && actor.Permission.DailySendQuota > 0 {
		quota.UserDaily = &store.OutboundUserDailyQuota{
			UserID: userID,
			Since:  todayStart,
			Limit:  actor.Permission.DailySendQuota,
		}
	}

	// Check suppression list against canonical envelope recipients.
	for _, rcpt := range append(append(append([]string{}, to...), cc...), bcc...) {
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

	if h.outbound == nil {
		errInternal(w)
		return
	}
	job, err := h.outbound.Submit(ctx, outbound.SendRequest{
		TenantID: tenant.ID,
		UserID:   userID,
		APIKeyID: apiKeyID,
		ZoneID:   zone.ID,
		From:     fromAddress,
		To:       to,
		CC:       cc,
		BCC:      bcc,
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
	actor := middleware.ActorFromContext(ctx)
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

	actor := middleware.ActorFromContext(ctx)
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

func normalizeEnvelopeAddresses(items []string) ([]string, error) {
	if len(items) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		address, err := normalizeEnvelopeAddress(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	return out, nil
}

func dedupeRecipientGroups(to, cc, bcc []string) ([]string, []string, []string) {
	seen := make(map[string]struct{}, len(to)+len(cc)+len(bcc))
	filter := func(items []string) []string {
		out := make([]string, 0, len(items))
		for _, address := range items {
			if _, exists := seen[address]; exists {
				continue
			}
			seen[address] = struct{}{}
			out = append(out, address)
		}
		return out
	}
	return filter(to), filter(cc), filter(bcc)
}

func normalizeEnvelopeAddress(raw string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(parsed.Address) == "" {
		return "", errors.New("invalid email address")
	}
	address := strings.ToLower(strings.TrimSpace(parsed.Address))
	if extractDomainFromAddress(address) == "" {
		return "", errors.New("invalid email address")
	}
	return address, nil
}

// extractDomainFromAddress extracts and normalizes the domain part from an
// already-canonical envelope address.
func extractDomainFromAddress(addr string) string {
	idx := strings.LastIndex(addr, "@")
	if idx < 0 || idx == len(addr)-1 {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(addr[idx+1:])), ".")
}
