package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/companymail"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Exercise the real content service, Auth middleware, and download handler.
// Only the repository and object reader are synthetic; these are not PG tests.
type sourceHTTPRepo struct {
	companymail.Repository
	message models.Message
	calls   atomic.Int64
	denyAt  int64
	denied  error
}

func (s *sourceHTTPRepo) GetWorkMessage(_ context.Context, a authz.Actor, mailbox, id uuid.UUID) (*models.Message, error) {
	n := s.calls.Add(1)
	if a.TenantID != s.message.TenantID || mailbox != s.message.MailboxID || id != s.message.ID {
		return nil, app.NotFound("message not found")
	}
	if n == s.denyAt {
		return nil, s.denied
	}
	m := s.message
	return &m, nil
}

type sourceHTTPStep struct {
	data []byte
	err  error
}
type sourceHTTPReader struct {
	steps  []sourceHTTPStep
	closes atomic.Int64
	reads  atomic.Int64
}

func (r *sourceHTTPReader) Read(p []byte) (int, error) {
	r.reads.Add(1)
	if len(r.steps) == 0 {
		return 0, io.EOF
	}
	step := &r.steps[0]
	n := copy(p, step.data)
	step.data = step.data[n:]
	if len(step.data) > 0 {
		return n, nil
	}
	err := step.err
	r.steps = r.steps[1:]
	return n, err
}
func (r *sourceHTTPReader) Close() error { r.closes.Add(1); return nil }

type sourceHTTPObjects struct {
	companymail.ObjectStore
	reader *sourceHTTPReader
	opens  atomic.Int64
}

func (s *sourceHTTPObjects) Get(context.Context, string) (io.ReadCloser, error) {
	s.opens.Add(1)
	return s.reader, nil
}
func sourceHTTPFixture(t *testing.T, steps []sourceHTTPStep) (*CompanyMailHandler, outboundAccessFixture, *sourceHTTPRepo, *sourceHTTPReader) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	repo := &sourceHTTPRepo{message: models.Message{ID: uuid.New(), TenantID: f.tenantID, MailboxID: uuid.New(), RawObjectKey: "synthetic/source"}}
	reader := &sourceHTTPReader{steps: steps}
	svc := companymail.NewService(repo, &sourceHTTPObjects{reader: reader})
	return NewCompanyMailHandler(nil, svc, nil, zerolog.Nop()), f, repo, reader
}
func sourceHTTPCall(t *testing.T, h *CompanyMailHandler, f outboundAccessFixture, repo *sourceHTTPRepo) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/company/mailboxes/" + repo.message.MailboxID.String() + "/messages/" + repo.message.ID.String() + "/source"
	return doOutboundHandlerRequest(t, f.st, h.Source, http.MethodGet, path, map[string]string{"id": repo.message.MailboxID.String(), "message": repo.message.ID.String()}, outboundUserHeaders(t, f.userA))
}
func TestCompanySourceHTTPFirstReadFailureIsNotSuccessfulDownload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		denied error
		status int
		code   string
	}{
		{"revoked", app.Forbidden("mailbox read permission required"), 403, "FORBIDDEN"},
		{"removed", app.NotFound("message not found"), 404, "NOT_FOUND"},
		{"source-changed", app.Conflict("message source changed; reload"), 409, "CONFLICT"},
		{"database", app.Internal(errors.New("private-fixture-db-detail")), 500, "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f, repo, reader := sourceHTTPFixture(t, []sourceHTTPStep{{[]byte("private-fixture-content"), nil}})
			repo.denyAt = 3
			repo.denied = tc.denied // B01-K's post-first-read guard.
			rr := sourceHTTPCall(t, h, f, repo)
			if rr.Code != tc.status {
				t.Errorf("status=%d want=%d", rr.Code, tc.status)
			}
			if !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") || rr.Header().Get("Content-Disposition") != "" {
				t.Error("failure committed download headers")
			}
			if !strings.Contains(rr.Body.String(), tc.code) {
				t.Error("missing structured application error")
			}
			if strings.Contains(rr.Body.String(), "private-fixture") {
				t.Error("private bytes or diagnostic leaked")
			}
			if reader.closes.Load() != 1 {
				t.Error("source not closed exactly once")
			}
		})
	}
}
func TestCompanySourceHTTPEarlyIOFailureAndNoProgress(t *testing.T) {
	for _, mode := range []string{"read-error", "bytes-and-error", "no-progress"} {
		t.Run(mode, func(t *testing.T) {
			steps := []sourceHTTPStep{{nil, errors.New("private-fixture-io-detail")}}
			if mode == "bytes-and-error" {
				steps[0].data = []byte("private-fixture-partial")
			}
			if mode == "no-progress" {
				steps = make([]sourceHTTPStep, 101)
			}
			h, f, repo, reader := sourceHTTPFixture(t, steps)
			rr := sourceHTTPCall(t, h, f, repo)
			if rr.Code != 500 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") {
				t.Errorf("I/O failure was not 500 JSON: %d", rr.Code)
			}
			if strings.Contains(rr.Body.String(), "private-fixture") || rr.Header().Get("Content-Disposition") != "" {
				t.Error("failed read released private data or download headers")
			}
			if mode == "no-progress" && reader.reads.Load() > 100 {
				t.Error("unbounded no-progress reads")
			}
			if reader.closes.Load() != 1 {
				t.Error("source not closed")
			}
		})
	}
}
func TestCompanySourceHTTPPreservesSuccessfulBytes(t *testing.T) {
	for _, mode := range []string{"empty", "one-byte", "data-and-eof", "large", "transient-zero"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte("From: fixture@example.test\r\n\r\nsynthetic body")
			if mode == "empty" {
				raw = nil
			}
			if mode == "one-byte" {
				raw = []byte("x")
			}
			if mode == "large" {
				raw = bytes.Repeat([]byte("fixture\r\n"), 100000)
			}
			step := sourceHTTPStep{data: append([]byte(nil), raw...)}
			if mode == "data-and-eof" {
				step.err = io.EOF
			}
			steps := []sourceHTTPStep{step}
			if mode == "transient-zero" {
				steps = append([]sourceHTTPStep{{}}, steps...)
			}
			h, f, repo, reader := sourceHTTPFixture(t, steps)
			rr := sourceHTTPCall(t, h, f, repo)
			if rr.Code != 200 || !bytes.Equal(rr.Body.Bytes(), raw) {
				t.Error("successful source bytes changed")
			}
			if rr.Header().Get("Content-Type") != "message/rfc822" || rr.Header().Get("Cache-Control") != "private, no-store" || rr.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(rr.Header().Get("Content-Disposition"), "attachment;") {
				t.Error("download protections missing")
			}
			if repo.calls.Load() > 3 {
				t.Error("authorization queries grew with chunks")
			}
			if reader.closes.Load() != 1 {
				t.Error("source not closed exactly once")
			}
		})
	}
}
func sourceHTTPServe(t *testing.T, h *CompanyMailHandler, f outboundAccessFixture, repo *sourceHTTPRepo, w http.ResponseWriter, ctx context.Context) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", repo.message.MailboxID.String())
	route.URLParams.Add("message", repo.message.ID.String())
	req = req.WithContext(withRouteContext(req, route))
	for k, v := range outboundUserHeaders(t, f.userA) {
		req.Header.Set(k, v)
	}
	middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(h.Source)).ServeHTTP(w, req)
}

type sourceHTTPFailWriter struct {
	header http.Header
	short  bool
}

func (w *sourceHTTPFailWriter) Header() http.Header { return w.header }
func (w *sourceHTTPFailWriter) WriteHeader(int)     {}
func (w *sourceHTTPFailWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, io.ErrClosedPipe
}
func TestCompanySourceHTTPAbortsFailedClientWrite(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "write-error", true: "short-write"}[short], func(t *testing.T) {
			h, f, repo, _ := sourceHTTPFixture(t, []sourceHTTPStep{{[]byte("fixture"), nil}})
			writer := &sourceHTTPFailWriter{header: make(http.Header), short: short}
			defer func() {
				if recover() != http.ErrAbortHandler {
					t.Error("failed download write did not abort")
				}
			}()
			sourceHTTPServe(t, h, f, repo, writer, context.Background())
		})
	}
}
func TestCompanySourceHTTPCancellationBeforeHeaders(t *testing.T) {
	h, f, repo, reader := sourceHTTPFixture(t, []sourceHTTPStep{{[]byte("private-fixture"), nil}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	sourceHTTPServe(t, h, f, repo, rr, ctx)
	if rr.Code == 200 || rr.Header().Get("Content-Disposition") != "" || reader.reads.Load() != 0 {
		t.Error("cancelled request started download")
	}
}

func TestCompanySourceHTTPNoProgressAfterFirstChunkAborts(t *testing.T) {
	steps := append([]sourceHTTPStep{{[]byte("fixture prefix"), nil}}, make([]sourceHTTPStep, 101)...)
	h, f, repo, reader := sourceHTTPFixture(t, steps)
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("stalled stream was not aborted")
		}
		if reader.reads.Load() > 101 {
			t.Error("stream made unbounded empty reads")
		}
		if reader.closes.Load() != 1 {
			t.Error("stalled source not closed exactly once")
		}
	}()
	sourceHTTPServe(t, h, f, repo, httptest.NewRecorder(), context.Background())
}

func TestCompanySourceHTTPLateReadFailureAbortsTransport(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		name := "http1"
		if http2 {
			name = "http2"
		}
		t.Run(name, func(t *testing.T) {
			prefix := bytes.Repeat([]byte("fixture"), 16000)
			h, f, repo, reader := sourceHTTPFixture(t, []sourceHTTPStep{{prefix, nil}, {nil, errors.New("private-fixture-late-error")}})
			router := chi.NewRouter()
			router.Use(chimw.Recoverer)
			router.Use(middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests))
			router.Get("/api/v1/company/mailboxes/{id}/messages/{message}/source", h.Source)
			server := httptest.NewUnstartedServer(router)
			server.EnableHTTP2 = http2
			if http2 {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/company/mailboxes/"+repo.message.MailboxID.String()+"/messages/"+repo.message.ID.String()+"/source", nil)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range outboundUserHeaders(t, f.userA) {
				req.Header.Set(k, v)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("fixture failed before headers: %v", err)
			}
			defer res.Body.Close()
			wantProto := 1
			if http2 {
				wantProto = 2
			}
			if res.ProtoMajor != wantProto {
				t.Fatalf("protocol=%d want=%d", res.ProtoMajor, wantProto)
			}
			raw, err := io.ReadAll(res.Body)
			if err == nil {
				t.Error("truncated EML looked like a complete successful download")
			}
			if bytes.Contains(raw, []byte("private-fixture-late-error")) || bytes.Contains(raw, []byte("INTERNAL_ERROR")) {
				t.Error("late failure appended diagnostic/JSON to mail")
			}
			server.Close()
			if reader.closes.Load() != 1 {
				t.Error("aborted source not closed exactly once")
			}
		})
	}
}
