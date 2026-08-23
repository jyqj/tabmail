package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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

type MessageHandler struct {
	service *messageapp.Service
	hub     *realtime.Hub
	logger  zerolog.Logger
}

func NewMessageHandler(s messageStore, obj store.ObjectStore, objects *rawobject.Store, hub *realtime.Hub, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, l zerolog.Logger) *MessageHandler {
	service := messageapp.NewService(s, obj, objects, hub, dispatcher, namingMode, stripPlus, tokenSecret, l)
	return &MessageHandler{service: service, hub: hub, logger: l.With().Str("handler", "messages").Logger()}
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

func (h *MessageHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	items, total, err := h.service.ListMessages(r.Context(), chi.URLParam(r, "address"), h.resolveViewer(r), pg)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}

func (h *MessageHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	item, err := h.service.GetMessageDetail(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *MessageHandler) StreamMailbox(w http.ResponseWriter, r *http.Request) {
	mb, err := h.service.ResolveMailbox(r.Context(), chi.URLParam(r, "address"), h.resolveViewer(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		errInternal(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	if h.hub == nil {
		writeSSE(w, "ping", realtime.Event{Type: realtime.EventPing, Mailbox: mb.FullAddress})
		flusher.Flush()
		<-r.Context().Done()
		return
	}
	ch, unsubscribe := h.hub.Subscribe(mb.FullAddress)
	defer unsubscribe()
	writeSSE(w, "ready", realtime.Event{Type: realtime.EventPing, Mailbox: mb.FullAddress})
	flusher.Flush()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			writeSSE(w, string(event.Type), event)
			flusher.Flush()
		case <-ticker.C:
			writeSSE(w, "ping", realtime.Event{Type: realtime.EventPing, Mailbox: mb.FullAddress})
			flusher.Flush()
		}
	}
}

func (h *MessageHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	rc, err := h.service.GetRawSource(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "message/rfc822")
	_, _ = io.Copy(w, rc)
}

func (h *MessageHandler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	if err := h.service.MarkSeen(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, map[string]bool{"seen": true})
}

func (h *MessageHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	if err := h.service.DeleteMessage(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r), middleware.ActorFromContext(r.Context()).AuditLabel()); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *MessageHandler) BreakGlassRead(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.BreakGlassRead(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r), middleware.ActorFromContext(r.Context()).AuditLabel(), body.Reason)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *MessageHandler) BreakGlassSource(w http.ResponseWriter, r *http.Request) {
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	rc, err := h.service.BreakGlassSource(r.Context(), chi.URLParam(r, "address"), msgID, h.resolveViewer(r), middleware.ActorFromContext(r.Context()).AuditLabel(), body.Reason)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "message/rfc822")
	_, _ = io.Copy(w, rc)
}

func (h *MessageHandler) PurgeMailbox(w http.ResponseWriter, r *http.Request) {
	if err := h.service.PurgeMailbox(r.Context(), chi.URLParam(r, "address"), h.resolveViewer(r), middleware.ActorFromContext(r.Context()).AuditLabel()); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
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
	data, _ := json.Marshal(payload)
	_, _ = io.WriteString(w, "event: "+event+"\n")
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}
