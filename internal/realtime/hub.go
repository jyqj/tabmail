package realtime

import (
	"container/ring"
	"context"
	"sync"
	"time"

	"tabmail/internal/metrics"
	"tabmail/internal/models"
)

type EventType string

const (
	EventMessage EventType = "message"
	EventDelete  EventType = "delete"
	EventPurge   EventType = "purge"
	EventPing    EventType = "ping"
)

type Event struct {
	Type      EventType `json:"type"`
	Mailbox   string    `json:"mailbox"`
	MessageID string    `json:"message_id,omitempty"`
	Sender    string    `json:"sender,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	Size      int64     `json:"size,omitempty"`
	At        time.Time `json:"at"`
}

type Hub struct {
	mu          sync.RWMutex
	nextID      int
	listeners   map[string]map[int]chan Event
	history     *ring.Ring
	historySize int
	recorder    Recorder
}

type Recorder interface {
	CreateMonitorEvent(ctx context.Context, e *models.MonitorEvent) error
}

func NewHub(historySize int, recorder Recorder) *Hub {
	var history *ring.Ring
	if historySize > 0 {
		history = ring.New(historySize)
	}
	return &Hub{
		listeners:   make(map[string]map[int]chan Event),
		history:     history,
		historySize: historySize,
		recorder:    recorder,
	}
}

func (h *Hub) Subscribe(mailbox string) (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	if h.listeners[mailbox] == nil {
		h.listeners[mailbox] = make(map[int]chan Event)
	}
	ch := make(chan Event, 64)
	if mailbox == "" && h.history != nil {
		h.history.Do(func(v any) {
			if event, ok := v.(Event); ok {
				select {
				case ch <- event:
				default:
				}
			}
		})
	}
	h.listeners[mailbox][id] = ch
	metrics.RealtimeSubscriberAdded()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if m := h.listeners[mailbox]; m != nil {
			if c, ok := m[id]; ok {
				delete(m, id)
				close(c)
				metrics.RealtimeSubscriberRemoved()
			}
			if len(m) == 0 {
				delete(h.listeners, mailbox)
			}
		}
	}
}

func (h *Hub) Publish(event Event) {
	event.At = time.Now().UTC()
	metrics.RealtimeEventPublished()

	h.mu.Lock()
	if h.history != nil {
		h.history.Value = event
		h.history = h.history.Next()
	}
	mailboxListeners := make([]chan Event, 0, len(h.listeners[event.Mailbox]))
	for _, ch := range h.listeners[event.Mailbox] {
		mailboxListeners = append(mailboxListeners, ch)
	}
	globalListeners := make([]chan Event, 0, len(h.listeners[""]))
	for _, ch := range h.listeners[""] {
		globalListeners = append(globalListeners, ch)
	}
	h.mu.Unlock()

	if h.recorder != nil {
		_ = h.recorder.CreateMonitorEvent(context.Background(), &models.MonitorEvent{
			Type:      string(event.Type),
			Mailbox:   event.Mailbox,
			MessageID: event.MessageID,
			Sender:    event.Sender,
			Subject:   event.Subject,
			Size:      event.Size,
			At:        event.At,
		})
	}
	for _, ch := range mailboxListeners {
		select {
		case ch <- event:
		default:
		}
	}
	for _, ch := range globalListeners {
		select {
		case ch <- event:
		default:
		}
	}
}
