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
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/hooks"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/sanitize"
	"tabmail/internal/store"
)

type messageStore interface {
	auditStore
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
}

type MessageHandler struct {
	store       messageStore
	obj         store.ObjectStore
	hub         *realtime.Hub
	dispatcher  *hooks.Dispatcher
	namingMode  policy.NamingMode
	stripPlus   bool
	tokenSecret string
	logger      zerolog.Logger
}

func NewMessageHandler(s messageStore, obj store.ObjectStore, hub *realtime.Hub, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, l zerolog.Logger) *MessageHandler {
	return &MessageHandler{store: s, obj: obj, hub: hub, dispatcher: dispatcher, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret, logger: l.With().Str("handler", "messages").Logger()}
}

// resolveMailbox finds the mailbox by address URL param and validates tenant access.
func (h *MessageHandler) resolveMailbox(w http.ResponseWriter, r *http.Request) *models.Mailbox {
	addr := strings.ToLower(chi.URLParam(r, "address"))
	if addr == "" {
		errBadRequest(w, "address is required")
		return nil
	}
	mailboxKey, err := policy.ExtractMailbox(addr, h.namingMode, h.stripPlus)
	if err != nil {
		errBadRequest(w, "invalid address")
		return nil
	}
	mb, err := h.store.GetMailboxByAddress(r.Context(), mailboxKey)
	if err != nil {
		errInternal(w)
		return nil
	}
	if mb == nil {
		errNotFound(w, "mailbox not found")
		return nil
	}
	t := middleware.TenantFromCtx(r.Context())
	if middleware.IsAdmin(r.Context()) {
		return mb
	}
	switch mb.AccessMode {
	case models.AccessPublic:
		return mb
	case models.AccessAPIKey:
		if t == nil || middleware.AuthModeFromCtx(r.Context()) != middleware.AuthModeAPIKey || mb.TenantID != t.ID {
			errForbidden(w, "api key access required")
			return nil
		}
	case models.AccessToken:
		if t != nil && middleware.AuthModeFromCtx(r.Context()) == middleware.AuthModeAPIKey && mb.TenantID == t.ID {
			return mb
		}
		token := mailboxBearerToken(r)
		if token == "" {
			errForbidden(w, "mailbox token required")
			return nil
		}
		claims, err := mailtoken.Verify(h.tokenSecret, token)
		if err != nil || claims.MailboxID != mb.ID.String() {
			errForbidden(w, "invalid mailbox token")
			return nil
		}
	default:
		errForbidden(w, "access denied")
		return nil
	}
	return mb
}

// ListMessages GET /api/v1/mailbox/{address}
func (h *MessageHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	pg := pageFromReq(r)
	msgs, total, err := h.store.ListMessages(r.Context(), mb.ID, pg)
	if err != nil {
		h.logger.Err(err).Msg("list messages")
		errInternal(w)
		return
	}
	okList(w, msgs, total, pg.Page, pg.PerPage)
}

// GetMessage GET /api/v1/mailbox/{address}/{id}
func (h *MessageHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	msg, err := h.store.GetMessage(r.Context(), msgID)
	if err != nil {
		errInternal(w)
		return
	}
	if msg == nil || msg.MailboxID != mb.ID {
		errNotFound(w, "message not found")
		return
	}
	detail := &models.MessageDetail{Message: *msg}
	if msg.RawObjectKey != "" {
		rc, err := h.obj.Get(r.Context(), msg.RawObjectKey)
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
	ok(w, detail)
}

// StreamMailbox GET /api/v1/mailbox/{address}/events
func (h *MessageHandler) StreamMailbox(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
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

// GetSource GET /api/v1/mailbox/{address}/{id}/source
func (h *MessageHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	msg, err := h.store.GetMessage(r.Context(), msgID)
	if err != nil || msg == nil || msg.MailboxID != mb.ID {
		errNotFound(w, "message not found")
		return
	}
	if msg.RawObjectKey == "" {
		errNotFound(w, "raw source not available")
		return
	}
	rc, err := h.obj.Get(r.Context(), msg.RawObjectKey)
	if err != nil {
		h.logger.Err(err).Str("key", msg.RawObjectKey).Msg("get object")
		errInternal(w)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "message/rfc822")
	_, _ = io.Copy(w, rc)
}

// MarkSeen PATCH /api/v1/mailbox/{address}/{id}
func (h *MessageHandler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	msg, err := h.store.GetMessage(r.Context(), msgID)
	if err != nil {
		errInternal(w)
		return
	}
	if msg == nil || msg.MailboxID != mb.ID {
		errNotFound(w, "message not found")
		return
	}
	if err := h.store.MarkSeen(r.Context(), msgID); err != nil {
		errInternal(w)
		return
	}
	ok(w, map[string]bool{"seen": true})
}

// DeleteMessage DELETE /api/v1/mailbox/{address}/{id}
func (h *MessageHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	msgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid message id")
		return
	}
	msg, err := h.store.GetMessage(r.Context(), msgID)
	if err != nil || msg == nil || msg.MailboxID != mb.ID {
		errNotFound(w, "message not found")
		return
	}
	if err := h.store.DeleteMessage(r.Context(), msgID); err != nil {
		errInternal(w)
		return
	}
	if msg.RawObjectKey != "" {
		refs, err := h.store.CountMessagesByObjectKey(r.Context(), msg.RawObjectKey)
		if err == nil && refs == 0 {
			_ = h.obj.Delete(r.Context(), msg.RawObjectKey)
		}
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(mb.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "message.delete",
		ResourceType: "message",
		ResourceID:   uuidPtr(msg.ID),
		Details:      mustJSON(map[string]any{"mailbox": mb.FullAddress}),
	})
	if h.hub != nil {
		h.hub.Publish(realtime.Event{
			Type:      realtime.EventDelete,
			Mailbox:   mb.FullAddress,
			MessageID: msg.ID.String(),
			Sender:    msg.Sender,
			Subject:   msg.Subject,
			Size:      msg.Size,
		})
	}
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:      "message.deleted",
			Mailbox:   mb.FullAddress,
			MessageID: msg.ID.String(),
			TenantID:  mb.TenantID.String(),
		})
	}
	noContent(w)
}

// PurgeMailbox DELETE /api/v1/mailbox/{address}
func (h *MessageHandler) PurgeMailbox(w http.ResponseWriter, r *http.Request) {
	mb := h.resolveMailbox(w, r)
	if mb == nil {
		return
	}
	keys, err := h.store.ListMailboxObjectKeys(r.Context(), mb.ID)
	if err != nil {
		errInternal(w)
		return
	}
	if err := h.store.PurgeMailbox(r.Context(), mb.ID); err != nil {
		errInternal(w)
		return
	}
	for _, key := range uniqueStrings(keys) {
		refs, err := h.store.CountMessagesByObjectKey(r.Context(), key)
		if err != nil {
			h.logger.Warn().Err(err).Str("key", key).Msg("count object references during purge")
			continue
		}
		if refs == 0 {
			if err := h.obj.Delete(r.Context(), key); err != nil {
				h.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during purge")
			}
		}
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(mb.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "mailbox.purge",
		ResourceType: "mailbox",
		ResourceID:   uuidPtr(mb.ID),
		Details:      mustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)}),
	})
	if h.hub != nil {
		h.hub.Publish(realtime.Event{
			Type:    realtime.EventPurge,
			Mailbox: mb.FullAddress,
		})
	}
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:     "mailbox.purged",
			Mailbox:  mb.FullAddress,
			TenantID: mb.TenantID.String(),
		})
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
