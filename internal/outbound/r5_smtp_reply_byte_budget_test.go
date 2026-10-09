package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/store"
)

// Freeze the externally observed transport budget independently of the product
// constant, so this file also runs against the unbounded original source.
const r5SMTPReplyByteLimit = 1 << 20

type r5ReplyBudgetMode struct {
	name, tls string
	direct    bool
}

var r5ReplyBudgetModes = []r5ReplyBudgetMode{
	{name: "relay_plain"}, {name: "relay_implicit_tls", tls: "tls"}, {name: "relay_starttls", tls: "starttls"},
	{name: "direct_plain", direct: true}, {name: "direct_starttls", tls: "starttls", direct: true},
}

type r5ReplyBudgetSpec struct {
	attack     string
	auth       bool
	rcptBytes  int
	finalLimit int
	finalCode  int
}

type r5ReplyBudgetObservation struct {
	attack        string
	recipients    int
	bodyBytes     int
	bodyComplete  bool
	tlsHandshakes int
	smtpBytes     int
	err           error
}

type r5ReplyBudgetPeer struct {
	addr     string
	mode     r5ReplyBudgetMode
	spec     r5ReplyBudgetSpec
	trust    *tls.Config
	reached  chan struct{}
	result   chan r5ReplyBudgetObservation
	finished chan struct{}
}

var r5ReplyAttackComplete = errors.New("owned reply attack completed")

// A complete reply whose individual lines are at most 512 bytes. Large sizes
// exercise a cumulative limit without relying on one oversized protocol line.
func r5SizedSMTPReply(code, size int) string {
	var b strings.Builder
	b.Grow(size)
	for size > 512 {
		n := 512
		if size-n < 6 {
			n = size - 6
		}
		fmt.Fprintf(&b, "%d-%s\r\n", code, strings.Repeat("x", n-6))
		size -= n
	}
	fmt.Fprintf(&b, "%d %s\r\n", code, strings.Repeat("x", size-6))
	return b.String()
}

// SMTP and TLS operate on real owned TCP sockets. Test trust is explicit and
// hostname-verified through the existing private transport helpers; public
// DeliverRelay/DeliverDirect trust defaults are never changed.
func r5NewReplyBudgetPeer(t *testing.T, mode r5ReplyBudgetMode, spec r5ReplyBudgetSpec) *r5ReplyBudgetPeer {
	t.Helper()
	serverTLS, trust := r5TrustedTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &r5ReplyBudgetPeer{addr: listener.Addr().String(), mode: mode, spec: spec, trust: trust,
		reached: make(chan struct{}), result: make(chan r5ReplyBudgetObservation, 1), finished: make(chan struct{})}
	go func() {
		defer close(p.finished)
		out := r5ReplyBudgetObservation{}
		out.err = func() error {
			raw, err := listener.Accept()
			if err != nil {
				return err
			}
			defer raw.Close()
			stopped := make(chan struct{})
			stop := context.AfterFunc(ctx, func() { _ = raw.Close(); close(stopped) })
			defer func() {
				if !stop() {
					<-stopped
				}
			}()
			if err := raw.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return err
			}
			var conn net.Conn = raw
			encrypted := false
			upgrade := func() error {
				secure := tls.Server(raw, serverTLS)
				if err := secure.HandshakeContext(ctx); err != nil {
					return err
				}
				conn, encrypted = secure, true
				out.tlsHandshakes++
				return nil
			}
			if mode.tls == "tls" {
				if err := upgrade(); err != nil {
					return err
				}
			}
			reader := bufio.NewReader(conn)
			write := func(value string) error {
				n, err := io.WriteString(conn, value)
				out.smtpBytes += n
				return err
			}
			attack := func(stage string, code int, unfinishedLine bool) error {
				out.attack = stage
				close(p.reached)
				chunk := fmt.Sprintf("%d-%s\r\n", code, strings.Repeat("x", 506))
				if unfinishedLine {
					if err := write(fmt.Sprintf("%d ", code)); err != nil {
						return r5ReplyAttackComplete
					}
					chunk = strings.Repeat("x", 512)
				}
				chunk = strings.Repeat(chunk, 64)
				for sent := 0; sent < 2*r5SMTPReplyByteLimit; sent += len(chunk) {
					if err := write(chunk); err != nil {
						return r5ReplyAttackComplete
					}
				}
				// The original reader consumes the entire malicious response and
				// remains blocked here. Only the test watchdog then cancels it.
				_, _ = io.Copy(io.Discard, reader)
				return r5ReplyAttackComplete
			}
			reply := func(stage string, code int, value string) error {
				if stage == spec.attack {
					return attack(stage, code, false)
				}
				return write(value)
			}
			if spec.attack == "greeting_line" || spec.attack == "greeting_multiline" {
				return attack(spec.attack, 220, spec.attack == "greeting_line")
			}
			if err := write("220 reply-budget.test ESMTP\r\n"); err != nil {
				return err
			}
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				switch {
				case strings.HasPrefix(line, "EHLO "):
					if spec.attack == "helo" {
						if err := write("500 EHLO unavailable\r\n"); err != nil {
							return err
						}
						continue
					}
					stage := "ehlo"
					if mode.tls == "starttls" && !encrypted {
						stage = "pre_tls_ehlo"
					}
					response := "250-reply-budget.test\r\n"
					if mode.tls == "starttls" && !encrypted {
						response += "250-STARTTLS\r\n"
					}
					response += "250 AUTH PLAIN\r\n"
					if err := reply(stage, 250, response); err != nil {
						return err
					}
				case strings.HasPrefix(line, "HELO "):
					if err := reply("helo", 250, "250 hello\r\n"); err != nil {
						return err
					}
				case line == "STARTTLS\r\n":
					if err := reply("starttls_reply", 220, "220 start TLS\r\n"); err != nil {
						return err
					}
					if err := upgrade(); err != nil {
						return err
					}
					reader = bufio.NewReader(conn)
				case strings.HasPrefix(line, "AUTH PLAIN "):
					if err := reply("auth", 235, "235 authenticated\r\n"); err != nil {
						return err
					}
				case line == "MAIL FROM:<sender@example.test>\r\n":
					if err := reply("mail", 250, "250 sender accepted\r\n"); err != nil {
						return err
					}
				case strings.HasPrefix(line, "RCPT TO:<reader") && strings.HasSuffix(line, "@recipient.test>\r\n"):
					out.recipients++
					response := "250 recipient accepted\r\n"
					if spec.rcptBytes > 0 {
						response = r5SizedSMTPReply(250, spec.rcptBytes)
					}
					if err := reply("rcpt", 250, response); err != nil {
						return err
					}
				case line == "DATA\r\n":
					if err := reply("data", 354, "354 message follows\r\n"); err != nil {
						return err
					}
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							return err
						}
						if line == ".\r\n" {
							out.bodyComplete = true
							break
						}
						out.bodyBytes += len(line)
					}
					code, stage := 250, "final"
					if spec.attack == "final_rejection" {
						code, stage = 550, "final_rejection"
					}
					if spec.finalCode != 0 {
						code = spec.finalCode
					}
					response := fmt.Sprintf("%d message result\r\n", code)
					if spec.finalLimit > 0 {
						remaining := spec.finalLimit - out.smtpBytes
						if remaining < 6 {
							return fmt.Errorf("invalid fixture final reply remainder: %d", remaining)
						}
						response = r5SizedSMTPReply(code, remaining)
					}
					if err := reply(stage, code, response); err != nil {
						return err
					}
				case line == "QUIT\r\n":
					return reply("quit", 221, "221 bye\r\n")
				default:
					return fmt.Errorf("unexpected SMTP command %q", line)
				}
			}
		}()
		if errors.Is(out.err, r5ReplyAttackComplete) {
			out.err = nil
		}
		p.result <- out
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case <-p.finished:
		case <-time.After(5 * time.Second):
			t.Error("reply-budget peer did not join")
		}
	})
	return p
}

func (p *r5ReplyBudgetPeer) deliver(ctx context.Context, to []string, body []byte) error {
	if p.mode.direct {
		return deliverDirectMXTLS(ctx, "127.0.0.1", p.addr, "sender@example.test", to, body, p.mode.tls == "starttls", p.trust)
	}
	host, port, _ := net.SplitHostPort(p.addr)
	number, _ := strconv.Atoi(port)
	cfg := config.Outbound{RelayHost: host, RelayPort: number, RelayTLS: p.mode.tls}
	if p.spec.auth {
		cfg.RelayUser, cfg.RelayPass = "fixture-user", "fixture-password"
	}
	return deliverRelayTLS(ctx, cfg, "sender@example.test", to, body, p.trust)
}

func r5ReplyBudgetJoin(t *testing.T, p *r5ReplyBudgetPeer) r5ReplyBudgetObservation {
	t.Helper()
	select {
	case out := <-p.result:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("production SMTP socket did not close")
		return r5ReplyBudgetObservation{}
	}
}

func r5ReplyBudgetResult(t *testing.T, ctx context.Context, cancel context.CancelFunc, run func() error, reached <-chan struct{}) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- run() }()
	if reached != nil {
		select {
		case <-reached:
		case err := <-done:
			t.Fatalf("SMTP did not reach the selected reply phase: %v", err)
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("SMTP reply barrier was not reached")
		}
	}
	select {
	case err := <-done:
		if ctx.Err() != nil {
			t.Errorf("SMTP depended on cancellation instead of its byte budget: %v", ctx.Err())
		}
		return err
	case <-time.After(time.Second):
		t.Error("SMTP consumed the excessive reply and remained in flight without a byte boundary")
		cancel() // Cleanup of the original red source, never a passing result.
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled SMTP fixture did not return")
			return nil
		}
	}
}

func r5ReplyBudgetError(t *testing.T, err error, uncertain bool) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "SMTP response byte limit exceeded") {
		t.Errorf("delivery did not report the response byte boundary: %v", err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("response byte boundary was replaced by caller cancellation: %v", err)
	}
	if errors.Is(err, store.ErrOutboundUncertain) != uncertain {
		t.Errorf("DATA uncertainty=%v want=%v: %v", errors.Is(err, store.ErrOutboundUncertain), uncertain, err)
	}
}

func TestR5SMTPReplyByteBudgetEveryReplyPhase(t *testing.T) {
	for _, mode := range r5ReplyBudgetModes {
		stages := []string{"greeting_line", "greeting_multiline", "ehlo", "mail", "rcpt", "data", "final", "final_rejection", "quit"}
		if mode.tls == "starttls" {
			stages = append(stages, "pre_tls_ehlo", "starttls_reply")
		} else {
			stages = append(stages, "helo")
		}
		if !mode.direct {
			stages = append(stages, "auth")
		}
		for _, stage := range stages {
			t.Run(mode.name+"/"+stage, func(t *testing.T) {
				p := r5NewReplyBudgetPeer(t, mode, r5ReplyBudgetSpec{attack: stage, auth: stage == "auth"})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				err := r5ReplyBudgetResult(t, ctx, cancel, func() error {
					return p.deliver(ctx, []string{"reader@recipient.test"}, []byte("Subject: bounded replies\r\n\r\nsynthetic body\r\n"))
				}, p.reached)
				if stage == "quit" {
					if err != nil {
						t.Errorf("acknowledged DATA became a failed delivery during QUIT: %v", err)
					}
				} else {
					r5ReplyBudgetError(t, err, stage == "final" || stage == "final_rejection")
				}
				out := r5ReplyBudgetJoin(t, p)
				if out.err != nil || out.attack != stage {
					t.Fatalf("reply fixture failed before target: %+v", out)
				}
				wantBody := stage == "final" || stage == "final_rejection" || stage == "quit"
				if out.bodyComplete != wantBody {
					t.Errorf("DATA terminator observed=%v want=%v", out.bodyComplete, wantBody)
				}
				wantTLS := mode.tls == "tls" || (mode.tls == "starttls" && stage != "pre_tls_ehlo" && stage != "starttls_reply" && !strings.HasPrefix(stage, "greeting_"))
				if (out.tlsHandshakes == 1) != wantTLS {
					t.Errorf("real TLS handshakes=%d wantTLS=%v", out.tlsHandshakes, wantTLS)
				}
			})
		}
	}
}

func TestR5SMTPReplyByteBudgetCumulativeRecipients(t *testing.T) {
	for _, mode := range r5ReplyBudgetModes {
		t.Run(mode.name, func(t *testing.T) {
			p := r5NewReplyBudgetPeer(t, mode, r5ReplyBudgetSpec{rcptBytes: 8192})
			to := make([]string, 200)
			for i := range to {
				to[i] = fmt.Sprintf("reader%d@recipient.test", i)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := r5ReplyBudgetResult(t, ctx, cancel, func() error { return p.deliver(ctx, to, []byte("body\r\n")) }, nil)
			r5ReplyBudgetError(t, err, false)
			out := r5ReplyBudgetJoin(t, p)
			if out.recipients < 2 || out.recipients >= len(to) || out.bodyComplete {
				t.Errorf("many individually complete replies bypassed the cumulative byte limit: %+v", out)
			}
		})
	}
}

func TestR5SMTPReplyByteBudgetNormalRecipientsAndLargeSentBody(t *testing.T) {
	for _, mode := range r5ReplyBudgetModes {
		t.Run(mode.name, func(t *testing.T) {
			// 200 is the existing inbound MAXRECIPIENTS default, used here as a
			// compatibility stress case, not a new outbound recipient cap. The
			// current durable outbound worker uses one recipient per session.
			p := r5NewReplyBudgetPeer(t, mode, r5ReplyBudgetSpec{rcptBytes: 512, auth: !mode.direct})
			to := make([]string, 200)
			for i := range to {
				to[i] = fmt.Sprintf("reader%d@recipient.test", i)
			}
			body := []byte(strings.Repeat("synthetic large body line\r\n", 1<<16))
			if len(body) <= r5SMTPReplyByteLimit {
				t.Fatal("fixture body must exceed the receive budget")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.deliver(ctx, to, body); err != nil {
				t.Fatal(err)
			}
			out := r5ReplyBudgetJoin(t, p)
			if out.err != nil || out.recipients != 200 || !out.bodyComplete || out.bodyBytes != len(body) || out.smtpBytes >= r5SMTPReplyByteLimit {
				t.Errorf("ordinary transaction changed or sent bytes consumed receive budget: %+v body=%d", out, len(body))
			}
			if (out.tlsHandshakes == 1) != (mode.tls != "") {
				t.Errorf("normal compatibility case did not use requested TLS: %+v", out)
			}
		})
	}
}

func TestR5SMTPReplyByteBudgetFinalBoundary(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("limit_plus_%d", extra), func(t *testing.T) {
			p := r5NewReplyBudgetPeer(t, r5ReplyBudgetModes[0], r5ReplyBudgetSpec{finalLimit: r5SMTPReplyByteLimit + extra})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := p.deliver(ctx, []string{"reader@recipient.test"}, []byte("body\r\n"))
			if extra == 0 {
				if err != nil {
					t.Errorf("final 250 exactly at the byte limit must remain accepted: %v", err)
				}
			} else {
				r5ReplyBudgetError(t, err, true)
			}
			out := r5ReplyBudgetJoin(t, p)
			if !out.bodyComplete {
				t.Errorf("boundary test never completed DATA: %+v", out)
			}
		})
	}
}

func TestR5SMTPReplyByteBudgetDefinitiveFinalRejection(t *testing.T) {
	for _, code := range []int{451, 550} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			p := r5NewReplyBudgetPeer(t, r5ReplyBudgetModes[2], r5ReplyBudgetSpec{finalCode: code})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := p.deliver(ctx, []string{"reader@recipient.test"}, []byte("body\r\n"))
			var reply *textproto.Error
			if !errors.As(err, &reply) || reply.Code != code || errors.Is(err, store.ErrOutboundUncertain) {
				t.Errorf("complete definitive rejection lost its original classification: %v", err)
			}
			out := r5ReplyBudgetJoin(t, p)
			if !out.bodyComplete || out.tlsHandshakes != 1 {
				t.Errorf("definitive reply did not follow real TLS and DATA: %+v", out)
			}
		})
	}
}

func TestR5SMTPReplyByteBudgetDirectDoesNotRepeatPossibleAcceptance(t *testing.T) {
	for _, mode := range []r5ReplyBudgetMode{r5ReplyBudgetModes[3], r5ReplyBudgetModes[4]} {
		for _, stage := range []string{"final", "final_rejection", "quit"} {
			t.Run(mode.name+"/"+stage, func(t *testing.T) {
				p := r5NewReplyBudgetPeer(t, mode, r5ReplyBudgetSpec{attack: stage})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				sessions := 0
				err := r5ReplyBudgetResult(t, ctx, cancel, func() error {
					return deliverDirectWith(ctx, "sender@example.test", []string{"reader@recipient.test"}, []byte("body\r\n"), mode.tls != "",
						func(context.Context, string) ([]*net.MX, error) {
							return []*net.MX{{Host: "first.test."}, {Host: "second.test."}}, nil
						},
						func(ctx context.Context, host, _ string, _ string, to []string, body []byte, _ bool) error {
							sessions++
							if host != "first.test" {
								return errors.New("unexpected second MX after possible acceptance")
							}
							return p.deliver(ctx, to, body)
						})
				}, p.reached)
				if stage == "quit" {
					if err != nil {
						t.Errorf("final 250 lost during cleanup: %v", err)
					}
				} else {
					r5ReplyBudgetError(t, err, true)
				}
				if sessions != 1 {
					t.Errorf("possible acceptance was repeated on %d MX sessions", sessions)
				}
				if out := r5ReplyBudgetJoin(t, p); !out.bodyComplete || out.err != nil {
					t.Errorf("direct uncertainty fixture failed: %+v", out)
				}
			})
		}
	}
}

type r5ReplyCountingConn struct {
	net.Conn
	received atomic.Int64
}

func (c *r5ReplyCountingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.received.Add(int64(n))
	return n, err
}

func TestR5SMTPReplyByteBudgetExactWireBoundary(t *testing.T) {
	for _, version := range []uint16{0, tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(fmt.Sprintf("TLS_%x", version), func(t *testing.T) {
			serverTLS, clientTLS := r5TrustedTLS(t)
			if version != 0 {
				serverTLS.MinVersion, serverTLS.MaxVersion = version, version
				clientTLS.MinVersion, clientTLS.MaxVersion = version, version
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer ln.Close()
			reached, peerDone := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(peerDone)
				raw, err := ln.Accept()
				if err != nil {
					return
				}
				defer raw.Close()
				stopped := make(chan struct{})
				stop := context.AfterFunc(ctx, func() { _ = raw.Close(); close(stopped) })
				defer func() {
					if !stop() {
						<-stopped
					}
				}()
				var conn net.Conn = raw
				if version != 0 {
					secure := tls.Server(raw, serverTLS)
					if err := secure.HandshakeContext(ctx); err != nil {
						return
					}
					conn = secure
				}
				close(reached)
				_, _ = io.WriteString(conn, strings.Repeat("x", 2*r5SMTPReplyByteLimit))
				_, _ = io.Copy(io.Discard, conn)
			}()
			var counted *r5ReplyCountingConn
			var applicationBytes int64
			err = r5ReplyBudgetResult(t, ctx, cancel, func() error {
				conn, release, err := dialSMTPWithConnector(ctx, ln.Addr().String(), func(ctx context.Context, network, address string) (net.Conn, error) {
					raw, err := (&net.Dialer{}).DialContext(ctx, network, address)
					if err != nil {
						return nil, err
					}
					counted = &r5ReplyCountingConn{Conn: raw}
					return counted, nil
				})
				if err != nil {
					return err
				}
				defer release()
				if version != 0 {
					secure := tls.Client(conn, clientTLS)
					if err := secure.HandshakeContext(ctx); err != nil {
						return err
					}
					conn = secure
				}
				applicationBytes, err = io.Copy(io.Discard, conn)
				before := counted.received.Load()
				if _, again := conn.Read(make([]byte, 1)); again == nil || counted.received.Load() != before {
					return fmt.Errorf("exhausted transport budget read more bytes: before=%d after=%d error=%v", before, counted.received.Load(), again)
				}
				return err
			}, reached)
			r5ReplyBudgetError(t, err, false)
			if counted == nil || counted.received.Load() != r5SMTPReplyByteLimit {
				t.Errorf("underlying raw socket did not stop exactly at the wire budget: %v", counted)
			}
			if version == 0 && applicationBytes != r5SMTPReplyByteLimit {
				t.Errorf("plaintext byte count=%d", applicationBytes)
			}
			if version != 0 && (applicationBytes <= 0 || applicationBytes >= r5SMTPReplyByteLimit) {
				t.Errorf("TLS handshake/ciphertext did not consume the same raw budget: application bytes=%d", applicationBytes)
			}
			select {
			case <-peerDone:
			case <-time.After(5 * time.Second):
				t.Fatal("wire-budget peer did not join")
			}
		})
	}
}

// Keep the race detector checking independent sockets without a package-global
// budget. Each session receives its own complete allowance.
func TestR5SMTPReplyByteBudgetIndependentSessions(t *testing.T) {
	var peers []*r5ReplyBudgetPeer
	for range 2 {
		peers = append(peers, r5NewReplyBudgetPeer(t, r5ReplyBudgetModes[0], r5ReplyBudgetSpec{finalLimit: 3 * r5SMTPReplyByteLimit / 4}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, len(peers))
	for _, p := range peers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- p.deliver(ctx, []string{"reader@recipient.test"}, []byte("body\r\n"))
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("independent SMTP sessions shared a byte allowance: %v", err)
		}
	}
	for _, p := range peers {
		if out := r5ReplyBudgetJoin(t, p); out.err != nil || !out.bodyComplete {
			t.Errorf("independent socket fixture: %+v", out)
		}
	}
}
