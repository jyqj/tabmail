package handlers

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/realtime"
	"time"
)

type mailboxEventReader interface {
	ListMailboxEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]company.MailEvent, int64, error)
}

func (h *MessageHandler) SetStreamRevalidator(f func(*http.Request) (*http.Request, error)) {
	h.revalidate = f
}
func (h *MessageHandler) SetMailboxEventReader(st mailboxEventReader) { h.eventReader = st }
func (h *MonitorHandler) SetStreamRevalidator(f func(*http.Request) (*http.Request, error)) {
	h.revalidate = f
}
func (h *MessageHandler) streamDurable(w http.ResponseWriter, r *http.Request, mb *models.Mailbox) {
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
			if err != nil {
				return false
			}
		}
		current, err := h.service.ResolveMailbox(fresh.Context(), mb.FullAddress, h.resolveViewer(fresh))
		if err != nil || current.ID != mb.ID {
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
