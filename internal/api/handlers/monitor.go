package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/realtime"
)

type monitorStore interface {
	ListMonitorEvents(ctx context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error)
}

type MonitorHandler struct {
	revalidate func(*http.Request) (*http.Request, error)
	store      monitorStore
	hub        *realtime.Hub
	logger     zerolog.Logger
}

func NewMonitorHandler(store monitorStore, hub *realtime.Hub, logger zerolog.Logger) *MonitorHandler {
	return &MonitorHandler{store: store, hub: hub, logger: logger.With().Str("handler", "monitor").Logger()}
}

// StreamAll GET /api/v1/admin/monitor/events
func (h *MonitorHandler) StreamAll(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		errInternal(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")

	// Read the shared monitor journal, not this API process's local Hub.
	// The history endpoint is the source of truth; resync covers journal gaps.
	seen := map[string]bool{}
	poll := func(initial bool) bool {
		fresh := r
		if h.revalidate != nil {
			var e error
			fresh, e = h.revalidate(r)
			if e != nil {
				return false
			}
		}
		if !middleware.IsSuperAdmin(fresh.Context()) {
			return false
		}
		rows, _, e := h.store.ListMonitorEvents(fresh.Context(), models.Page{Page: 1, PerPage: 100}, "", "", "")
		if e != nil {
			return false
		}
		next := map[string]bool{}
		for i := len(rows) - 1; i >= 0; i-- {
			v := rows[i]
			key := v.ID.String()
			next[key] = true
			if !initial && !seen[key] {
				writeSSE(w, v.Type, realtime.Event{Type: realtime.EventType(v.Type), Mailbox: v.Mailbox, MessageID: v.MessageID, Sender: v.Sender, Subject: v.Subject, Size: v.Size, At: v.At})
			}
		}
		seen = next
		writeSSE(w, "resync", map[string]bool{"history": true})
		flusher.Flush()
		return true
	}
	if !poll(true) {
		return
	}
	writeSSE(w, "ready", map[string]bool{"ready": true})
	flusher.Flush()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !poll(false) {
				return
			}
		}
	}

}

func (h *MonitorHandler) History(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	eventType := r.URL.Query().Get("type")
	mailbox := r.URL.Query().Get("mailbox")
	sender := r.URL.Query().Get("sender")
	items, total, err := h.store.ListMonitorEvents(r.Context(), pg, eventType, mailbox, sender)
	if err != nil {
		errInternal(w)
		return
	}
	okList(w, items, total, pg.Page, pg.PerPage)
}
