package handlers

import (
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

	// Resolve caller identity.
	var userID *uuid.UUID
	apiKeyID := middleware.APIKeyIDFromCtx(ctx)
	user := middleware.UserFromCtx(ctx)
	if user != nil {
		id := user.ID
		userID = &id
	} else if ownerID := middleware.OwnerUserIDFromCtx(ctx); ownerID != nil {
		// API key with an active owner: use the owner's user ID for quota tracking.
		userID = ownerID
	}

	// Check user-level permission. For JWT users, load from store.
	// For API key callers, use permission from context (loaded by auth middleware
	// from the API key owner, if present).
	var perm *models.EffectivePermission
	if user != nil && apiKeyID == nil {
		ep, err := h.store.EffectivePermission(ctx, user.ID)
		if err != nil {
			h.logger.Err(err).Msg("loading effective permission")
			errInternal(w)
			return
		}
		if ep != nil {
			perm = ep
		}
	} else {
		// For API key callers, use permission from context (set by auth middleware).
		perm = middleware.PermissionFromCtx(ctx)
	}
	if perm != nil && !perm.CanSend {
		errForbidden(w, "sending is not allowed for your account")
		return
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

	if !zone.MXVerified {
		errBadRequest(w, "from domain MX is not verified")
		return
	}

	// If the user/API key has a zone allowlist, enforce it.
	if actor.Permission != nil && len(actor.Permission.AllowedZoneIDs) > 0 && !isZoneAllowed(actor.Permission, zone.ID) {
		errForbidden(w, "you are not allowed to send from this domain")
		return
	}

	// Send-as validation: check send_identity + send_as_grant.
	// Platform admin / tenant admin bypass send-as checks.
	if !actor.IsPlatformAdmin && !actor.IsTenantAdmin {
		principalType := string(actor.Type)
		principalID := actor.ID
		// actor.Type is already PrincipalAPIKey for API keys (actor.ID == key UUID).
		// No need to override principalID — it is correctly set by ActorFromContext.
		if principalType != "" {
			grant, err := h.store.GetSendAsGrant(ctx, tenant.ID, body.From, principalType, principalID)
			if err != nil {
				h.logger.Err(err).Str("from", body.From).Msg("checking send-as grant")
				errInternal(w)
				return
			}
			// Fallback: if no grant for the API key itself, check the owner user's grant.
			if grant == nil && actor.OwnerUserID != nil {
				grant, err = h.store.GetSendAsGrant(ctx, tenant.ID, body.From, string(authz.PrincipalUser), *actor.OwnerUserID)
				if err != nil {
					h.logger.Err(err).Str("from", body.From).Msg("checking owner send-as grant")
					errInternal(w)
					return
				}
			}
			if grant == nil {
				errForbidden(w, "you are not authorized to send from this address")
				return
			}
			// Enforce send-as daily quota if configured on the grant.
			if grant.DailyQuota > 0 {
				todayStart := time.Now().UTC().Truncate(24 * time.Hour)
				identityCount, err := h.store.CountOutboundByIdentitySince(ctx, tenant.ID, grant.PrincipalType, grant.PrincipalID, grant.IdentityID, todayStart)
				if err != nil {
					h.logger.Err(err).Msg("counting outbound jobs for send-as quota")
					errInternal(w)
					return
				}
				if identityCount >= grant.DailyQuota {
					writeJSON(w, http.StatusTooManyRequests, envelope{
						Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: "send-as daily quota exceeded"},
					})
					return
				}
			}
		}
	}

	// Fallback: verify the From address corresponds to an existing mailbox or
	// a matching route in the zone.
	mailbox, err := h.store.GetMailboxByAddressForTenant(ctx, body.From, tenant.ID)
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
	actor := authz.ActorFromContext(ctx)
	if !actor.IsPlatformAdmin && !actor.IsTenantAdmin {
		switch actor.Type {
		case authz.PrincipalUser:
			if job.UserID == nil || *job.UserID != actor.ID {
				errNotFound(w, "outbound job not found")
				return
			}
		case authz.PrincipalAPIKey:
			if job.APIKeyID == nil || *job.APIKeyID != actor.ID {
				errNotFound(w, "outbound job not found")
				return
			}
		default:
			errForbidden(w, "authentication required")
			return
		}
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

	// Non-admin users only see their own outbound jobs
	actor := authz.ActorFromContext(ctx)
	if actor.Type == authz.PrincipalUser && !actor.IsPlatformAdmin && !actor.IsTenantAdmin {
		items, total, err := h.store.ListOutboundJobsByUser(ctx, tenant.ID, actor.ID, pg)
		if err != nil {
			h.logger.Err(err).Msg("listing outbound jobs by user")
			errInternal(w)
			return
		}
		okList(w, items, total, pg.Page, pg.PerPage)
		return
	}
	if actor.Type == authz.PrincipalAPIKey && !actor.IsPlatformAdmin && !actor.IsTenantAdmin {
		items, total, err := h.store.ListOutboundJobsByAPIKey(ctx, tenant.ID, actor.ID, pg)
		if err != nil {
			h.logger.Err(err).Msg("listing outbound jobs by api key")
			errInternal(w)
			return
		}
		okList(w, items, total, pg.Page, pg.PerPage)
		return
	}

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
