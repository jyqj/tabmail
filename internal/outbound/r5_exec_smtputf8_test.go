package outbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
	"tabmail/internal/workqueue"
)

type execUTF8Spec struct {
	beforeTLS, afterTLS bool
	heloOnly            bool
}

type execUTF8Observation struct {
	mail, recipients []string
	raw              []byte
	data, ehlo, helo int
	tlsHandshakes    int
	err              error
}

type execUTF8Peer struct {
	addr   string
	mode   r5ReplyBudgetMode
	spec   execUTF8Spec
	trust  *tls.Config
	result chan execUTF8Observation
}

// The peer accepts every envelope and complete DATA, including an illegal
// internationalized transaction without SMTPUTF8. Rejection must therefore
// come from the real production client, before any mail command reaches TCP.
// TLS uses an explicitly trusted test CA; no production trust policy changes.
func newExecUTF8Peer(t *testing.T, mode r5ReplyBudgetMode, spec execUTF8Spec) *execUTF8Peer {
	t.Helper()
	serverTLS, trust := r5TrustedTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &execUTF8Peer{addr: listener.Addr().String(), mode: mode, spec: spec, trust: trust,
		result: make(chan execUTF8Observation, 1)}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out := execUTF8Observation{}
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
			write := func(value string) error { _, err := io.WriteString(conn, value); return err }
			if err := write("220 utf8.test ESMTP\r\n"); err != nil {
				return err
			}
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					if errors.Is(err, io.EOF) && line == "" {
						return nil // the client may reject immediately after EHLO
					}
					return err
				}
				line = strings.TrimSuffix(line, "\r\n")
				switch {
				case line == "EHLO localhost":
					out.ehlo++
					if spec.heloOnly {
						if err := write("500 use HELO\r\n"); err != nil {
							return err
						}
						continue
					}
					response := "250-utf8.test\r\n"
					if mode.tls == "starttls" && !encrypted {
						response += "250-STARTTLS\r\n"
					}
					capable := spec.beforeTLS
					if encrypted {
						capable = spec.afterTLS
					}
					if capable {
						response += "250-SMTPUTF8\r\n"
					}
					if err := write(response + "250 8BITMIME\r\n"); err != nil {
						return err
					}
				case line == "HELO localhost":
					out.helo++
					if err := write("250 hello\r\n"); err != nil {
						return err
					}
				case line == "STARTTLS":
					if err := write("220 start TLS\r\n"); err != nil {
						return err
					}
					if err := upgrade(); err != nil {
						return err
					}
					reader = bufio.NewReader(conn)
				case strings.HasPrefix(line, "MAIL FROM:"):
					out.mail = append(out.mail, line)
					if err := write("250 sender accepted\r\n"); err != nil {
						return err
					}
				case strings.HasPrefix(line, "RCPT TO:"):
					out.recipients = append(out.recipients, line)
					if err := write("250 recipient accepted\r\n"); err != nil {
						return err
					}
				case line == "DATA":
					out.data++
					if err := write("354 send message\r\n"); err != nil {
						return err
					}
					lines, err := textproto.NewReader(reader).ReadDotLines()
					if err != nil {
						return err
					}
					out.raw = []byte(strings.Join(lines, "\r\n") + "\r\n")
					if err := write("250 accepted\r\n"); err != nil {
						return err
					}
				case line == "QUIT":
					return write("221 bye\r\n")
				default:
					return fmt.Errorf("unexpected SMTP command %q", line)
				}
			}
		}()
		p.result <- out
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("owned SMTPUTF8 peer did not stop")
		}
	})
	return p
}

func (p *execUTF8Peer) relayConfig(t *testing.T) config.Outbound {
	t.Helper()
	host, port, err := net.SplitHostPort(p.addr)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return config.Outbound{RelayHost: host, RelayPort: number, RelayTLS: p.mode.tls}
}

func (p *execUTF8Peer) deliver(t *testing.T, ctx context.Context, from string, to []string, raw []byte) error {
	t.Helper()
	if p.mode.direct {
		return deliverDirectMXTLS(ctx, "127.0.0.1", p.addr, from, to, raw, p.mode.tls == "starttls", p.trust)
	}
	if p.mode.tls == "" {
		return DeliverRelay(ctx, p.relayConfig(t), from, to, raw)
	}
	return deliverRelayTLS(ctx, p.relayConfig(t), from, to, raw, p.trust)
}

func (p *execUTF8Peer) observe(t *testing.T, ctx context.Context) execUTF8Observation {
	t.Helper()
	select {
	case got := <-p.result:
		if got.err != nil {
			t.Fatalf("actual SMTPUTF8 conversation failed: %v", got.err)
		}
		wantEHLO, wantTLS, wantHELO := 1, 0, 0
		if p.mode.tls != "" {
			wantTLS = 1
		}
		if p.mode.tls == "starttls" {
			wantEHLO = 2
		}
		if p.spec.heloOnly {
			wantHELO = 1
		}
		if got.ehlo != wantEHLO || got.helo != wantHELO || got.tlsHandshakes != wantTLS {
			t.Errorf("negotiation counts EHLO=%d HELO=%d TLS=%d, want %d/%d/%d", got.ehlo, got.helo, got.tlsHandshakes, wantEHLO, wantHELO, wantTLS)
		}
		return got
	case <-ctx.Done():
		t.Fatal("SMTPUTF8 peer did not return its observation")
		return execUTF8Observation{}
	}
}

func execUTF8Rejected(t *testing.T, err error, got execUTF8Observation) {
	t.Helper()
	var reply *textproto.Error
	if err == nil || !strings.Contains(err.Error(), "SMTPUTF8") || errors.Is(err, store.ErrOutboundUncertain) || errors.As(err, &reply) {
		t.Errorf("SMTPUTF8 requirement must produce a local pre-send error, got %v", err)
	}
	if len(got.mail) != 0 || len(got.recipients) != 0 || got.data != 0 || len(got.raw) != 0 {
		t.Errorf("EXEC09_SENT_WITHOUT_SMTPUTF8 MAIL=%q RCPT=%q DATA=%d body_bytes=%d", got.mail, got.recipients, got.data, len(got.raw))
	}
}

func execUTF8Accepted(t *testing.T, err error, got execUTF8Observation, from string, to []string, raw []byte, capable, heloOnly bool) {
	t.Helper()
	if err != nil {
		t.Errorf("compatible message was rejected: %v", err)
	}
	wantMail := "MAIL FROM:<" + from + ">"
	if !heloOnly {
		wantMail += " BODY=8BITMIME"
	}
	if capable {
		wantMail += " SMTPUTF8"
	}
	wantRecipients := make([]string, 0, len(to))
	for _, recipient := range to {
		wantRecipients = append(wantRecipients, "RCPT TO:<"+recipient+">")
	}
	if !reflect.DeepEqual(got.mail, []string{wantMail}) || !reflect.DeepEqual(got.recipients, wantRecipients) || got.data != 1 {
		t.Errorf("SMTP envelope or negotiated MAIL flag changed: MAIL=%q RCPT=%q DATA=%d, want MAIL=%q RCPT=%q", got.mail, got.recipients, got.data, wantMail, wantRecipients)
	}
	wantRaw := raw
	if !bytes.HasSuffix(raw, []byte("\r\n")) {
		wantRaw = append(bytes.Clone(raw), '\r', '\n')
	}
	if !bytes.Equal(got.raw, wantRaw) {
		t.Errorf("SMTP changed MIME bytes: received=%d wanted=%d", len(got.raw), len(wantRaw))
	}
}

func execUTF8Message(t *testing.T, name string) (from string, to []string, raw []byte, required bool) {
	t.Helper()
	m := Message{From: "sender@example.test", To: []string{"reader@recipient.test"},
		Subject: "ASCII subject", TextBody: "message\r\n.dot line\r\n", MessageID: "<exec09@example.test>"}
	from = m.From
	switch name {
	case "ascii_quoted_alabel":
		m.To = []string{`"team @ help"@xn--fsqu00a.test`}
	case "unicode_sender_envelope_only":
		from, required = "发送者@example.test", true
	case "unicode_recipient_envelope_only":
		to, required = []string{"收件人@recipient.test"}, true
	case "unicode_domain_envelope_only":
		to, required = []string{"reader@例子.test"}, true
	case "unicode_last_bcc":
		m.BCC, required = []string{`"信箱 @ 帮助台"@recipient.test`}, true
	case "unicode_top_level_header":
		m.Headers, required = map[string]string{"X-Campaign": "每周简报"}, true
	case "encoded_headers_body_and_attachment":
		m.Subject, m.TextBody, m.HTMLBody = "每周简报", "消息正文\r\n", "<p>消息正文</p>\r\n"
		m.Headers = map[string]string{"X-Campaign": "=?UTF-8?B?5L2g5aW9?="}
		m.Attachments = []Attachment{{Filename: "附件.txt", Data: []byte("附件内容\x00\xff")}}
	case "eight_bit_body_only":
		raw = []byte("From: sender@example.test\r\nTo: reader@recipient.test\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n消息正文\r\n.dot line\r\n")
	default:
		t.Fatalf("unknown SMTPUTF8 case %q", name)
	}
	if to == nil {
		to = m.EnvelopeRecipients()
	}
	if raw == nil {
		var err error
		raw, err = Build(m)
		if err != nil {
			t.Fatal(err)
		}
	}
	if name == "unicode_last_bcc" && bytes.Contains(raw, []byte(m.BCC[0])) {
		t.Fatal("BCC fixture must require SMTPUTF8 solely in its envelope")
	}
	return from, to, raw, required
}

func TestExecSMTPUTF8Capabilities(t *testing.T) {
	for _, mode := range r5ReplyBudgetModes {
		for _, capable := range []bool{false, true} {
			for _, name := range []string{"ascii_quoted_alabel", "unicode_sender_envelope_only", "unicode_recipient_envelope_only", "unicode_domain_envelope_only", "unicode_last_bcc", "unicode_top_level_header", "encoded_headers_body_and_attachment", "eight_bit_body_only"} {
				t.Run(mode.name+"/capable="+strconv.FormatBool(capable)+"/"+name, func(t *testing.T) {
					from, to, raw, required := execUTF8Message(t, name)
					originalRaw, originalTo := bytes.Clone(raw), append([]string(nil), to...)
					p := newExecUTF8Peer(t, mode, execUTF8Spec{beforeTLS: capable, afterTLS: capable})
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					err := p.deliver(t, ctx, from, to, raw)
					got := p.observe(t, ctx)
					if required && !capable {
						execUTF8Rejected(t, err, got)
					} else {
						execUTF8Accepted(t, err, got, from, to, raw, capable, false)
					}
					if !bytes.Equal(raw, originalRaw) || !reflect.DeepEqual(to, originalTo) {
						t.Error("delivery rewrote caller-owned message or internationalized addresses")
					}
				})
			}
		}
	}
}

func TestExecSMTPUTF8RefreshAfterSTARTTLS(t *testing.T) {
	for _, mode := range []r5ReplyBudgetMode{r5ReplyBudgetModes[2], r5ReplyBudgetModes[4]} {
		for _, finalCapability := range []bool{false, true} {
			for _, name := range []string{"unicode_last_bcc", "encoded_headers_body_and_attachment"} {
				t.Run(mode.name+"/final_capable="+strconv.FormatBool(finalCapability)+"/"+name, func(t *testing.T) {
					from, to, raw, required := execUTF8Message(t, name)
					p := newExecUTF8Peer(t, mode, execUTF8Spec{beforeTLS: !finalCapability, afterTLS: finalCapability})
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					err := p.deliver(t, ctx, from, to, raw)
					got := p.observe(t, ctx)
					if required && !finalCapability {
						execUTF8Rejected(t, err, got)
					} else {
						execUTF8Accepted(t, err, got, from, to, raw, finalCapability, false)
					}
				})
			}
		}
	}
}

func TestExecSMTPUTF8HELOFallback(t *testing.T) {
	for _, mode := range []r5ReplyBudgetMode{r5ReplyBudgetModes[0], r5ReplyBudgetModes[3]} {
		for _, name := range []string{"ascii_quoted_alabel", "unicode_sender_envelope_only"} {
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				from, to, raw, required := execUTF8Message(t, name)
				p := newExecUTF8Peer(t, mode, execUTF8Spec{heloOnly: true})
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				err := p.deliver(t, ctx, from, to, raw)
				got := p.observe(t, ctx)
				if required {
					execUTF8Rejected(t, err, got)
				} else {
					execUTF8Accepted(t, err, got, from, to, raw, false, true)
				}
			})
		}
	}
}

func TestExecSMTPUTF8TriesNextMXBeforeSending(t *testing.T) {
	from, to, raw, _ := execUTF8Message(t, "unicode_recipient_envelope_only")
	peers := []*execUTF8Peer{newExecUTF8Peer(t, r5ReplyBudgetModes[3], execUTF8Spec{}),
		newExecUTF8Peer(t, r5ReplyBudgetModes[3], execUTF8Spec{beforeTLS: true, afterTLS: true})}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var visited []string
	var firstErr error
	err := deliverDirectWith(ctx, from, to, raw, false,
		func(context.Context, string) ([]*net.MX, error) {
			return []*net.MX{{Pref: 10, Host: "first.test."}, {Pref: 20, Host: "second.test."}}, nil
		},
		func(ctx context.Context, host, _ string, from string, to []string, raw []byte, required bool) error {
			index := len(visited)
			visited = append(visited, host)
			if index >= len(peers) {
				return errors.New("unexpected extra MX attempt")
			}
			err := peers[index].deliver(t, ctx, from, to, raw)
			if index == 0 {
				firstErr = err
			}
			return err
		})
	if err != nil {
		t.Errorf("capable second MX did not accept: %v", err)
	}
	execUTF8Rejected(t, firstErr, peers[0].observe(t, ctx))
	if !reflect.DeepEqual(visited, []string{"first.test", "second.test"}) {
		t.Fatalf("SMTPUTF8 mismatch must try the next MX before any DATA: visited=%q", visited)
	}
	execUTF8Accepted(t, err, peers[1].observe(t, ctx), from, to, raw, true, false)
}

// Submit, MIME build, authorization, fenced recipient completion and adapter
// delivery are the shipping paths. Only persistence is the existing FakeStore;
// this is real SMTP evidence, not a claim of live PostgreSQL execution.
func TestExecSMTPUTF8SubmissionRecipientLedger(t *testing.T) {
	for _, mode := range []r5ReplyBudgetMode{r5ReplyBudgetModes[0], r5ReplyBudgetModes[3]} {
		for _, capable := range []bool{false, true} {
			for _, role := range []string{"to", "cc", "bcc", "header"} {
				t.Run(mode.name+"/capable="+strconv.FormatBool(capable)+"/"+role, func(t *testing.T) {
					st := testutil.NewFakeStore()
					req := quotaTestSendRequest(uuid.New(), uuid.New())
					seedQuotaSender(t, st, &req)
					req.To, req.CC, req.BCC = nil, nil, nil
					req.Subject, req.TextBody = "编码主题", "消息正文\r\n"
					const recipient = `"信箱 @ 帮助台"@recipient.test`
					switch role {
					case "to":
						req.To = []string{recipient}
					case "cc":
						req.CC = []string{recipient}
					case "bcc":
						req.BCC = []string{recipient}
					case "header":
						req.To = []string{"reader@recipient.test"}
						req.Headers = map[string]string{"X-Campaign": "每周简报"}
					}
					svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, nopGovernance{}, zerolog.Nop())
					job, err := svc.Submit(t.Context(), req)
					if err != nil {
						t.Fatalf("valid internationalized message must reach delivery: %v", err)
					}
					p := newExecUTF8Peer(t, mode, execUTF8Spec{beforeTLS: capable, afterTLS: capable})
					if mode.direct {
						svc.adapter = r5QuotedAdapter(func(ctx context.Context, current *models.OutboundJob, raw []byte) (*DeliveryResult, error) {
							err := deliverDirectWith(ctx, current.MailFrom, current.RcptTo, raw, false,
								func(context.Context, string) ([]*net.MX, error) { return []*net.MX{{Host: "mx.test."}}, nil },
								func(ctx context.Context, _, _ string, from string, to []string, raw []byte, _ bool) error {
									return p.deliver(t, ctx, from, to, raw)
								})
							result := &DeliveryResult{Adapter: "direct_mx"}
							if err == nil {
								result.SMTPCode = 250
							} else {
								result.Error = err.Error()
							}
							return result, err
						})
					} else {
						svc.adapter = NewRelayAdapter(p.relayConfig(t))
					}
					claimed, err := st.ClaimOutboundJobs(t.Context(), time.Now(), 1)
					if err != nil || len(claimed) != 1 {
						t.Fatalf("claim real submitted job: jobs=%d err=%v", len(claimed), err)
					}
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					work := &workqueue.Job[*outboundJob]{ID: job.ID, Attempts: claimed[0].Attempts,
						Payload: &outboundJob{OutboundJob: claimed[0]}, Lease: workqueue.Lease{Token: claimed[0].DeliveryToken}}
					err = svc.processOne(ctx, work)
					got := p.observe(t, ctx)
					wantState := delivery.Temporary
					if capable {
						wantState = delivery.Accepted
						raw, buildErr := svc.buildQueuedMIME(ctx, claimed[0])
						if buildErr != nil {
							t.Fatal(buildErr)
						}
						// Date is built at send time; assert wire identities and the
						// byte-stable body separately from that clock-owned header.
						if err != nil || got.data != 1 || len(got.mail) != 1 || !strings.HasSuffix(got.mail[0], " SMTPUTF8") || !reflect.DeepEqual(got.recipients, []string{"RCPT TO:<" + job.RcptTo[0] + ">"}) {
							t.Errorf("capable peer lost original submitted envelope: got=%+v err=%v", got, err)
						}
						_, gotBody, _ := bytes.Cut(got.raw, []byte("\r\n\r\n"))
						_, wantBody, _ := bytes.Cut(raw, []byte("\r\n\r\n"))
						if !bytes.Equal(gotBody, wantBody) {
							t.Error("submitted MIME body changed during SMTPUTF8 delivery")
						}
					} else {
						execUTF8Rejected(t, err, got)
					}
					if role == "bcc" && bytes.Contains(got.raw, []byte(recipient)) {
						t.Error("SMTPUTF8 BCC identity leaked into the MIME message")
					}
					rows, rowsErr := st.ListOutboundRecipients(ctx, job.TenantID, job.ID)
					if rowsErr != nil || len(rows) != 1 || rows[0].State != wantState || rows[0].Address != job.RcptTo[0] {
						t.Errorf("actual delivery recipient state = %+v, want %s: %v", rows, wantState, rowsErr)
					}
					stored, storeErr := st.GetOutboundJob(ctx, job.ID)
					if storeErr != nil || stored == nil || (stored.State == models.OutboundSent) != capable {
						t.Errorf("job sent state disagrees with actual peer capability: capable=%t job=%+v err=%v", capable, stored, storeErr)
					}
					attempts, attemptsErr := st.ListOutboundAttempts(ctx, job.ID)
					if attemptsErr != nil || len(attempts) != 1 || (!capable && attempts[0].SMTPCode != 0) {
						t.Errorf("capability check fabricated a server rejection or acceptance: attempts=%+v err=%v", attempts, attemptsErr)
					}
				})
			}
		}
	}
}
