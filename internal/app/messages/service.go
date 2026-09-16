package messageapp

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
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
	authz.MailboxGrantReader
	app.AuditStore
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	ForTenant(tenantID uuid.UUID) store.TenantScoped
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
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

// IsTenantAdmin reports whether the viewer has admin authority within its
// tenant — a tenant admin or a global super admin.
func (v Viewer) IsTenantAdmin() bool { return v.IsSuperAdmin || v.IsAdmin }

type Service struct {
	store      storeRepo
	obj        store.ObjectStore
	objects    *rawobject.Store
	hub        *realtime.Hub
	dispatcher *hooks.Dispatcher
	resolver   *mailboxResolver
	logger     zerolog.Logger
}

func NewService(s storeRepo, obj store.ObjectStore, objects *rawobject.Store, hub *realtime.Hub, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, logger zerolog.Logger) *Service {
	return &Service{store: s, obj: obj, objects: objects, hub: hub, dispatcher: dispatcher, resolver: newMailboxResolver(s, namingMode, stripPlus, tokenSecret), logger: logger.With().Str("service", "messages").Logger()}
}

// ResolveMailbox delegates to the mailbox-access resolver, which owns the
// address-to-mailbox lookup and the access-mode authorization matrix.
func (s *Service) ResolveMailbox(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	return s.resolver.Resolve(ctx, address, viewer)
}

// ResolveMailboxForWrite delegates to the resolver's write path.
func (s *Service) ResolveMailboxForWrite(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	return s.resolver.ResolveForWrite(ctx, address, viewer)
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
	allowed, authErr := s.canReadMessageContent(ctx, mb, viewer)
	if authErr != nil {
		return nil, 0, authErr
	}
	if !allowed {
		for i, m := range items {
			cp := *m
			cp.OTPCode = ""
			cp.OTPConfidence = 0
			items[i] = &cp
		}
	}
	return items, total, nil
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
	allowed, authErr := s.canReadMessageContent(ctx, mb, viewer)
	if authErr != nil {
		return nil, authErr
	}
	if !allowed {
		detail.OTPCode = ""
		detail.OTPConfidence = 0
		detail.BodyRedacted = true
		detail.BodyAccess = "break_glass_required"
		return detail, nil
	}
	if msg.RawObjectKey != "" {
		rc, err := s.obj.Get(ctx, msg.RawObjectKey)
		if err != nil {
			return nil, app.Internal(err)
		}
		if err == nil {
			defer rc.Close()
			env, parseErr := enmime.ReadEnvelope(rc)
			if parseErr != nil {
				return nil, app.Internal(parseErr)
			}
			if env != nil {
				detail.TextBody = env.Text
				if env.HTML != "" {
					if cleaned, err := sanitize.HTML(env.HTML); err == nil {
						detail.HTMLBody = cleaned
					} else {
						detail.HTMLBody = ""
						detail.BodyAccess = "sanitize_failed"
					}
				}
			}
		}
	}
	return detail, nil
}

func (s *Service) GetRawSource(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (io.ReadCloser, error) {
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	allowed, authErr := s.canReadMessageContent(ctx, mb, viewer)
	if authErr != nil {
		return nil, authErr
	}
	if !allowed {
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
	if mb.OwnerUserID != nil || (mb.Kind != "" && mb.Kind != "legacy") {
		if lifecycle, ok := s.store.(interface {
			TrashCompanyMessages(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, string) error
		}); ok {
			return app.Internal(lifecycle.TrashCompanyMessages(ctx, mb.TenantID, mb.ID, &msgID, actor))
		}
	}
	if err := s.store.DeleteMessage(ctx, msgID); err != nil {
		return app.Internal(err)
	}
	_, _ = s.objects.Release(ctx, msg.RawObjectKey)
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "message.delete", ResourceType: "message", ResourceID: app.UUIDPtr(msg.ID), Details: app.MustJSON(map[string]any{"mailbox": mb.FullAddress})})
	if s.hub != nil {
		s.hub.Publish(realtime.Event{Type: realtime.EventDelete, Mailbox: mb.FullAddress, MessageID: msg.ID.String(), Sender: msg.Sender, Subject: msg.Subject, Size: msg.Size})
	}
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "message.deleted", Mailbox: mb.FullAddress, MessageID: msg.ID.String(), TenantID: mb.TenantID.String()})
	}
	return nil
}

func (s *Service) PurgeMailbox(ctx context.Context, address string, viewer Viewer, actor string) error {
	mb, err := s.ResolveMailboxForWrite(ctx, address, viewer)
	if err != nil {
		return err
	}
	if mb.OwnerUserID != nil || (mb.Kind != "" && mb.Kind != "legacy") {
		if lifecycle, ok := s.store.(interface {
			TrashCompanyMessages(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, string) error
		}); ok {
			return app.Internal(lifecycle.TrashCompanyMessages(ctx, mb.TenantID, mb.ID, nil, actor))
		}
	}
	keys, err := s.store.ListMailboxObjectKeys(ctx, mb.ID)
	if err != nil {
		return app.Internal(err)
	}
	if err := s.store.PurgeMailbox(ctx, mb.ID); err != nil {
		return app.Internal(err)
	}
	for _, key := range uniqueStrings(keys) {
		if _, err := s.objects.Release(ctx, key); err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("release raw object during purge")
		}
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "mailbox.purge", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)})})
	if s.hub != nil {
		s.hub.Publish(realtime.Event{Type: realtime.EventPurge, Mailbox: mb.FullAddress})
	}
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "mailbox.purged", Mailbox: mb.FullAddress, TenantID: mb.TenantID.String()})
	}
	return nil
}

// BreakGlassRead allows an admin to read a message body with an audited
// reason. The access is logged and the full message detail is returned.
func (s *Service) BreakGlassRead(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer, actor string, reason string) (*models.MessageDetail, error) {
	if !viewer.IsTenantAdmin() {
		return nil, app.Forbidden("break-glass is only available to admin users")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, app.BadRequest("reason is required for break-glass access")
	}
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
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
	if msg.RawObjectKey != "" {
		rc, err := s.obj.Get(ctx, msg.RawObjectKey)
		if err != nil {
			return nil, app.Internal(err)
		}
		{
			defer rc.Close()
			env, err := enmime.ReadEnvelope(rc)
			if err != nil {
				return nil, app.Internal(err)
			}
			{
				detail.TextBody = env.Text
				if env.HTML != "" {
					if cleaned, err := sanitize.HTML(env.HTML); err == nil {
						detail.HTMLBody = cleaned
					} else {
						detail.HTMLBody = ""
						detail.BodyAccess = "sanitize_failed"
					}
				}
			}
		}
	}
	return detail, nil
}

// BreakGlassSource allows an admin to read raw message source with an audited reason.
func (s *Service) BreakGlassSource(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer, actor string, reason string) (io.ReadCloser, error) {
	if !viewer.IsTenantAdmin() {
		return nil, app.Forbidden("break-glass is only available to admin users")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, app.BadRequest("reason is required for break-glass access")
	}
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
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

// canReadMessageContent checks whether the viewer has permission to read
// message body / raw source. Admin roles require break-glass access for
// content; non-admin viewers who passed mailbox resolution can read content.
func (s *Service) canReadMessageContent(ctx context.Context, mb *models.Mailbox, viewer Viewer) (bool, error) {
	if mb == nil {
		return false, nil
	}
	if !viewer.IsTenantAdmin() {
		return true, nil
	}
	return s.resolver.canAccess(ctx, mb, viewer, false)
}

func (s *Service) lookupMessageForWrite(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.Mailbox, *models.Message, error) {
	mb, err := s.ResolveMailboxForWrite(ctx, address, viewer)
	if err != nil {
		return nil, nil, err
	}
	var msg *models.Message
	if viewer.Tenant != nil {
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

func (s *Service) lookupMessage(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.Mailbox, *models.Message, error) {
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return nil, nil, err
	}
	var msg *models.Message
	// For cross-tenant public access, use the resolved mailbox rather than the
	// viewer's tenant (which would miss or mask the message).
	if viewer.Tenant != nil && viewer.AuthMode != AuthModePublic && mb.TenantID == viewer.Tenant.ID {
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
