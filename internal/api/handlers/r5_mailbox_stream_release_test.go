package handlers

import (
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

// The real HTTP authenticator and stream revalidator remain in the path. Only
// the durable event read/current mailbox port and transport flush are controlled.
type r5MailboxReleaseReader struct {
	mailbox   models.Mailbox
	allowed   bool
	missing   bool
	getErr    error
	events    []company.MailEvent
	reads     int
	cursors   []int64
	afterRead func()
}

func (s *r5MailboxReleaseReader) GetWorkMailbox(_ context.Context, _ authz.Actor, _ uuid.UUID) (*company.MailboxAccess, error) {
	if s.missing || s.getErr != nil {
		return nil, s.getErr
	}
	return &company.MailboxAccess{Mailbox: s.mailbox, CanRead: s.allowed, CanManage: true}, nil
}

func (s *r5MailboxReleaseReader) ListMailboxEvents(_ context.Context, tenant, mailbox uuid.UUID, cursor int64, limit int) ([]company.MailEvent, int64, error) {
	if tenant != s.mailbox.TenantID || mailbox != s.mailbox.ID || limit != 200 {
		return nil, cursor, errors.New("wrong stream query scope")
	}
	s.reads++
	s.cursors = append(s.cursors, cursor)
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.events, 101, nil
}

type r5MailboxReleaseWriter struct {
	*r5SSEWriter
	afterFlush func()
}

func (w *r5MailboxReleaseWriter) FlushError() error {
	if err := w.r5SSEWriter.FlushError(); err != nil {
		return err
	}
	if w.afterFlush != nil {
		w.afterFlush()
	}
	return nil
}

func r5RunMailboxRelease(t *testing.T, f outboundAccessFixture, reader *r5MailboxReleaseReader, ctx context.Context, w *r5MailboxReleaseWriter) {
	t.Helper()
	revalidate := func(r *http.Request) (*http.Request, error) {
		return middleware.RevalidateRequest(r, f.st, outboundTestJWTSecret, publicTenantIDForTests)
	}
	h := NewMailboxEventHandler(reader, revalidate, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	params := chi.NewRouteContext()
	params.URLParams.Add("id", reader.mailbox.ID.String())
	req = req.WithContext(withRouteContext(req, params))
	for name, value := range outboundUserHeaders(t, f.userA) {
		req.Header.Set(name, value)
	}
	req.Header.Set("Last-Event-ID", "99")
	middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(h.Events)).ServeHTTP(w, req)
}

func r5MailboxReleaseFixture(t *testing.T) (outboundAccessFixture, *r5MailboxReleaseReader) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	message := uuid.New()
	reader := &r5MailboxReleaseReader{
		mailbox: models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, FullAddress: "release@synthetic.test"},
		allowed: true,
		events:  []company.MailEvent{{Sequence: 100, Type: "changed", MessageID: &message}, {Sequence: 101, Type: "delete", MessageID: &message}},
	}
	return f, reader
}

func r5ChangeStreamAuthority(t *testing.T, reason string, f outboundAccessFixture, reader *r5MailboxReleaseReader, cancel context.CancelFunc) {
	t.Helper()
	switch reason {
	case "grant revoked":
		reader.allowed = false // administrative metadata access does not grant read
	case "mailbox removed":
		reader.missing = true
	case "wrong mailbox":
		reader.mailbox.ID = uuid.New()
	case "wrong tenant":
		reader.mailbox.TenantID = f.otherTenantID
	case "authority read fails":
		reader.getErr = errors.New("synthetic current authority lookup failed")
	case "employee frozen", "session version changed":
		u, err := f.st.GetUser(context.Background(), f.userA.ID)
		if err != nil || u == nil {
			t.Fatal("get current employee", err)
		}
		if reason == "employee frozen" {
			u.IsActive = false
		} else {
			u.SessionVersion++
		}
		if err := f.st.UpdateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	case "request canceled":
		cancel()
	case "allowed":
	default:
		t.Fatal("unknown authority mutation", reason)
	}
}

func TestR5MailboxStreamRechecksAfterEventRead(t *testing.T) {
	for _, reason := range []string{"grant revoked", "mailbox removed", "wrong mailbox", "wrong tenant", "authority read fails", "employee frozen", "session version changed", "request canceled", "allowed"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f, reader := r5MailboxReleaseFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				reader.afterRead = func() { r5ChangeStreamAuthority(t, reason, f, reader, cancel) }
				w := &r5MailboxReleaseWriter{r5SSEWriter: &r5SSEWriter{ResponseRecorder: httptest.NewRecorder()}}
				w.afterFlush = func() {
					if w.flushes == 4 {
						cancel() // stop only after both event and both control frames
					}
				}
				r5RunMailboxRelease(t, f, reader, ctx, w)
				if reader.reads != 1 || len(reader.cursors) != 1 || reader.cursors[0] != 99 {
					t.Fatalf("query scope/cursor changed: reads=%d cursors=%v", reader.reads, reader.cursors)
				}
				if reason != "allowed" {
					if w.Body.Len() != 0 || w.flushes != 0 {
						t.Fatalf("authority changed during event read released frames: flushes=%d body=%q", w.flushes, w.Body.String())
					}
					return
				}
				body := w.Body.String()
				for _, frame := range []string{"id: 100\nevent: changed\n", "id: 101\nevent: delete\n", "event: ready\n", "event: resync\n", `"cursor":101`} {
					if !strings.Contains(body, frame) {
						t.Fatalf("authorized stream lost frame %q: %q", frame, body)
					}
				}
				if w.flushes != 4 || len(w.deadlines) != 8 || !w.deadlines[len(w.deadlines)-1].IsZero() {
					t.Fatalf("authorized frame/idle deadline contract changed: flushes=%d deadlines=%v", w.flushes, w.deadlines)
				}
			})
		})
	}
}

func TestR5MailboxStreamRechecksBetweenFrames(t *testing.T) {
	for _, after := range []int{1, 2, 3} {
		for _, reason := range []string{"grant revoked", "authority read fails", "employee frozen", "session version changed", "request canceled"} {
			t.Run(reason+"/"+[]string{"", "first event", "second event", "ready"}[after], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f, reader := r5MailboxReleaseFixture(t)
					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
					defer cancel()
					w := &r5MailboxReleaseWriter{r5SSEWriter: &r5SSEWriter{ResponseRecorder: httptest.NewRecorder()}}
					w.afterFlush = func() {
						if w.flushes == after {
							r5ChangeStreamAuthority(t, reason, f, reader, cancel)
						}
						if w.flushes == 4 {
							cancel()
						}
					}
					r5RunMailboxRelease(t, f, reader, ctx, w)
					if w.flushes != after || reader.reads != 1 {
						t.Fatalf("revoked stream continued after frame %d: flushes=%d reads=%d body=%q", after, w.flushes, reader.reads, w.Body.String())
					}
				})
			})
		}
	}
}

func TestR5MailboxStreamRechecksEmptyReadBeforeReady(t *testing.T) {
	for _, reason := range []string{"grant revoked", "employee frozen", "request canceled"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f, reader := r5MailboxReleaseFixture(t)
				reader.events = nil
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				reader.afterRead = func() { r5ChangeStreamAuthority(t, reason, f, reader, cancel) }
				w := &r5MailboxReleaseWriter{r5SSEWriter: &r5SSEWriter{ResponseRecorder: httptest.NewRecorder()}}
				w.afterFlush = func() {
					if w.flushes == 2 {
						cancel()
					}
				}
				r5RunMailboxRelease(t, f, reader, ctx, w)
				if w.Body.Len() != 0 || w.flushes != 0 || reader.reads != 1 {
					t.Fatalf("revoked empty read exposed ready/cursor/resync: flushes=%d reads=%d body=%q", w.flushes, reader.reads, w.Body.String())
				}
			})
		})
	}
}
