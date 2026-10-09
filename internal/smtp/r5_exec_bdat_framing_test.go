package smtp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// These tests exercise the actual TCP listener, local go-smtp fork, session,
// ingest service and both acceptance modes. Only persistence is in-memory.
type execFramingFixture struct {
	st      *testutil.FakeStore
	obj     *testutil.MemoryObjectStore
	mailbox *models.Mailbox
	conn    *net.TCPConn
	reader  *bufio.Reader
	server  *Server
	started <-chan error
	once    sync.Once
	durable bool
}

func newExecFramingFixture(t *testing.T, durable bool, maxBytes int64) *execFramingFixture {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()
	tenant, plan, zone := uuid.New(), uuid.New(), uuid.New()
	st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: int(maxBytes), MaxMessagesPerMailbox: 100, RetentionHours: 24})
	st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
	st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "framing.test", IsVerified: true, MXVerified: true})
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@framing.test", LocalPart: "reader", ResolvedDomain: "framing.test", AccessMode: models.AccessPublic}
	st.SeedMailbox(mb)
	rv := resolver.New(st, policy.NamingFull, true)
	svc := ingest.NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: durable}, zerolog.Nop())
	s := NewServer(config.SMTP{Domain: "mx.framing.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: int(maxBytes)}, svc, rv, zerolog.Nop())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
	f := &execFramingFixture{st: st, obj: obj, mailbox: mb, server: s, started: r5Start(s, context.Background()), durable: durable}
	t.Cleanup(func() { f.finish(t) })
	r5Await(t, s.ready)
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f.conn = conn.(*net.TCPConn)
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	f.reader = bufio.NewReader(conn)
	f.expect(t, "", 220)
	if reply := f.expect(t, "EHLO client.framing.test\r\n", 250); !strings.Contains(reply, "CHUNKING") {
		t.Fatal("production SMTP server did not advertise CHUNKING")
	}
	f.begin(t)
	return f
}

func (f *execFramingFixture) begin(t *testing.T) {
	t.Helper()
	f.expect(t, "MAIL FROM:<sender@example.test>\r\n", 250)
	f.expect(t, "RCPT TO:<reader@framing.test>\r\n", 250)
}

func (f *execFramingFixture) reply(t *testing.T, write string) (int, string) {
	t.Helper()
	if write != "" {
		if _, err := io.WriteString(f.conn, write); err != nil {
			t.Fatal(err)
		}
	}
	var response strings.Builder
	for {
		line, err := f.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SMTP reply: %v (partial %q)", err, line)
		}
		response.WriteString(line)
		if len(line) < 4 {
			t.Fatalf("short SMTP reply %q", line)
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil {
			t.Fatal(err)
		}
		if line[3] != '-' {
			return code, response.String()
		}
	}
}

func (f *execFramingFixture) expect(t *testing.T, write string, want int) string {
	t.Helper()
	code, response := f.reply(t, write)
	if code != want {
		t.Fatalf("SMTP reply = %q, want %d", response, want)
	}
	return response
}

func (f *execFramingFixture) finish(t *testing.T) {
	t.Helper()
	f.once.Do(func() {
		if f.conn != nil {
			_ = f.conn.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.server.Shutdown(ctx); err != nil {
			t.Errorf("SMTP framing owner did not drain: %v", err)
		}
		if err := r5Result(t, f.started); err != nil {
			t.Errorf("SMTP framing Serve returned: %v", err)
		}
	})
}

func (f *execFramingFixture) originals(t *testing.T, want ...[]byte) {
	t.Helper()
	ctx := context.Background()
	var keys []string
	if f.durable {
		jobs, total, err := f.st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 20}, "", "", "")
		if err != nil || total != len(want) || len(jobs) != len(want) {
			t.Errorf("durable receipt effects: total=%d jobs=%d err=%v, want %d", total, len(jobs), err, len(want))
		}
		for _, job := range jobs {
			keys = append(keys, job.RawObjectKey)
		}
	} else {
		messages, total, err := f.st.ListMessages(ctx, f.mailbox.ID, models.Page{Page: 1, PerPage: 20})
		if err != nil || total != len(want) || len(messages) != len(want) {
			t.Errorf("message effects: total=%d rows=%d err=%v, want %d", total, len(messages), err, len(want))
		}
		for _, message := range messages {
			keys = append(keys, message.RawObjectKey)
		}
	}
	if f.obj.Count() != len(want) {
		t.Errorf("object effects: %d, want %d", f.obj.Count(), len(want))
	}
	unmatched := append([][]byte(nil), want...)
	for _, key := range keys {
		r, err := f.obj.Get(ctx, key)
		if err != nil {
			t.Error(err)
			continue
		}
		actual, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil || closeErr != nil {
			t.Errorf("read stored original: %v / %v", err, closeErr)
		}
		matched := false
		for i, expected := range unmatched {
			if bytes.Equal(actual, expected) {
				unmatched = append(unmatched[:i], unmatched[i+1:]...)
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("unexpected or truncated persisted original: %q", actual)
		}
	}
	if len(unmatched) != 0 {
		t.Errorf("missing %d complete originals", len(unmatched))
	}
}

func TestExecBDATTruncatedChunkNeverAccepted(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, name := range []string{"empty-last", "short-last", "short-non-last", "short-second-last"} {
			t.Run(fmt.Sprintf("durable=%t/%s", durable, name), func(t *testing.T) {
				f := newExecFramingFixture(t, durable, 4096)
				partial := "Subject: short chunk\r\n\r\nunfinished body"
				if name == "empty-last" {
					partial = ""
				}
				if name == "short-second-last" {
					prefix := "Subject: earlier complete chunk\r\n\r\n"
					f.expect(t, fmt.Sprintf("BDAT %d\r\n%s", len(prefix), prefix), 250)
				}
				last := " LAST"
				if name == "short-non-last" {
					last = ""
				}
				if _, err := fmt.Fprintf(f.conn, "BDAT %d%s\r\n%s", len(partial)+7, last, partial); err != nil {
					t.Fatal(err)
				}
				// EOF is observable to the server while its reply remains readable.
				if err := f.conn.CloseWrite(); err != nil {
					t.Fatal(err)
				}
				code, response := f.reply(t, "")
				if code < 400 || code >= 600 {
					t.Errorf("truncated BDAT was acknowledged: %q", response)
				}
				f.finish(t)
				f.originals(t)
			})
		}
	}
}

func TestExecBDATCompleteChunksPreserveOriginalAndNextCommand(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, name := range []string{"single", "empty-last", "multiple", "empty-first", "binary-body"} {
			t.Run(fmt.Sprintf("durable=%t/%s", durable, name), func(t *testing.T) {
				f := newExecFramingFixture(t, durable, 4096)
				raw := []byte("Subject: complete chunk\r\n\r\nbody\r\n.\r\n")
				if name == "binary-body" {
					raw = append(raw, 0, 255, 13, 10)
				}
				var chunks [][]byte
				switch name {
				case "empty-last":
					chunks = [][]byte{raw, {}}
				case "multiple":
					chunks = [][]byte{raw[:7], raw[7:25], raw[25:]}
				case "empty-first":
					chunks = [][]byte{{}, raw}
				default:
					chunks = [][]byte{raw}
				}
				for i, chunk := range chunks {
					last := ""
					if i == len(chunks)-1 {
						last = " LAST"
					}
					f.expect(t, fmt.Sprintf("BDAT %d%s\r\n%s", len(chunk), last, chunk), 250)
				}
				f.expect(t, "NOOP\r\n", 250)
				f.begin(t)
				next := []byte("Subject: next transaction\r\n\r\nsecond original\r\n")
				// Keep the next command in the same socket write as the exact chunk.
				f.expect(t, fmt.Sprintf("BDAT %d LAST\r\n%sNOOP\r\n", len(next), next), 250)
				f.expect(t, "", 250)
				f.finish(t)
				f.originals(t, raw, next)
			})
		}
	}
}
