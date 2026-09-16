package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	messageapp "tabmail/internal/app/messages"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/store"
)

type messageStore interface {
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

// MessageHandler carries the shared message plumbing (viewer resolution and
// the durable SSE stream) used by the company mailbox event and message
// endpoints. The legacy /api/v1/mailbox/{address} HTTP surface was removed;
// the underlying messageapp.Service methods remain shared infrastructure.
type MessageHandler struct {
	revalidate  func(*http.Request) (*http.Request, error)
	eventReader mailboxEventReader
	service     *messageapp.Service
	logger      zerolog.Logger
}

func NewMessageHandler(s messageStore, obj store.ObjectStore, objects *rawobject.Store, hub *realtime.Hub, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, l zerolog.Logger) *MessageHandler {
	service := messageapp.NewService(s, obj, objects, hub, dispatcher, namingMode, stripPlus, tokenSecret, l)
	return &MessageHandler{service: service, logger: l.With().Str("handler", "messages").Logger()}
}

func (h *MessageHandler) resolveViewer(r *http.Request) messageapp.Viewer {
	actor := middleware.ActorFromContext(r.Context())
	var userID *uuid.UUID
	if actor.Type == authz.PrincipalUser {
		id := actor.ID
		userID = &id
	}
	var principalID *uuid.UUID
	if actor.Type != "" && actor.ID != uuid.Nil {
		id := actor.ID
		principalID = &id
	}
	var allowedZoneIDs []uuid.UUID
	if actor.Permission != nil && len(actor.Permission.AllowedZoneIDs) > 0 {
		allowedZoneIDs = append([]uuid.UUID(nil), actor.Permission.AllowedZoneIDs...)
	}
	mode := middleware.AuthModeFromCtx(r.Context())
	return messageapp.Viewer{
		Tenant:         middleware.TenantFromCtx(r.Context()),
		IsSuperAdmin:   actor.IsSuperAdmin,
		IsAdmin:        actor.IsAdmin,
		AuthMode:       mode,
		UserID:         userID,
		OwnerUserID:    actor.OwnerUserID,
		TenantWide:     actor.TenantWide,
		BearerToken:    mailboxBearerToken(r),
		PrincipalType:  string(actor.Type),
		PrincipalID:    principalID,
		AllowedZoneIDs: allowedZoneIDs,
	}
}

func mailboxBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	data, _ := json.Marshal(payload)
	_, _ = io.WriteString(w, "event: "+event+"\n")
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}
