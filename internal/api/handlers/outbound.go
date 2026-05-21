package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

// OutboundHandler serves the outbound (send) API endpoints.
type OutboundHandler struct {
	outbound *outbound.Service
	store    store.Store
	logger   zerolog.Logger
}

// NewOutboundHandler creates a new OutboundHandler.
func NewOutboundHandler(svc *outbound.Service, st store.Store, logger zerolog.Logger) *OutboundHandler {
	return &OutboundHandler{
		outbound: svc,
		store:    st,
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

// Send handles POST /api/v1/send — submit an outbound email.
func (h *OutboundHandler) Send(w http.ResponseWriter, r *http.Request) {
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

	// Resolve caller identity.
	var userID *uuid.UUID
	var apiKeyID *uuid.UUID
	user := middleware.UserFromCtx(ctx)
	if user != nil {
		id := user.ID
		userID = &id
	}

	// Check user-level permission if the caller is a JWT user.
	var perm *models.EffectivePermission
	if user != nil {
		ep, err := h.store.EffectivePermission(ctx, user.ID)
		if err != nil {
			h.logger.Err(err).Msg("loading effective permission")
			errInternal(w)
			return
		}
		if ep != nil {
			perm = ep
			if !perm.CanSend {
				errForbidden(w, "sending is not allowed for your account")
				return
			}
		}
	}

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
	if zone.TenantID != tenant.ID {
		errForbidden(w, "from domain does not belong to your tenant")
		return
	}
	if !zone.IsVerified {
		errBadRequest(w, "from domain is not verified")
		return
	}

	// If the user has a permission profile, check zone allowlist.
	if perm != nil && len(perm.AllowedZoneIDs) > 0 {
		if !isZoneAllowed(perm, zone.ID) {
			errForbidden(w, "you are not allowed to send from this domain")
			return
		}
	}

	// Quota check: if the user has a daily send quota, enforce it.
	if perm != nil && perm.DailySendQuota > 0 {
		todayStart := time.Now().UTC().Truncate(24 * time.Hour)
		count, err := h.store.CountOutboundSince(ctx, tenant.ID, userID, todayStart)
		if err != nil {
			h.logger.Err(err).Msg("counting outbound jobs for quota")
			errInternal(w)
			return
		}
		if count >= perm.DailySendQuota {
			writeJSON(w, http.StatusTooManyRequests, envelope{
				Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "daily send quota exceeded"},
			})
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
	})
	if err != nil {
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
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	job, err := h.store.GetOutboundJob(ctx, jobID)
	if err != nil {
		h.logger.Err(err).Msg("getting outbound job")
		errInternal(w)
		return
	}
	if job == nil {
		errNotFound(w, "outbound job not found")
		return
	}
	if job.TenantID != tenant.ID {
		errNotFound(w, "outbound job not found")
		return
	}

	ok(w, job)
}

// ListJobs handles GET /api/v1/outbound — list outbound jobs for the tenant.
func (h *OutboundHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	pg := pageFromReq(r)
	items, total, err := h.store.ListOutboundJobs(ctx, tenant.ID, pg)
	if err != nil {
		h.logger.Err(err).Msg("listing outbound jobs")
		errInternal(w)
		return
	}

	okList(w, items, total, pg.Page, pg.PerPage)
}

// extractDomainFromAddress extracts the domain part from an email address.
func extractDomainFromAddress(addr string) string {
	idx := strings.LastIndex(addr, "@")
	if idx < 0 || idx == len(addr)-1 {
		return ""
	}
	return addr[idx+1:]
}

// isZoneAllowed checks whether the given zone ID is in the permission's allowlist.
func isZoneAllowed(perm *models.EffectivePermission, zoneID uuid.UUID) bool {
	for _, id := range perm.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}
