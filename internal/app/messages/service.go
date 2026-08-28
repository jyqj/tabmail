package messageapp

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/hooks"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/sanitize"
	"tabmail/internal/store"
)

const (
	AuthModeAPIKey = "api_key"
	AuthModeUser   = "user"
	AuthModePublic = "public"
)

type storeRepo interface {
	store.Transactor
	app.AuditStore
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	ListMessagesKeyset(ctx context.Context, mailboxID uuid.UUID, before *models.MessageCursor, limit int) ([]*models.Message, error)
	CountMessages(ctx context.Context, mailboxID uuid.UUID) (int, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	ForTenant(tenantID uuid.UUID) store.TenantScoped
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	CountRawObjectReferences(ctx context.Context, objectKey string) (int, error)
}

type Viewer struct {
	Tenant         *models.Tenant
	IsSuperAdmin   bool
	IsAdmin        bool
	AuthMode       string
	UserID         *uuid.UUID
	OwnerUserID    *uuid.UUID
	TenantWide     bool
	BearerToken    string
	PrincipalType  string
	PrincipalID    *uuid.UUID
	AllowedZoneIDs []uuid.UUID
}

type Service struct {
	store       storeRepo
	obj         store.ObjectStore
	hub         *realtime.Hub
	dispatcher  *hooks.Dispatcher
	namingMode  policy.NamingMode
	stripPlus   bool
	tokenSecret string
	logger      zerolog.Logger
}

func NewService(s storeRepo, obj store.ObjectStore, hub *realtime.Hub, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, logger zerolog.Logger) *Service {
	return &Service{store: s, obj: obj, hub: hub, dispatcher: dispatcher, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret, logger: logger.With().Str("service", "messages").Logger()}
}

// ResolveMailbox maps an address to a mailbox the viewer may read. Authenticated
// callers are looked up in their tenant first, but a global fallback is retained
// so explicitly-public mailboxes remain reachable. Administrator bypass is
// tenant-bound: even a platform administrator must select the target tenant via
// X-Tenant-ID before protected mailbox data becomes visible.
func (s *Service) ResolveMailbox(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return nil, app.BadRequest("address is required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, s.namingMode, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}

	var mb *models.Mailbox
	if viewer.Tenant != nil && viewer.AuthMode != AuthModePublic {
		mb, err = s.store.ForTenant(viewer.Tenant.ID).GetMailboxByAddress(ctx, mailboxKey)
		if err == nil && mb == nil {
			mb, err = s.store.GetMailboxByAddress(ctx, mailboxKey)
		}
	} else {
		mb, err = s.store.GetMailboxByAddress(ctx, mailboxKey)
	}
	if err != nil {
		return nil, app.Internal(err)
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if viewerIsAdminForMailbox(viewer, mb) {
		return mb, nil
	}
	// A global fallback exists only so public mailboxes remain reachable. Do
	// not confirm the existence or state of protected cross-tenant mailboxes.
	if viewer.Tenant != nil && mb.TenantID != viewer.Tenant.ID && mb.AccessMode != models.AccessPublic {
		mailboxCapability := viewer.AuthMode == AuthModePublic &&
			mb.AccessMode == models.AccessToken &&
			strings.TrimSpace(viewer.BearerToken) != ""
		if !mailboxCapability {
			return nil, app.NotFound("mailbox not found")
		}
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, accessDeniedOrNotFound(viewer, "mailbox expired")
	}
	canManage, err := s.canAccessMailbox(ctx, mb, viewer)
	if err != nil {
		return nil, err
	}
	if canManage {
		return mb, nil
	}

	switch mb.AccessMode {
	case models.AccessPublic:
		return mb, nil
	case models.AccessAPIKey:
		if viewer.Tenant == nil || viewer.AuthMode != AuthModeAPIKey || mb.TenantID != viewer.Tenant.ID {
			return nil, accessDeniedOrNotFound(viewer, "api key access required")
		}
		if !viewerZoneAllowed(viewer, mb.ZoneID) {
			return nil, app.Forbidden("zone not in allowed list")
		}
		if !viewer.TenantWide {
			return nil, accessDeniedOrNotFound(viewer, "api key requires tenant-wide access")
		}
	case models.AccessToken:
		if viewer.Tenant != nil && viewer.AuthMode == AuthModeAPIKey && viewer.TenantWide && mb.TenantID == viewer.Tenant.ID {
			return mb, nil
		}
		if strings.TrimSpace(viewer.BearerToken) == "" {
			return nil, accessDeniedOrNotFound(viewer, "mailbox token required")
		}
		claims, err := mailtoken.Verify(s.tokenSecret, viewer.BearerToken)
		if err != nil || claims.MailboxID != mb.ID.String() {
			return nil, accessDeniedOrNotFound(viewer, "invalid mailbox token")
		}
	default:
		return nil, accessDeniedOrNotFound(viewer, "access denied")
	}
	return mb, nil
}

// ResolveMailboxForWrite never performs a cross-tenant fallback. Platform
// administrators must explicitly impersonate the target tenant before a write.
func (s *Service) ResolveMailboxForWrite(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	if viewer.AuthMode == AuthModePublic {
		return nil, app.Forbidden("authentication required for write operations")
	}
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return nil, app.BadRequest("address is required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, s.namingMode, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}
	if viewer.Tenant == nil {
		return nil, app.Forbidden("tenant context required for write operations")
	}
	mb, err := s.store.ForTenant(viewer.Tenant.ID).GetMailboxByAddress(ctx, mailboxKey)
	if err != nil {
		return nil, app.Internal(err)
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if viewerIsAdminForMailbox(viewer, mb) {
		return mb, nil
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, app.Forbidden("mailbox expired")
	}
	canManage, err := s.canAccessMailbox(ctx, mb, viewer)
	if err != nil {
		return nil, err
	}
	if canManage {
		return mb, nil
	}
	return nil, app.Forbidden("write access requires mailbox owner or admin")
}

func (s *Service) canAccessMailbox(ctx context.Context, mb *models.Mailbox, viewer Viewer) (bool, error) {
	if mb == nil {
		return false, nil
	}
	if viewerIsAdminForMailbox(viewer, mb) {
		return true, nil
	}
	if viewer.Tenant == nil || mb.TenantID != viewer.Tenant.ID {
		return false, nil
	}
	if !viewerZoneAllowed(viewer, mb.ZoneID) {
		return false, app.Forbidden("zone not in allowed list")
	}
	if viewer.TenantWide {
		return true, nil
	}
	zone, err := s.store.GetZone(ctx, mb.ZoneID)
	if err != nil {
		return false, app.Internal(err)
	}
	if zone == nil || zone.OwnerUserID == nil {
		return false, nil
	}
	if viewer.UserID != nil && *viewer.UserID == *zone.OwnerUserID {
		return true, nil
	}
	return viewer.OwnerUserID != nil && *viewer.OwnerUserID == *zone.OwnerUserID, nil
}

func viewerIsAdminForMailbox(viewer Viewer, mb *models.Mailbox) bool {
	return mb != nil &&
		viewer.Tenant != nil &&
		mb.TenantID == viewer.Tenant.ID &&
		(viewer.IsSuperAdmin || viewer.IsAdmin)
}

func viewerZoneAllowed(viewer Viewer, zoneID uuid.UUID) bool {
	if len(viewer.AllowedZoneIDs) == 0 {
		return true
	}
	for _, id := range viewer.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}

func accessDeniedOrNotFound(viewer Viewer, msg string) error {
	if viewer.AuthMode == AuthModePublic {
		return app.NotFound("mailbox not found")
	}
	return app.Forbidden(msg)
}

func (s *Service) ListMessages(ctx context.Context, address string, viewer Viewer, pg models.Page) ([]*models.Message, int, error) {
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := s.store.ListMessages(ctx, mb.ID, pg)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	return items, total, nil
}

// ListMessagesKeyset lists messages after the cursor position. It returns the
// page, the mailbox's total message count (an O(1) counter read, not a scan),
// and the cursor for the next page — empty when this page ends the list.
func (s *Service) ListMessagesKeyset(ctx context.Context, address string, viewer Viewer, cursor *models.MessageCursor, limit int) ([]*models.Message, int, string, error) {
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return nil, 0, "", err
	}
	items, err := s.store.ListMessagesKeyset(ctx, mb.ID, cursor, limit)
	if err != nil {
		return nil, 0, "", app.Internal(err)
	}
	total, err := s.store.CountMessages(ctx, mb.ID)
	if err != nil {
		return nil, 0, "", app.Internal(err)
	}
	next := ""
	if len(items) == limit && limit > 0 {
		next = EncodeCursor(items[len(items)-1])
	}
	return items, total, next, nil
}

func (s *Service) GetMessageDetail(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.MessageDetail, error) {
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	detail := &models.MessageDetail{Message: *msg}
	if !s.canReadMessageContent(mb, viewer) {
		detail.BodyRedacted = true
		detail.BodyAccess = "break_glass_required"
		return detail, nil
	}
	s.populateMessageBodies(ctx, msg, detail)
	return detail, nil
}

func (s *Service) populateMessageBodies(ctx context.Context, msg *models.Message, detail *models.MessageDetail) {
	if msg == nil || detail == nil || msg.RawObjectKey == "" {
		return
	}
	rc, err := s.obj.Get(ctx, msg.RawObjectKey)
	if err != nil {
		return
	}
	defer rc.Close()
	env, err := enmime.ReadEnvelope(rc)
	if err != nil {
		return
	}
	detail.TextBody = env.Text
	if env.HTML == "" {
		return
	}
	cleaned, err := sanitize.HTML(env.HTML)
	if err != nil {
		// Sanitizer failure is a security failure, not a reason to return raw
		// attacker-controlled HTML to API clients.
		detail.HTMLBody = ""
		detail.BodyAccess = "sanitize_failed"
		return
	}
	detail.HTMLBody = cleaned
}

func (s *Service) GetRawSource(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (io.ReadCloser, error) {
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	if !s.canReadMessageContent(mb, viewer) {
		return nil, app.Forbidden("message source access requires break-glass")
	}
	if msg.RawObjectKey == "" {
		return nil, app.NotFound("raw source not available")
	}
	rc, err := s.obj.Get(ctx, msg.RawObjectKey)
	if err != nil {
		s.logger.Err(err).Str("key", msg.RawObjectKey).Msg("get object")
		return nil, app.Internal(err)
	}
	return rc, nil
}

func (s *Service) MarkSeen(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) error {
	mb, msg, err := s.lookupMessageForWrite(ctx, address, msgID, viewer)
	if err != nil {
		return err
	}
	if msg.MailboxID != mb.ID {
		return app.NotFound("message not found")
	}
	if err := s.store.MarkSeen(ctx, msgID); err != nil {
		return app.Internal(err)
	}
	return nil
}

func (s *Service) DeleteMessage(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer, actor string) error {
	mb, msg, err := s.lookupMessageForWrite(ctx, address, msgID, viewer)
	if err != nil {
		return err
	}
	if msg.MailboxID != mb.ID {
		return app.NotFound("message not found")
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "message.delete", ResourceType: "message", ResourceID: app.UUIDPtr(msg.ID), Details: app.MustJSON(map[string]any{"mailbox": mb.FullAddress})}
	event := hooks.Event{Type: "message.deleted", Mailbox: mb.FullAddress, MessageID: msg.ID.String(), TenantID: mb.TenantID.String()}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeleteMessage(txCtx, msgID)
	}); err != nil {
		return err
	}

	if msg.RawObjectKey != "" {
		refs, err := s.store.CountRawObjectReferences(ctx, msg.RawObjectKey)
		if err != nil {
			s.logger.Warn().Err(err).Str("key", msg.RawObjectKey).Msg("count object references after message delete")
		} else if refs == 0 && s.obj != nil {
			if err := s.obj.Delete(ctx, msg.RawObjectKey); err != nil {
				s.logger.Warn().Err(err).Str("key", msg.RawObjectKey).Msg("delete orphan raw object after message delete")
			}
		}
	}
	if s.hub != nil {
		s.hub.Publish(realtime.Event{Type: realtime.EventDelete, Mailbox: mb.FullAddress, MessageID: msg.ID.String(), Sender: msg.Sender, Subject: msg.Subject, Size: msg.Size})
	}
	return nil
}

func (s *Service) PurgeMailbox(ctx context.Context, address string, viewer Viewer, actor string) error {
	mb, err := s.ResolveMailboxForWrite(ctx, address, viewer)
	if err != nil {
		return err
	}
	keys, err := s.store.ListMailboxObjectKeys(ctx, mb.ID)
	if err != nil {
		return app.Internal(err)
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "mailbox.purge", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)})}
	event := hooks.Event{Type: "mailbox.purged", Mailbox: mb.FullAddress, TenantID: mb.TenantID.String()}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.PurgeMailbox(txCtx, mb.ID)
	}); err != nil {
		return err
	}

	for _, key := range uniqueStrings(keys) {
		refs, err := s.store.CountRawObjectReferences(ctx, key)
		if err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("count object references during purge")
			continue
		}
		if refs == 0 && s.obj != nil {
			if err := s.obj.Delete(ctx, key); err != nil {
				s.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during purge")
			}
		}
	}
	if s.hub != nil {
		s.hub.Publish(realtime.Event{Type: realtime.EventPurge, Mailbox: mb.FullAddress})
	}
	return nil
}

// BreakGlassRead allows a tenant administrator to read a message body with an
// audited reason. Platform administrators must first select the target tenant.
func (s *Service) BreakGlassRead(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer, actor string, reason string) (*models.MessageDetail, error) {
	if !viewer.IsSuperAdmin && !viewer.IsAdmin {
		return nil, app.Forbidden("break-glass is only available to admin users")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, app.BadRequest("reason is required for break-glass access")
	}
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if !viewerIsAdminForMailbox(viewer, mb) {
		return nil, app.Forbidden("break-glass requires administrator access to the mailbox tenant")
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	if err := app.InsertAuditRequired(ctx, s.store, models.AuditEntry{
		TenantID:     app.UUIDPtr(mb.TenantID),
		Actor:        actor,
		Action:       "message.break_glass_read",
		ResourceType: "message",
		ResourceID:   app.UUIDPtr(msg.ID),
		Details:      app.MustJSON(map[string]any{"mailbox": mb.FullAddress, "reason": reason, "scope": "body"}),
	}); err != nil {
		return nil, app.Internal(err)
	}
	detail := &models.MessageDetail{Message: *msg}
	s.populateMessageBodies(ctx, msg, detail)
	return detail, nil
}

// BreakGlassSource allows an administrator scoped to the mailbox tenant to
// read raw message source with an audited reason.
func (s *Service) BreakGlassSource(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer, actor string, reason string) (io.ReadCloser, error) {
	if !viewer.IsSuperAdmin && !viewer.IsAdmin {
		return nil, app.Forbidden("break-glass is only available to admin users")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, app.BadRequest("reason is required for break-glass access")
	}
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if !viewerIsAdminForMailbox(viewer, mb) {
		return nil, app.Forbidden("break-glass requires administrator access to the mailbox tenant")
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	if msg.RawObjectKey == "" {
		return nil, app.NotFound("raw source not available")
	}
	if err := app.InsertAuditRequired(ctx, s.store, models.AuditEntry{
		TenantID:     app.UUIDPtr(mb.TenantID),
		Actor:        actor,
		Action:       "message.break_glass_read",
		ResourceType: "message",
		ResourceID:   app.UUIDPtr(msg.ID),
		Details:      app.MustJSON(map[string]any{"mailbox": mb.FullAddress, "reason": reason, "scope": "source"}),
	}); err != nil {
		return nil, app.Internal(err)
	}
	rc, err := s.obj.Get(ctx, msg.RawObjectKey)
	if err != nil {
		s.logger.Err(err).Str("key", msg.RawObjectKey).Msg("get object")
		return nil, app.Internal(err)
	}
	return rc, nil
}

// canReadMessageContent requires tenant administrators to use the audited
// break-glass path for mailboxes they administer. Cross-tenant public mailboxes
// remain ordinary public resources rather than inheriting the viewer's admin
// role.
func (s *Service) canReadMessageContent(mb *models.Mailbox, viewer Viewer) bool {
	return !viewerIsAdminForMailbox(viewer, mb)
}

func (s *Service) lookupMessageForWrite(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.Mailbox, *models.Message, error) {
	mb, err := s.ResolveMailboxForWrite(ctx, address, viewer)
	if err != nil {
		return nil, nil, err
	}
	if viewer.Tenant == nil {
		return nil, nil, app.Forbidden("tenant context required for write operations")
	}
	msg, err := s.store.ForTenant(viewer.Tenant.ID).GetMessage(ctx, msgID)
	if err != nil {
		return nil, nil, app.Internal(err)
	}
	if msg == nil || msg.MailboxID != mb.ID {
		return nil, nil, app.NotFound("message not found")
	}
	return mb, msg, nil
}

func (s *Service) lookupMessage(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.Mailbox, *models.Message, error) {
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return nil, nil, err
	}
	var msg *models.Message
	if viewer.Tenant != nil && mb.TenantID == viewer.Tenant.ID {
		msg, err = s.store.ForTenant(viewer.Tenant.ID).GetMessage(ctx, msgID)
	} else {
		msg, err = s.store.GetMessage(ctx, msgID)
	}
	if err != nil {
		return nil, nil, app.Internal(err)
	}
	if msg == nil || msg.MailboxID != mb.ID {
		return nil, nil, app.NotFound("message not found")
	}
	return mb, msg, nil
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
