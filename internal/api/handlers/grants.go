package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type GrantHandler struct {
	store  store.Store
	logger zerolog.Logger
}

func NewGrantHandler(st store.Store, l zerolog.Logger) *GrantHandler {
	return &GrantHandler{
		store:  st,
		logger: l.With().Str("handler", "grants").Logger(),
	}
}

// ================================================================
// Authorization helpers
// ================================================================

// canManageMailbox checks whether the caller can manage grants on a mailbox.
// Platform admin and tenant admin are always allowed.
// Regular users need mailbox-level owner/manager grant or zone-level owner/admin grant.
func (h *GrantHandler) canManageMailbox(r *http.Request, mailbox *models.Mailbox) bool {
	ctx := r.Context()
	actor := authz.ActorFromContext(ctx)
	if actor.IsPlatformAdmin || actor.IsTenantAdmin {
		return true
	}
	if !actorCanUseZone(actor, mailbox.ZoneID) {
		return false
	}
	if actor.TenantWide {
		return true
	}
	if actor.Type == "" || actor.ID == uuid.Nil {
		return false
	}
	// Check mailbox-level grant
	grant, err := h.store.GetMailboxGrant(ctx, mailbox.ID, string(actor.Type), actor.ID)
	if err == nil && grant != nil && grant.Role.CanManage() {
		return true
	}
	// Fall back to zone-level grant
	role, err := h.store.GetHighestZoneRole(ctx, mailbox.ZoneID, string(actor.Type), actor.ID)
	if err == nil && role.CanManage() {
		return true
	}
	if actor.OwnerUserID != nil {
		grant, err = h.store.GetMailboxGrant(ctx, mailbox.ID, "user", *actor.OwnerUserID)
		if err == nil && grant != nil && grant.Role.CanManage() {
			return true
		}
		role, err = h.store.GetHighestZoneRole(ctx, mailbox.ZoneID, "user", *actor.OwnerUserID)
		if err == nil && role.CanManage() {
			return true
		}
	}
	return false
}

// canManageZone checks whether the caller can manage grants on a zone.
// Platform admin and tenant admin are always allowed.
// Regular users need zone-level owner/admin grant.
func (h *GrantHandler) canManageZone(r *http.Request, zoneID uuid.UUID) bool {
	ctx := r.Context()
	actor := authz.ActorFromContext(ctx)
	if actor.IsPlatformAdmin || actor.IsTenantAdmin {
		return true
	}
	if !actorCanUseZone(actor, zoneID) {
		return false
	}
	if actor.TenantWide {
		return true
	}
	if actor.Type == "" || actor.ID == uuid.Nil {
		return false
	}
	role, err := h.store.GetHighestZoneRole(ctx, zoneID, string(actor.Type), actor.ID)
	if err == nil && role.CanManage() {
		return true
	}
	if actor.OwnerUserID != nil {
		role, err = h.store.GetHighestZoneRole(ctx, zoneID, "user", *actor.OwnerUserID)
		if err == nil && role.CanManage() {
			return true
		}
	}
	return false
}

// canManageSendIdentity checks whether the caller can manage send-as grants on an identity.
// Platform admin and tenant admin are always allowed.
// Regular users need zone-level owner/admin grant on the identity's zone.
func (h *GrantHandler) canManageSendIdentity(r *http.Request, si *models.SendIdentity) bool {
	ctx := r.Context()
	actor := authz.ActorFromContext(ctx)
	if actor.IsPlatformAdmin || actor.IsTenantAdmin {
		return true
	}
	if !actorCanUseZone(actor, si.ZoneID) {
		return false
	}
	if actor.TenantWide {
		return true
	}
	if actor.Type == "" || actor.ID == uuid.Nil {
		return false
	}
	role, err := h.store.GetHighestZoneRole(ctx, si.ZoneID, string(actor.Type), actor.ID)
	if err == nil && role.CanManage() {
		return true
	}
	if actor.OwnerUserID != nil {
		role, err = h.store.GetHighestZoneRole(ctx, si.ZoneID, "user", *actor.OwnerUserID)
		if err == nil && role.CanManage() {
			return true
		}
	}
	return false
}

func actorCanUseZone(actor authz.Actor, zoneID uuid.UUID) bool {
	if actor.Permission == nil || len(actor.Permission.AllowedZoneIDs) == 0 {
		return true
	}
	for _, id := range actor.Permission.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}

// ================================================================
// Mailbox Grants
// ================================================================

func (h *GrantHandler) ListMailboxGrants(w http.ResponseWriter, r *http.Request) {
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxId"))
	if err != nil {
		errBadRequest(w, "invalid mailbox id")
		return
	}

	ctx := r.Context()
	mailbox, err := h.store.GetMailbox(ctx, mailboxID)
	if err != nil {
		h.logger.Err(err).Msg("getting mailbox")
		errInternal(w)
		return
	}
	if mailbox == nil {
		errNotFound(w, "mailbox not found")
		return
	}

	if err := h.requireTenantAccess(r, mailbox.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}
	if !h.canManageMailbox(r, mailbox) {
		errForbidden(w, "insufficient permissions to manage this mailbox")
		return
	}

	grants, err := h.store.ListMailboxGrants(ctx, mailboxID)
	if err != nil {
		h.logger.Err(err).Msg("listing mailbox grants")
		errInternal(w)
		return
	}
	ok(w, grants)
}

func (h *GrantHandler) CreateMailboxGrant(w http.ResponseWriter, r *http.Request) {
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxId"))
	if err != nil {
		errBadRequest(w, "invalid mailbox id")
		return
	}

	var body struct {
		PrincipalType string                  `json:"principal_type"`
		PrincipalID   uuid.UUID               `json:"principal_id"`
		Role          models.MailboxGrantRole `json:"role"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	if body.PrincipalType != "user" && body.PrincipalType != "api_key" {
		errBadRequest(w, "principal_type must be 'user' or 'api_key'")
		return
	}
	if body.PrincipalID == (uuid.UUID{}) {
		errBadRequest(w, "principal_id is required")
		return
	}
	if !body.Role.Valid() {
		errBadRequest(w, "role must be one of: owner, manager, writer, reader")
		return
	}

	ctx := r.Context()
	mailbox, err := h.store.GetMailbox(ctx, mailboxID)
	if err != nil {
		h.logger.Err(err).Msg("getting mailbox")
		errInternal(w)
		return
	}
	if mailbox == nil {
		errNotFound(w, "mailbox not found")
		return
	}

	if err := h.requireTenantAccess(r, mailbox.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}

	// Authorization: caller must be able to manage this mailbox
	if !h.canManageMailbox(r, mailbox) {
		errForbidden(w, "insufficient permissions to manage this mailbox")
		return
	}

	if body.PrincipalType == "user" {
		user, err := h.store.GetUser(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal user")
			errInternal(w)
			return
		}
		if user == nil || user.TenantID != mailbox.TenantID {
			errBadRequest(w, "principal user not found in same tenant")
			return
		}
	} else {
		key, err := h.store.GetAPIKey(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal api key")
			errInternal(w)
			return
		}
		if key == nil || key.TenantID != mailbox.TenantID {
			errBadRequest(w, "principal api key not found in same tenant")
			return
		}
	}

	grant := &models.MailboxGrant{
		ID:            uuid.New(),
		TenantID:      mailbox.TenantID,
		MailboxID:     mailboxID,
		PrincipalType: body.PrincipalType,
		PrincipalID:   body.PrincipalID,
		Role:          body.Role,
	}

	if err := h.store.CreateMailboxGrant(ctx, grant); err != nil {
		h.logger.Err(err).Msg("creating mailbox grant")
		errInternal(w)
		return
	}
	created(w, grant)
}

func (h *GrantHandler) DeleteMailboxGrant(w http.ResponseWriter, r *http.Request) {
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxId"))
	if err != nil {
		errBadRequest(w, "invalid mailbox id")
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid grant id")
		return
	}

	ctx := r.Context()
	mailbox, err := h.store.GetMailbox(ctx, mailboxID)
	if err != nil {
		h.logger.Err(err).Msg("getting mailbox")
		errInternal(w)
		return
	}
	if mailbox == nil {
		errNotFound(w, "mailbox not found")
		return
	}

	if err := h.requireTenantAccess(r, mailbox.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}

	// Authorization: caller must be able to manage this mailbox
	if !h.canManageMailbox(r, mailbox) {
		errForbidden(w, "insufficient permissions to manage this mailbox")
		return
	}

	// Use scoped delete to ensure the grant belongs to this mailbox
	if err := h.store.DeleteMailboxGrantScoped(ctx, grantID, mailboxID); err != nil {
		h.logger.Err(err).Msg("deleting mailbox grant")
		errNotFound(w, "grant not found")
		return
	}
	noContent(w)
}

// ================================================================
// Send Identities
// ================================================================

func (h *GrantHandler) ListSendIdentities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	items, err := h.store.ListSendIdentities(ctx, tenant.ID)
	if err != nil {
		h.logger.Err(err).Msg("listing send identities")
		errInternal(w)
		return
	}
	ok(w, items)
}

func (h *GrantHandler) CreateSendIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	if !middleware.IsAdmin(ctx) && !middleware.IsTenantAdmin(ctx) {
		errForbidden(w, "admin access required to manage send identities")
		return
	}

	var body struct {
		ZoneID  uuid.UUID `json:"zone_id"`
		Address string    `json:"address"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	if body.ZoneID == (uuid.UUID{}) {
		errBadRequest(w, "zone_id is required")
		return
	}
	if body.Address == "" {
		errBadRequest(w, "address is required")
		return
	}
	if !strings.Contains(body.Address, "@") {
		errBadRequest(w, "address must contain @")
		return
	}

	zone, err := h.store.GetZone(ctx, body.ZoneID)
	if err != nil {
		h.logger.Err(err).Msg("looking up zone")
		errInternal(w)
		return
	}
	if zone == nil || zone.TenantID != tenant.ID {
		errBadRequest(w, "zone not found or does not belong to tenant")
		return
	}

	parts := strings.SplitN(body.Address, "@", 2)
	if len(parts) != 2 || parts[1] != zone.Domain {
		errBadRequest(w, "address domain must match zone domain")
		return
	}

	// Determine identity type based on local part
	identityType := models.SendIdentityExact
	if parts[0] == "*" {
		identityType = models.SendIdentityDomainWildcard
	}

	si := &models.SendIdentity{
		ID:           uuid.New(),
		TenantID:     tenant.ID,
		ZoneID:       body.ZoneID,
		Address:      body.Address,
		IdentityType: identityType,
	}

	if err := h.store.CreateSendIdentity(ctx, si); err != nil {
		h.logger.Err(err).Msg("creating send identity")
		errInternal(w)
		return
	}
	created(w, si)
}

func (h *GrantHandler) DeleteSendIdentity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	if !middleware.IsAdmin(ctx) && !middleware.IsTenantAdmin(ctx) {
		errForbidden(w, "admin access required to manage send identities")
		return
	}

	si, err := h.store.GetSendIdentity(ctx, id)
	if err != nil {
		h.logger.Err(err).Msg("getting send identity")
		errInternal(w)
		return
	}
	if si == nil || si.TenantID != tenant.ID {
		errNotFound(w, "send identity not found")
		return
	}

	if err := h.store.DeleteSendIdentity(ctx, id); err != nil {
		h.logger.Err(err).Msg("deleting send identity")
		errInternal(w)
		return
	}
	noContent(w)
}

// ================================================================
// Send-As Grants
// ================================================================

func (h *GrantHandler) ListSendAsGrants(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid identity id")
		return
	}

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	si, err := h.store.GetSendIdentity(ctx, identityID)
	if err != nil {
		h.logger.Err(err).Msg("getting send identity")
		errInternal(w)
		return
	}
	if si == nil || si.TenantID != tenant.ID {
		errNotFound(w, "send identity not found")
		return
	}
	if !h.canManageSendIdentity(r, si) {
		errForbidden(w, "insufficient permissions to manage this send identity")
		return
	}

	grants, err := h.store.ListSendAsGrantsByIdentity(ctx, identityID)
	if err != nil {
		h.logger.Err(err).Msg("listing send-as grants")
		errInternal(w)
		return
	}
	ok(w, grants)
}

func (h *GrantHandler) CreateSendAsGrant(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid identity id")
		return
	}

	var body struct {
		PrincipalType string    `json:"principal_type"`
		PrincipalID   uuid.UUID `json:"principal_id"`
		DailyQuota    int       `json:"daily_quota"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	if body.PrincipalType != "user" && body.PrincipalType != "api_key" {
		errBadRequest(w, "principal_type must be 'user' or 'api_key'")
		return
	}
	if body.PrincipalID == (uuid.UUID{}) {
		errBadRequest(w, "principal_id is required")
		return
	}
	if body.DailyQuota < 0 {
		errBadRequest(w, "daily_quota must be greater than or equal to 0")
		return
	}

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	si, err := h.store.GetSendIdentity(ctx, identityID)
	if err != nil {
		h.logger.Err(err).Msg("getting send identity")
		errInternal(w)
		return
	}
	if si == nil || si.TenantID != tenant.ID {
		errNotFound(w, "send identity not found")
		return
	}

	// Authorization: caller must be able to manage this send identity
	if !h.canManageSendIdentity(r, si) {
		errForbidden(w, "insufficient permissions to manage this send identity")
		return
	}

	if body.PrincipalType == "user" {
		user, err := h.store.GetUser(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal user")
			errInternal(w)
			return
		}
		if user == nil || user.TenantID != tenant.ID {
			errBadRequest(w, "principal user not found in same tenant")
			return
		}
	} else {
		key, err := h.store.GetAPIKey(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal api key")
			errInternal(w)
			return
		}
		if key == nil || key.TenantID != tenant.ID {
			errBadRequest(w, "principal api key not found in same tenant")
			return
		}
	}

	grant := &models.SendAsGrant{
		ID:            uuid.New(),
		TenantID:      tenant.ID,
		IdentityID:    identityID,
		PrincipalType: body.PrincipalType,
		PrincipalID:   body.PrincipalID,
		DailyQuota:    body.DailyQuota,
	}

	if err := h.store.CreateSendAsGrant(ctx, grant); err != nil {
		h.logger.Err(err).Msg("creating send-as grant")
		errInternal(w)
		return
	}
	created(w, grant)
}

func (h *GrantHandler) DeleteSendAsGrant(w http.ResponseWriter, r *http.Request) {
	identityID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid identity id")
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		errBadRequest(w, "invalid grant id")
		return
	}

	ctx := r.Context()
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		errForbidden(w, "authentication required")
		return
	}

	si, err := h.store.GetSendIdentity(ctx, identityID)
	if err != nil {
		h.logger.Err(err).Msg("getting send identity")
		errInternal(w)
		return
	}
	if si == nil || si.TenantID != tenant.ID {
		errNotFound(w, "send identity not found")
		return
	}

	// Authorization: caller must be able to manage this send identity
	if !h.canManageSendIdentity(r, si) {
		errForbidden(w, "insufficient permissions to manage this send identity")
		return
	}

	// Use scoped delete to ensure the grant belongs to this identity
	if err := h.store.DeleteSendAsGrantScoped(ctx, grantID, identityID); err != nil {
		h.logger.Err(err).Msg("deleting send-as grant")
		errNotFound(w, "grant not found")
		return
	}
	noContent(w)
}

// ================================================================
// Zone Grants
// ================================================================

func (h *GrantHandler) ListZoneGrants(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid zone id")
		return
	}

	ctx := r.Context()
	zone, err := h.store.GetZone(ctx, zoneID)
	if err != nil {
		h.logger.Err(err).Msg("getting zone")
		errInternal(w)
		return
	}
	if zone == nil {
		errNotFound(w, "zone not found")
		return
	}

	if err := h.requireTenantAccess(r, zone.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}
	if !h.canManageZone(r, zoneID) {
		errForbidden(w, "insufficient permissions to manage this zone")
		return
	}

	grants, err := h.store.ListZoneGrants(ctx, zoneID)
	if err != nil {
		h.logger.Err(err).Msg("listing zone grants")
		errInternal(w)
		return
	}
	ok(w, grants)
}

func (h *GrantHandler) CreateZoneGrant(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid zone id")
		return
	}

	var body struct {
		PrincipalType string               `json:"principal_type"`
		PrincipalID   uuid.UUID            `json:"principal_id"`
		Role          models.ZoneGrantRole `json:"role"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	if body.PrincipalType != "user" && body.PrincipalType != "api_key" {
		errBadRequest(w, "principal_type must be 'user' or 'api_key'")
		return
	}
	if body.PrincipalID == (uuid.UUID{}) {
		errBadRequest(w, "principal_id is required")
		return
	}
	if !body.Role.Valid() {
		errBadRequest(w, "role must be one of: owner, admin, editor, viewer")
		return
	}

	ctx := r.Context()
	zone, err := h.store.GetZone(ctx, zoneID)
	if err != nil {
		h.logger.Err(err).Msg("getting zone")
		errInternal(w)
		return
	}
	if zone == nil {
		errNotFound(w, "zone not found")
		return
	}

	if err := h.requireTenantAccess(r, zone.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}

	// Authorization: caller must be able to manage this zone
	if !h.canManageZone(r, zoneID) {
		errForbidden(w, "insufficient permissions to manage this zone")
		return
	}

	if body.PrincipalType == "user" {
		user, err := h.store.GetUser(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal user")
			errInternal(w)
			return
		}
		if user == nil || user.TenantID != zone.TenantID {
			errBadRequest(w, "principal user not found in same tenant")
			return
		}
	} else {
		key, err := h.store.GetAPIKey(ctx, body.PrincipalID)
		if err != nil {
			h.logger.Err(err).Msg("looking up principal api key")
			errInternal(w)
			return
		}
		if key == nil || key.TenantID != zone.TenantID {
			errBadRequest(w, "principal api key not found in same tenant")
			return
		}
	}

	grant := &models.ZoneGrant{
		ID:            uuid.New(),
		TenantID:      zone.TenantID,
		ZoneID:        zoneID,
		PrincipalType: body.PrincipalType,
		PrincipalID:   body.PrincipalID,
		Role:          body.Role,
	}

	if err := h.store.CreateZoneGrant(ctx, grant); err != nil {
		h.logger.Err(err).Msg("creating zone grant")
		errInternal(w)
		return
	}
	created(w, grant)
}

func (h *GrantHandler) DeleteZoneGrant(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid zone id")
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		errBadRequest(w, "invalid grant id")
		return
	}

	ctx := r.Context()
	zone, err := h.store.GetZone(ctx, zoneID)
	if err != nil {
		h.logger.Err(err).Msg("getting zone")
		errInternal(w)
		return
	}
	if zone == nil {
		errNotFound(w, "zone not found")
		return
	}

	if err := h.requireTenantAccess(r, zone.TenantID); err != nil {
		errForbidden(w, err.Error())
		return
	}

	// Authorization: caller must be able to manage this zone
	if !h.canManageZone(r, zoneID) {
		errForbidden(w, "insufficient permissions to manage this zone")
		return
	}

	// Use scoped delete to ensure the grant belongs to this zone
	if err := h.store.DeleteZoneGrantScoped(ctx, grantID, zoneID); err != nil {
		h.logger.Err(err).Msg("deleting zone grant")
		errNotFound(w, "grant not found")
		return
	}
	noContent(w)
}

// ================================================================
// Helpers
// ================================================================

func (h *GrantHandler) requireTenantAccess(r *http.Request, tenantID uuid.UUID) error {
	ctx := r.Context()
	if middleware.IsAdmin(ctx) {
		return nil
	}
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil || tenant.ID != tenantID {
		return errors.New("access denied")
	}
	return nil
}
