package handlers

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/realtime"
	"tabmail/internal/store"
)

type MonitorHandler struct {
	store  store.Store
	hub    *realtime.Hub
	logger zerolog.Logger
}

func NewMonitorHandler(store store.Store, hub *realtime.Hub, logger zerolog.Logger) *MonitorHandler {
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

	if h.hub == nil {
		writeSSE(w, "ping", realtime.Event{Type: realtime.EventPing})
		flusher.Flush()
		<-r.Context().Done()
		return
	}

	ch, unsubscribe := h.hub.Subscribe("")
	defer unsubscribe()

	writeSSE(w, "ready", realtime.Event{Type: realtime.EventPing})
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
			writeSSE(w, "ping", realtime.Event{Type: realtime.EventPing})
			flusher.Flush()
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
