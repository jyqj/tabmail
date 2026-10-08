package handlers

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// These tests drive the two shipping consumers. The writer implements the
// same ResponseController ports as net/http; only the transport and final
// persistence reader are controlled. No PostgreSQL execution is implied.
type r5SSEReader struct {
	mailbox models.Mailbox
	reads   int
	cursors []int64
}

func (s *r5SSEReader) GetWorkMailbox(_ context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	if a.TenantID != s.mailbox.TenantID || id != s.mailbox.ID {
		return nil, errors.New("wrong mailbox authority")
	}
	return &company.MailboxAccess{Mailbox: s.mailbox, CanRead: true}, nil
}

func (s *r5SSEReader) ListMailboxEvents(_ context.Context, tenant, mailbox uuid.UUID, cursor int64, limit int) ([]company.MailEvent, int64, error) {
	s.reads++
	s.cursors = append(s.cursors, cursor)
	if tenant != s.mailbox.TenantID || mailbox != s.mailbox.ID || limit != 200 {
		return nil, 0, errors.New("wrong event query")
	}
	return []company.MailEvent{{Sequence: 100, Type: "changed"}, {Sequence: 101, Type: "delete"}}, 101, nil
}

func (s *r5SSEReader) ListMonitorEvents(_ context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error) {
	s.reads++
	if pg.Page != 1 || pg.PerPage != 100 || eventType != "" || mailbox != "" || sender != "" {
		return nil, 0, errors.New("wrong monitor query")
	}
	return []*models.MonitorEvent{{ID: uuid.New(), Type: "message", Mailbox: "monitor@synthetic.test"}}, 1, nil
}

type r5SSEWriter struct {
	*httptest.ResponseRecorder
	mode                 string
	failed               bool
	writesAfterFailure   int
	flushesAfterFailure  int
	flushes              int
	deadlines            []time.Time
	deadlineAfterFailure int
	cancel               context.CancelFunc
	stopAtReady          bool
}

func (w *r5SSEWriter) SetWriteDeadline(deadline time.Time) error {
	if w.failed {
		w.deadlineAfterFailure++
	}
	w.deadlines = append(w.deadlines, deadline)
	if (w.mode == "deadline" && !deadline.IsZero()) || (w.mode == "deadline-reset" && deadline.IsZero()) {
		w.failed = true
		return errors.New("synthetic deadline failure")
	}
	return nil
}

func (w *r5SSEWriter) Write(data []byte) (int, error) {
	if w.failed {
		w.writesAfterFailure++
		return 0, errors.New("write after failed transport")
	}
	if w.mode == "write" || (w.mode == "id-write" && strings.Contains(string(data), "id: ")) {
		w.failed = true
		return 0, errors.New("synthetic write failure")
	}
	if w.mode == "short-write" {
		w.failed = true
		n := len(data) / 2
		_, _ = w.ResponseRecorder.Write(data[:n])
		return n, nil
	}
	n, err := w.ResponseRecorder.Write(data)
	if w.stopAtReady && strings.Contains(string(data), "event: ready") {
		w.cancel()
	}
	return n, err
}

// Override ResponseRecorder's StringWriter as well: the old shipping helper
// calls io.WriteString, which must observe the same controlled transport.
func (w *r5SSEWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func (w *r5SSEWriter) FlushError() error {
	if w.failed {
		w.flushesAfterFailure++
		return errors.New("flush after failed transport")
	}
	w.flushes++
	if w.mode == "flush" {
		w.failed = true
		return errors.New("synthetic flush failure")
	}
	w.ResponseRecorder.Flush()
	return nil
}

func (w *r5SSEWriter) Flush() { _ = w.FlushError() }

func r5SSEHandler(t *testing.T, consumer string, reader *r5SSEReader) (http.Handler, map[string]string) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	reader.mailbox = models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, FullAddress: "stream@synthetic.test"}
	refresh := func(r *http.Request) (*http.Request, error) {
		return middleware.RevalidateRequest(r, f.st, outboundTestJWTSecret, publicTenantIDForTests)
	}
	var handle http.HandlerFunc
	var headers map[string]string
	if consumer == "mailbox" {
		h := NewMailboxEventHandler(reader, refresh, zerolog.Nop())
		handle = func(w http.ResponseWriter, r *http.Request) {
			params := chi.NewRouteContext()
			params.URLParams.Add("id", reader.mailbox.ID.String())
			h.Events(w, r.WithContext(withRouteContext(r, params)))
		}
		headers = outboundUserHeaders(t, f.userA)
	} else {
		h := NewMonitorHandler(reader, nil, zerolog.Nop())
		h.SetStreamRevalidator(refresh)
		handle = h.StreamAll
		headers = outboundUserHeaders(t, f.platformAdmin)
	}
	return middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests)(handle), headers
}

func TestR5SSETransportFailureStopsConsumers(t *testing.T) {
	for _, consumer := range []string{"mailbox", "monitor"} {
		for _, mode := range []string{"deadline", "write", "short-write", "flush", "deadline-reset"} {
			t.Run(consumer+"/"+mode, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					reader := &r5SSEReader{}
					h, headers := r5SSEHandler(t, consumer, reader)
					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
					defer cancel()
					req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
					for key, value := range headers {
						req.Header.Set(key, value)
					}
					w := &r5SSEWriter{ResponseRecorder: httptest.NewRecorder(), mode: mode}
					h.ServeHTTP(w, req)
					if !w.failed || ctx.Err() != nil || reader.reads != 1 || w.writesAfterFailure != 0 || w.flushesAfterFailure != 0 {
						t.Fatalf("failed stream continued: failure=%v context=%v reads=%d laterWrites=%d laterFlushes=%d body=%q", w.failed, ctx.Err(), reader.reads, w.writesAfterFailure, w.flushesAfterFailure, w.Body.String())
					}
					if mode == "deadline" && w.Body.Len() != 0 {
						t.Fatal("deadline failure released stream bytes")
					}
				})
			})
		}
	}
	t.Run("mailbox/id-write", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			reader := &r5SSEReader{}
			h, headers := r5SSEHandler(t, "mailbox", reader)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			w := &r5SSEWriter{ResponseRecorder: httptest.NewRecorder(), mode: "id-write"}
			h.ServeHTTP(w, req)
			if !w.failed || ctx.Err() != nil || reader.reads != 1 || w.writesAfterFailure != 0 || w.flushesAfterFailure != 0 {
				t.Fatalf("failed cursor write continued: failure=%v context=%v reads=%d laterWrites=%d laterFlushes=%d", w.failed, ctx.Err(), reader.reads, w.writesAfterFailure, w.flushesAfterFailure)
			}
		})
	})
}

func TestR5SSETransportSuccessfulFrames(t *testing.T) {
	for _, consumer := range []string{"mailbox", "monitor"} {
		t.Run(consumer, func(t *testing.T) {
			reader := &r5SSEReader{}
			h, headers := r5SSEHandler(t, consumer, reader)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
			req.Header.Set("Last-Event-ID", "99")
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			w := &r5SSEWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, stopAtReady: true}
			h.ServeHTTP(w, req)
			body := w.Body.String()
			if reader.reads != 1 || !strings.Contains(body, "event: ready\ndata: ") || !strings.Contains(body, "event: resync\ndata: ") {
				t.Fatalf("normal stream lost handshake: reads=%d body=%q", reader.reads, body)
			}
			if consumer == "mailbox" && (len(reader.cursors) != 1 || reader.cursors[0] != 99 || !strings.Contains(body, "id: 100\nevent: changed\ndata: ") || !strings.Contains(body, "id: 101\nevent: delete\ndata: ") || !strings.Contains(body, `"cursor":101`)) {
				t.Fatal("mailbox sequence/cursor changed", body)
			}
			if len(w.deadlines) == 0 || !w.deadlines[len(w.deadlines)-1].IsZero() {
				t.Fatal("successful frame retained a write deadline during idle")
			}
			for _, deadline := range w.deadlines {
				if !deadline.IsZero() && time.Until(deadline) > 35100*time.Millisecond {
					t.Fatal("write budget exceeded existing 35-second limit")
				}
			}
		})
	}
}

func TestR5SSETransportRealHTTP(t *testing.T) {
	for _, consumer := range []string{"mailbox", "monitor"} {
		for _, h2 := range []bool{false, true} {
			protocol := "http1"
			if h2 {
				protocol = "http2"
			}
			t.Run(consumer+"/"+protocol, func(t *testing.T) {
				reader := &r5SSEReader{}
				h, headers := r5SSEHandler(t, consumer, reader)
				done := make(chan struct{})
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(done)
					h.ServeHTTP(w, r)
				}))
				server.EnableHTTP2 = h2
				server.StartTLS()
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
				if err != nil {
					t.Fatal(err)
				}
				for key, value := range headers {
					req.Header.Set(key, value)
				}
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK || response.ProtoMajor != map[bool]int{false: 1, true: 2}[h2] {
					t.Fatalf("unexpected HTTP stream: %s %s", response.Status, response.Proto)
				}
				scanner := bufio.NewScanner(response.Body)
				ready := false
				for scanner.Scan() {
					if scanner.Text() == "event: ready" {
						ready = true
						break
					}
				}
				if !ready {
					t.Fatalf("no real ready frame: %v", scanner.Err())
				}
				cancel()
				_ = response.Body.Close()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("client disconnect retained stream handler")
				}
			})
		}
	}
}
