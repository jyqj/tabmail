package smtp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// The SMTP transport, session, policy cache, resolver and ingest service are
// production. Only the backing store is in-memory: a selected read waits for
// context cancellation, as a cooperative database call would. These tests do
// not claim PostgreSQL locking, remote delivery or crash durability.
type r5AdmissionGate struct {
	entered chan context.Context
	exited  chan error
	release chan struct{}
	once    sync.Once
	used    atomic.Bool
}

func r5NewAdmissionGate() *r5AdmissionGate {
	return &r5AdmissionGate{entered: make(chan context.Context, 1), exited: make(chan error, 1), release: make(chan struct{})}
}

func (g *r5AdmissionGate) open() { g.once.Do(func() { close(g.release) }) }

func (g *r5AdmissionGate) wait(ctx context.Context) error {
	if !g.used.CompareAndSwap(false, true) {
		return nil
	}
	g.entered <- ctx
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-g.release:
	}
	g.exited <- err
	return err
}

type r5AdmissionCall struct {
	name string
	ctx  context.Context
}

type r5AdmissionStore struct {
	*testutil.FakeStore
	mu       sync.Mutex
	gateName string
	gate     *r5AdmissionGate
	calls    []r5AdmissionCall
}

func (s *r5AdmissionStore) arm(name string) *r5AdmissionGate {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gateName, s.gate = name, r5NewAdmissionGate()
	s.calls = nil
	return s.gate
}

func (s *r5AdmissionStore) observe(ctx context.Context, name string) error {
	s.mu.Lock()
	s.calls = append(s.calls, r5AdmissionCall{name: name, ctx: ctx})
	gate, target := s.gate, s.gateName
	s.mu.Unlock()
	if gate != nil && name == target {
		return gate.wait(ctx)
	}
	return nil
}

func (s *r5AdmissionStore) snapshot() []r5AdmissionCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]r5AdmissionCall(nil), s.calls...)
}

func (s *r5AdmissionStore) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	if err := s.observe(ctx, "policy"); err != nil {
		return nil, err
	}
	return s.FakeStore.GetSMTPPolicy(ctx)
}

func (s *r5AdmissionStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	if err := s.observe(ctx, "zone"); err != nil {
		return nil, err
	}
	return s.FakeStore.GetZoneByDomain(ctx, domain)
}

func (s *r5AdmissionStore) GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error) {
	if err := s.observe(ctx, "mailbox"); err != nil {
		return nil, err
	}
	return s.FakeStore.GetMailboxByAddress(ctx, address)
}

func (s *r5AdmissionStore) ListRoutes(ctx context.Context, zone uuid.UUID) ([]*models.DomainRoute, error) {
	if err := s.observe(ctx, "routes"); err != nil {
		return nil, err
	}
	return s.FakeStore.ListRoutes(ctx, zone)
}

func (s *r5AdmissionStore) CreateIngress(ctx context.Context, job *models.IngestJob, targets []store.IngressTarget, hash string, size int64) error {
	if err := s.observe(ctx, "ingress"); err != nil {
		return err
	}
	return s.FakeStore.CreateIngress(ctx, job, targets, hash, size)
}

type r5AdmissionFixture struct {
	st  *r5AdmissionStore
	obj *testutil.MemoryObjectStore
	svc *ingest.Service
	srv *Server
}

func r5NewAdmissionFixture(timeout time.Duration, provisioned bool) *r5AdmissionFixture {
	st := &r5AdmissionStore{FakeStore: testutil.NewFakeStore()}
	plan, tenant, zone := uuid.New(), uuid.New(), uuid.New()
	st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: 4096, MaxMessagesPerMailbox: 100, RetentionHours: 24})
	st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
	st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "admission.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: zone, RouteType: models.RouteExact, MatchValue: "admission.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})
	if provisioned {
		st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@admission.test", LocalPart: "reader", ResolvedDomain: "admission.test", AccessMode: models.AccessPublic})
	}
	obj := testutil.NewMemoryObjectStore()
	rv := resolver.New(st, policy.NamingFull, true)
	svc := ingest.NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	srv := NewServer(config.SMTP{Domain: "mx.admission.test", Timeout: timeout, MaxMessageBytes: 4096, MaxRecipients: 10}, svc, rv, zerolog.Nop())
	return &r5AdmissionFixture{st: st, obj: obj, svc: svc, srv: srv}
}

func (f *r5AdmissionFixture) open(t *testing.T) (net.Conn, *bufio.Reader) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.srv.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
	started := r5Start(f.srv, context.Background())
	r5Await(t, f.srv.ready)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.srv.Shutdown(ctx); err != nil {
			t.Errorf("admission fixture shutdown: %v", err)
		}
		if err := r5Result(t, started); err != nil {
			t.Errorf("admission fixture Serve: %v", err)
		}
	})
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	r5CloudWire(t, conn, r, "", "220")
	r5CloudWire(t, conn, r, "EHLO fixture.admission.test", "250")
	return conn, r
}

var r5AdmissionStages = []struct{ command, query string }{
	{"MAIL", "policy"}, {"RCPT", "policy"}, {"RCPT", "zone"}, {"RCPT", "mailbox"}, {"RCPT", "routes"},
}

func r5AdmissionPrepare(t *testing.T, f *r5AdmissionFixture, c net.Conn, r *bufio.Reader, command string) string {
	t.Helper()
	if command == "MAIL" {
		return "MAIL FROM:<sender@example.test>"
	}
	r5CloudWire(t, c, r, "MAIL FROM:<sender@example.test>", "250")
	f.svc.InvalidatePolicy() // RCPT's policy read must actually reach the store.
	return "RCPT TO:<reader@admission.test>"
}

func r5AdmissionEntered(t *testing.T, g *r5AdmissionGate) context.Context {
	t.Helper()
	select {
	case ctx := <-g.entered:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("production admission did not reach the selected store read")
		return nil
	}
}

func r5AdmissionSend(t *testing.T, c net.Conn, command string) {
	t.Helper()
	if _, err := io.WriteString(c, command+"\r\n"); err != nil {
		t.Fatal(err)
	}
}

func r5AdmissionRead(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, want+" ") {
		t.Errorf("SMTP response=%q want=%s", line, want)
	}
}

func (f *r5AdmissionFixture) queued(t *testing.T, want int) {
	t.Helper()
	jobs, count, err := f.st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 10}, "", "smtp", "reader@admission.test")
	if err != nil || count != want || len(jobs) != want || f.obj.Count() != want {
		t.Fatalf("ingress effects: rows=%d jobs=%d objects=%d error=%v; want=%d", count, len(jobs), f.obj.Count(), err, want)
	}
	for _, job := range jobs {
		if job.State != "pending" || job.MailFrom != "sender@example.test" || len(job.Recipients) != 1 || job.Recipients[0] != "reader@admission.test" {
			t.Errorf("unexpected accepted ingress: %+v", job)
		}
	}
}

func TestR5SMTPAdmissionCommandTimeout(t *testing.T) {
	for _, stage := range r5AdmissionStages {
		t.Run(stage.command+"_"+stage.query, func(t *testing.T) {
			const budget = 100 * time.Millisecond
			f := r5NewAdmissionFixture(budget, stage.query != "routes")
			c, r := f.open(t)
			command := r5AdmissionPrepare(t, f, c, r, stage.command)
			gate := f.st.arm(stage.query)
			defer gate.open() // A failing old implementation still has a real join.
			sent := time.Now()
			r5AdmissionSend(t, c, command)
			ctx := r5AdmissionEntered(t, gate)
			deadline, bounded := ctx.Deadline()
			if !bounded || deadline.Before(sent.Add(budget)) || deadline.After(time.Now().Add(budget)) {
				t.Errorf("in-flight %s read lacks configured command deadline: bounded=%v deadline=%v", stage.query, bounded, deadline)
			}
			// Every preceding lookup in this RCPT shares the original absolute
			// budget. A new timeout for each resolver read would extend admission.
			for _, call := range f.st.snapshot() {
				got, ok := call.ctx.Deadline()
				if !ok || !got.Equal(deadline) {
					t.Errorf("%s renewed or lost command deadline: bounded=%v deadline=%v", call.name, ok, got)
				}
			}
			var result error
			select {
			case result = <-gate.exited:
			case <-time.After(4*budget + 250*time.Millisecond):
				t.Errorf("cooperative %s read remained in flight beyond the SMTP command budget", stage.query)
				gate.open()
				result = r5Result(t, gate.exited)
			}
			if !errors.Is(result, context.DeadlineExceeded) {
				t.Errorf("admission read returned %v; want its command deadline", result)
			}
			r5AdmissionRead(t, r, "451")
			f.queued(t, 0)
			// A failed read must not cache an error or poison the connection's
			// next transaction. Complete a fresh real SMTP transaction afterward.
			r5CloudWire(t, c, r, "RSET", "250")
			r5CloudWire(t, c, r, "MAIL FROM:<sender@example.test>", "250")
			r5CloudWire(t, c, r, "RCPT TO:<reader@admission.test>", "250")
			r5CloudWire(t, c, r, "DATA", "354")
			r5CloudWire(t, c, r, "Subject: after timeout\r\n\r\naccepted after recovery\r\n.", "250")
			f.queued(t, 1)
		})
	}
}

func TestR5SMTPAdmissionShortShutdownCancelsRead(t *testing.T) {
	for _, stage := range r5AdmissionStages {
		t.Run(stage.command+"_"+stage.query, func(t *testing.T) {
			f := r5NewAdmissionFixture(time.Minute, stage.query != "routes")
			c, r := f.open(t)
			command := r5AdmissionPrepare(t, f, c, r, stage.command)
			gate := f.st.arm(stage.query)
			defer gate.open()
			r5AdmissionSend(t, c, command)
			queryCtx := r5AdmissionEntered(t, gate)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if err := f.srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if err := r5Result(t, gate.exited); !errors.Is(err, context.Canceled) {
				t.Errorf("short shutdown did not cancel the in-flight lookup: %v", err)
			}
			if !errors.Is(queryCtx.Err(), context.Canceled) || !errors.Is(f.srv.backend.ctx.Err(), context.Canceled) {
				t.Errorf("command did not inherit work-owner cancellation: query=%v owner=%v", queryCtx.Err(), f.srv.backend.ctx.Err())
			}
			joinCtx, joinCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer joinCancel()
			if err := f.srv.Shutdown(joinCtx); err != nil {
				t.Fatal(err)
			}
			f.queued(t, 0)
		})
	}
}

func TestR5SMTPAdmissionShortParentDeadline(t *testing.T) {
	for _, stage := range r5AdmissionStages[:3] {
		t.Run(stage.command+"_"+stage.query, func(t *testing.T) {
			// Direct backend construction verifies an already-shorter parent
			// deadline as well as the real Server.Shutdown cancellation above.
			f := r5NewAdmissionFixture(time.Minute, true)
			parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			f.srv.backend.ctx = parent
			sess := &session{backend: f.srv.backend, logger: zerolog.Nop()}
			gate := f.st.arm(stage.query)
			defer gate.open()
			done := make(chan error, 1)
			go func() {
				if stage.command == "MAIL" {
					done <- sess.Mail("sender@example.test", nil)
				} else {
					done <- sess.Rcpt("reader@admission.test", nil)
				}
			}()
			ctx := r5AdmissionEntered(t, gate)
			got, ok := ctx.Deadline()
			want, _ := parent.Deadline()
			if !ok || !got.Equal(want) {
				t.Errorf("command extended parent deadline: got=%v bounded=%v want=%v", got, ok, want)
			}
			if err := r5Result(t, gate.exited); !errors.Is(err, context.DeadlineExceeded) {
				t.Error(err)
			}
			var reply *gosmtp.SMTPError
			if err := r5Result(t, done); !errors.As(err, &reply) || reply.Code != 451 {
				t.Errorf("parent deadline command response=%v; want SMTP451", err)
			}
			f.srv.backend.sealAndWait()
			f.queued(t, 0)
		})
	}
}

func TestR5SMTPAdmissionFreshBudgetAndRelease(t *testing.T) {
	f := r5NewAdmissionFixture(time.Second, true)
	c, r := f.open(t)
	r5CloudWire(t, c, r, "MAIL FROM:<sender@example.test>", "250")
	mailCalls := f.st.snapshot()
	if len(mailCalls) != 1 || mailCalls[0].name != "policy" {
		t.Fatalf("MAIL did not perform the expected policy lookup: %+v", mailCalls)
	}
	mailDeadline, mailBounded := mailCalls[0].ctx.Deadline()
	if !mailBounded || !errors.Is(mailCalls[0].ctx.Err(), context.Canceled) {
		t.Errorf("successful MAIL did not release its bounded context: bounded=%v error=%v", mailBounded, mailCalls[0].ctx.Err())
	}
	f.svc.InvalidatePolicy()
	r5CloudWire(t, c, r, "RCPT TO:<reader@admission.test>", "250")
	allCalls := f.st.snapshot()
	if len(allCalls) != 4 {
		t.Fatalf("RCPT did not perform policy, zone and mailbox reads: %+v", allCalls)
	}
	rcptDeadline, rcptBounded := allCalls[1].ctx.Deadline()
	if !rcptBounded || !rcptDeadline.After(mailDeadline) {
		t.Errorf("RCPT did not receive a fresh command budget: MAIL=%v RCPT=%v bounded=%v", mailDeadline, rcptDeadline, rcptBounded)
	}
	for _, call := range allCalls[1:] {
		deadline, ok := call.ctx.Deadline()
		if !ok || !deadline.Equal(rcptDeadline) || !errors.Is(call.ctx.Err(), context.Canceled) {
			t.Errorf("successful RCPT %s budget/release: bounded=%v deadline=%v error=%v", call.name, ok, deadline, call.ctx.Err())
		}
	}
	if f.srv.backend.ctx.Err() != nil {
		t.Fatal("completed admission cancelled the server's DATA owner")
	}
	r5CloudWire(t, c, r, "DATA", "354")
	r5CloudWire(t, c, r, "Subject: ordinary\r\n\r\nnormal acceptance\r\n.", "250")
	f.queued(t, 1)
}

func TestR5SMTPAdmissionStandaloneDefaultIsFinite(t *testing.T) {
	// Config loading rejects nonpositive timeouts. Direct NewServer callers
	// still receive the existing 300s default instead of an unbounded read.
	f := r5NewAdmissionFixture(0, true)
	c, r := f.open(t)
	gate := f.st.arm("policy")
	defer gate.open()
	sent := time.Now()
	r5AdmissionSend(t, c, "MAIL FROM:<sender@example.test>")
	ctx := r5AdmissionEntered(t, gate)
	deadline, bounded := ctx.Deadline()
	if !bounded || deadline.Before(sent.Add(300*time.Second)) || deadline.After(time.Now().Add(300*time.Second)) {
		t.Errorf("standalone admission lost finite default: bounded=%v deadline=%v", bounded, deadline)
	}
	gate.open()
	if err := r5Result(t, gate.exited); err != nil {
		t.Fatal(err)
	}
	r5AdmissionRead(t, r, "250")
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("standalone successful command retained its context: %v", ctx.Err())
	}
}

func TestR5SMTPAdmissionDATAKeepsOwnedLifetime(t *testing.T) {
	const budget = 100 * time.Millisecond
	f := r5NewAdmissionFixture(budget, true)
	c, r := f.open(t)
	r5CloudWire(t, c, r, "MAIL FROM:<sender@example.test>", "250")
	r5CloudWire(t, c, r, "RCPT TO:<reader@admission.test>", "250")
	gate := f.st.arm("ingress")
	defer gate.open()
	r5CloudWire(t, c, r, "DATA", "354")
	r5AdmissionSend(t, c, "Subject: durable owner\r\n\r\nread before admission expires\r\n.")
	ctx := r5AdmissionEntered(t, gate)
	// Positive timer completion establishes that DATA's actual store call
	// has remained in flight longer than an admission command may run.
	timer := time.NewTimer(2 * budget)
	defer timer.Stop()
	select {
	case err := <-gate.exited:
		t.Fatalf("DATA inherited an admission timeout before commit: %v", err)
	case <-timer.C:
	}
	if ctx != f.srv.backend.ctx || ctx.Err() != nil {
		t.Fatalf("DATA lost its original work owner: ctx=%v owner=%v", ctx.Err(), f.srv.backend.ctx.Err())
	}
	if n, err := f.st.CountIngestJobsByState(context.Background()); err != nil || n != 0 || f.obj.Count() != 1 {
		t.Fatalf("DATA did not reach the real pre-commit boundary: jobs=%d objects=%d error=%v", n, f.obj.Count(), err)
	}
	gate.open()
	if err := r5Result(t, gate.exited); err != nil {
		t.Fatal(err)
	}
	r5AdmissionRead(t, r, "250")
	f.queued(t, 1)
}
