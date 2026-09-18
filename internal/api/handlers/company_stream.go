package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"io"
	"net/http"
	"strconv"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/realtime"
	"time"
)

type mailboxEventReader interface {
	GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*company.MailboxAccess, error)
	ListMailboxEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]company.MailEvent, int64, error)
}

// MailboxEventHandler has no content service and no legacy/public resolver.
// A subscription is scoped by immutable mailbox ID and current CanRead rights.
type MailboxEventHandler struct {
	eventReader mailboxEventReader
	revalidate  func(*http.Request) (*http.Request, error)
	logger      zerolog.Logger
}

func NewMailboxEventHandler(reader mailboxEventReader, revalidate func(*http.Request) (*http.Request, error), logger zerolog.Logger) *MailboxEventHandler {
	return &MailboxEventHandler{eventReader: reader, revalidate: revalidate, logger: logger}
}

func (h *MailboxEventHandler) Events(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	mb, err := h.eventReader.GetWorkMailbox(r.Context(), companyActor(r), id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	if mb == nil || !mb.CanRead || mb.Mailbox.ID != id || mb.Mailbox.TenantID != companyActor(r).TenantID {
		errForbidden(w, "mailbox read permission required")
		return
	}
	h.streamDurable(w, r, &mb.Mailbox)
}

func (h *MonitorHandler) SetStreamRevalidator(f func(*http.Request) (*http.Request, error)) {
	h.revalidate = f
}
func (h *MailboxEventHandler) streamDurable(w http.ResponseWriter, r *http.Request, mb *models.Mailbox) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	flusher, ok := w.(http.Flusher)
	if !ok {
		errInternal(w)
		return
	}
	cursor := int64(-1)
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		n, e := strconv.ParseInt(header, 10, 64)
		if e == nil && n >= 0 {
			cursor = n
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	resync := time.NewTicker(25 * time.Second)
	defer resync.Stop()
	poll := func() bool {
		fresh := r
		var err error
		if h.revalidate != nil {
			fresh, err = h.revalidate(r)
			if err != nil || fresh == nil {
				return false
			}
		}
		actor := middleware.ActorFromContext(fresh.Context())
		current, err := h.eventReader.GetWorkMailbox(fresh.Context(), actor, mb.ID)
		if err != nil || current == nil || !current.CanRead || current.Mailbox.ID != mb.ID || current.Mailbox.TenantID != mb.TenantID || actor.TenantID != mb.TenantID {
			return false
		}
		events, next, err := h.eventReader.ListMailboxEvents(fresh.Context(), mb.TenantID, mb.ID, cursor, 200)
		if err != nil {
			return false
		}
		cursor = next
		for _, event := range events {
			_, _ = fmt.Fprintf(w, "id: %d\n", event.Sequence)
			id := ""
			if event.MessageID != nil {
				id = event.MessageID.String()
			}
			writeSSE(w, event.Type, realtime.Event{Type: realtime.EventType(event.Type), Mailbox: mb.FullAddress, MessageID: id})
		}
		flusher.Flush()
		return true
	}
	if !poll() {
		return
	}
	writeSSE(w, "ready", map[string]any{"mailbox": mb.FullAddress, "cursor": cursor})
	writeSSE(w, "resync", map[string]string{"mailbox": mb.FullAddress})
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !poll() {
				return
			}
		case <-resync.C:
			if !poll() {
				return
			}
			writeSSE(w, "resync", map[string]string{"mailbox": mb.FullAddress})
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(35 * time.Second))
	data, _ := json.Marshal(payload)
	_, _ = io.WriteString(w, "event: "+event+"\n")
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
}
