package messageapp

import (
	"context"
	"io"
	"strings"

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

const AuthModeAPIKey = "api_key"

type storeRepo interface {
	app.AuditStore
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
}

type Viewer struct {
	Tenant      *models.Tenant
	IsAdmin     bool
	AuthMode    string
	BearerToken string
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

func (s *Service) ResolveMailbox(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return nil, app.BadRequest("address is required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, s.namingMode, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}
	mb, err := s.store.GetMailboxByAddress(ctx, mailboxKey)
	if err != nil {
		return nil, app.Internal(err)
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if viewer.IsAdmin {
		return mb, nil
	}
	switch mb.AccessMode {
	case models.AccessPublic:
		return mb, nil
	case models.AccessAPIKey:
		if viewer.Tenant == nil || viewer.AuthMode != AuthModeAPIKey || mb.TenantID != viewer.Tenant.ID {
			return nil, app.Forbidden("api key access required")
		}
	case models.AccessToken:
		if viewer.Tenant != nil && viewer.AuthMode == AuthModeAPIKey && mb.TenantID == viewer.Tenant.ID {
			return mb, nil
		}
		if strings.TrimSpace(viewer.BearerToken) == "" {
			return nil, app.Forbidden("mailbox token required")
		}
		claims, err := mailtoken.Verify(s.tokenSecret, viewer.BearerToken)
		if err != nil || claims.MailboxID != mb.ID.String() {
			return nil, app.Forbidden("invalid mailbox token")
		}
	default:
		return nil, app.Forbidden("access denied")
	}
	return mb, nil
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

func (s *Service) GetMessageDetail(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.MessageDetail, error) {
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return nil, err
	}
	if msg.MailboxID != mb.ID {
		return nil, app.NotFound("message not found")
	}
	detail := &models.MessageDetail{Message: *msg}
	if msg.RawObjectKey != "" {
		rc, err := s.obj.Get(ctx, msg.RawObjectKey)
		if err == nil {
			defer rc.Close()
			if env, err := enmime.ReadEnvelope(rc); err == nil {
				detail.TextBody = env.Text
				if env.HTML != "" {
					if cleaned, err := sanitize.HTML(env.HTML); err == nil {
						detail.HTMLBody = cleaned
					} else {
						detail.HTMLBody = env.HTML
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
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
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
	mb, msg, err := s.lookupMessage(ctx, address, msgID, viewer)
	if err != nil {
		return err
	}
	if msg.MailboxID != mb.ID {
		return app.NotFound("message not found")
	}
	if err := s.store.DeleteMessage(ctx, msgID); err != nil {
		return app.Internal(err)
	}
	if msg.RawObjectKey != "" {
		refs, err := s.store.CountMessagesByObjectKey(ctx, msg.RawObjectKey)
		if err == nil && refs == 0 {
			_ = s.obj.Delete(ctx, msg.RawObjectKey)
		}
	}
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
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return err
	}
	keys, err := s.store.ListMailboxObjectKeys(ctx, mb.ID)
	if err != nil {
		return app.Internal(err)
	}
	if err := s.store.PurgeMailbox(ctx, mb.ID); err != nil {
		return app.Internal(err)
	}
	for _, key := range uniqueStrings(keys) {
		refs, err := s.store.CountMessagesByObjectKey(ctx, key)
		if err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("count object references during purge")
			continue
		}
		if refs == 0 {
			if err := s.obj.Delete(ctx, key); err != nil {
				s.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during purge")
			}
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

func (s *Service) lookupMessage(ctx context.Context, address string, msgID uuid.UUID, viewer Viewer) (*models.Mailbox, *models.Message, error) {
	mb, err := s.ResolveMailbox(ctx, address, viewer)
	if err != nil {
		return nil, nil, err
	}
	msg, err := s.store.GetMessage(ctx, msgID)
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
