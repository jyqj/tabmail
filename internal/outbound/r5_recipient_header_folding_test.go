package outbound

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/textproto"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/testutil"
)

func r5AdvanceHeaderRecipients(kind string, n int) []string {
	result := make([]string, n)
	for i := range result {
		value := fmt.Sprintf("mailbox-%02d-long-address@recipient.example.test", i)
		if kind == "quoted" {
			value = fmt.Sprintf(`"desk %02d,help@room\"east\\west"@recipient.example.test`, i)
		}
		if kind == "unicode" {
			value = fmt.Sprintf(`"信箱 %02d,帮助台"@recipient.example.test`, i)
		}
		result[i] = value
	}
	return result
}

func r5AdvanceHeaderIdentity(t *testing.T, values []string) []string {
	t.Helper()
	result := make([]string, len(values))
	for i, value := range values {
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			t.Fatalf("fixture address %q: %v", value, err)
		}
		result[i] = parsed.Address
	}
	return result
}

func r5AdvanceCheckHeaderRecipients(t *testing.T, raw []byte, to, cc, bcc []string, softLimit bool) {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("built message is not parseable: %v", err)
	}
	for name, want := range map[string][]string{"To": to, "Cc": cc} {
		if len(want) == 0 {
			if message.Header.Get(name) != "" {
				t.Errorf("empty %s acquired recipients: %s", name, message.Header.Get(name))
			}
			continue
		}
		parsed, err := message.Header.AddressList(name)
		if err != nil {
			t.Fatalf("folded %s address list: %v", name, err)
		}
		got := make([]string, len(parsed))
		for i, address := range parsed {
			got[i] = address.Address
		}
		if !reflect.DeepEqual(got, r5AdvanceHeaderIdentity(t, want)) {
			t.Errorf("%s folding changed identities or order: got=%q want=%q", name, got, want)
		}
	}
	head := bytes.SplitN(raw, []byte("\r\n\r\n"), 2)[0]
	if message.Header.Get("Bcc") != "" {
		t.Error("BCC header appeared")
	}
	for _, address := range bcc {
		if bytes.Contains(head, []byte(address)) {
			t.Errorf("BCC address was disclosed: %q", address)
		}
	}
	field := ""
	for _, line := range strings.Split(string(head), "\r\n") {
		if len(line) > 998 {
			t.Errorf("MIME header line has %d octets, exceeding 998", len(line))
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			field, _, _ = strings.Cut(line, ":")
		}
		if softLimit && (field == "To" || field == "Cc") && len(line) > 78 {
			t.Errorf("foldable %s line has %d octets, exceeding target 78", field, len(line))
		}
	}
}

func TestR5AdvanceRecipientHeaderFolding(t *testing.T) {
	for _, kind := range []string{"plain", "quoted", "unicode"} {
		for _, count := range []int{1, 2, 50} {
			for _, role := range []string{"to", "cc"} {
				t.Run(fmt.Sprintf("%s/%d/%s", kind, count, role), func(t *testing.T) {
					message := Message{From: "sender@example.test", Subject: "Folding fixture", TextBody: "body", MessageID: "<folding@example.test>"}
					if role == "to" {
						message.To = r5AdvanceHeaderRecipients(kind, count)
					} else {
						message.CC = r5AdvanceHeaderRecipients(kind, count)
					}
					before := append([]string(nil), message.EnvelopeRecipients()...)
					raw, err := Build(message)
					if err != nil {
						t.Fatal(err)
					}
					r5AdvanceCheckHeaderRecipients(t, raw, message.To, message.CC, nil, true)
					if !reflect.DeepEqual(before, message.EnvelopeRecipients()) {
						t.Error("folding mutated the caller's envelope")
					}
				})
			}
		}
	}
	for _, roles := range []string{"mixed", "bcc-only"} {
		t.Run(roles, func(t *testing.T) {
			message := Message{From: "sender@example.test", BCC: []string{"undisclosed@example.test"}, Subject: "Folding fixture", TextBody: "body", HTMLBody: "<p>body</p>", Attachments: []Attachment{{Filename: "file.txt", Data: []byte("content")}}}
			if roles == "mixed" {
				message.To = r5AdvanceHeaderRecipients("plain", 24)
				message.CC = r5AdvanceHeaderRecipients("quoted", 25)
			}
			raw, err := Build(message)
			if err != nil {
				t.Fatal(err)
			}
			r5AdvanceCheckHeaderRecipients(t, raw, message.To, message.CC, message.BCC, true)
		})
	}
}

func TestR5AdvanceRecipientHeaderHardLimit(t *testing.T) {
	// A single mailbox token cannot be split safely inside its local part.
	// Bound the emitted continuation even for legacy input beyond SMTP's
	// address-size rules; do not silently emit an overlong MIME field.
	for _, role := range []string{"to", "cc"} {
		for _, length := range []int{77, 78, 995, 996, 997, 998, 1200} {
			t.Run(fmt.Sprintf("%s/%d", role, length), func(t *testing.T) {
				address := strings.Repeat("a", length-len("@example.test")) + "@example.test"
				message := Message{From: "sender@example.test", Subject: "Folding fixture", TextBody: "body"}
				if role == "to" {
					message.To = []string{address}
				} else {
					message.CC = []string{address}
				}
				raw, err := Build(message)
				if length > 997 {
					if err == nil || raw != nil {
						t.Fatal("unbreakable overlong address released malformed MIME")
					}
					return
				}
				if err != nil {
					t.Fatalf("bounded legacy token rejected: %v", err)
				}
				r5AdvanceCheckHeaderRecipients(t, raw, message.To, message.CC, nil, false)
			})
		}
	}
}

type r5AdvanceHeaderPeerResult struct {
	raw []byte
	err error
}

func r5AdvanceHeaderPeer(t *testing.T, recipients []string) (net.Addr, <-chan r5AdvanceHeaderPeerResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan r5AdvanceHeaderPeerResult, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		out := r5AdvanceHeaderPeerResult{}
		out.err = func() error {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			reader := textproto.NewReader(bufio.NewReader(conn))
			write := func(s string) error { _, err := fmt.Fprint(conn, s+"\r\n"); return err }
			read := func(want, response string) error {
				line, err := reader.ReadLine()
				if err != nil || line != want {
					return fmt.Errorf("SMTP command=%q want=%q error=%v", line, want, err)
				}
				return write(response)
			}
			if err := write("220 loopback.test ESMTP"); err != nil {
				return err
			}
			if err := read("EHLO localhost", "250 loopback.test"); err != nil {
				return err
			}
			if err := read("MAIL FROM:<sender@example.test>", "250 sender accepted"); err != nil {
				return err
			}
			for _, recipient := range recipients {
				if err := read("RCPT TO:<"+recipient+">", "250 recipient accepted"); err != nil {
					return err
				}
			}
			if err := read("DATA", "354 send body"); err != nil {
				return err
			}
			lines, err := reader.ReadDotLines()
			if err != nil {
				return err
			}
			out.raw = []byte(strings.Join(lines, "\r\n") + "\r\n")
			for _, line := range lines {
				if len(line) > 998 {
					return write("550 5.6.0 MIME line exceeds 998 octets")
				}
			}
			if err := write("250 message accepted"); err != nil {
				return err
			}
			return read("QUIT", "221 goodbye")
		}()
		done <- out
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("header validation SMTP peer did not join")
		}
	})
	return listener.Addr(), done
}

func TestR5AdvanceRecipientHeadersSubmitAndSMTP(t *testing.T) {
	for _, mode := range []string{"direct", "relay"} {
		for _, roles := range []string{"to", "cc", "mixed"} {
			t.Run(mode+"/"+roles, func(t *testing.T) {
				st := testutil.NewFakeStore()
				req := quotaTestSendRequest(uuid.New(), uuid.New())
				seedQuotaSender(t, st, &req)
				req.To, req.CC = nil, nil
				req.BCC = []string{"undisclosed@example.test"}
				switch roles {
				case "to":
					req.To = r5AdvanceHeaderRecipients("plain", 49)
				case "cc":
					req.CC = r5AdvanceHeaderRecipients("quoted", 49)
				case "mixed":
					req.To = r5AdvanceHeaderRecipients("plain", 24)
					req.CC = r5AdvanceHeaderRecipients("quoted", 25)
				}
				svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, nopGovernance{}, zerolog.Nop())
				job, err := svc.Submit(context.Background(), req)
				if err != nil {
					t.Fatalf("valid 50-recipient submission rejected: %v", err)
				}
				queued, err := st.GetOutboundJob(context.Background(), job.ID)
				if err != nil || len(queued.RcptTo) != 50 {
					t.Fatalf("queued envelope lost recipients: %v", err)
				}
				raw, err := svc.buildQueuedMIME(context.Background(), queued)
				if err != nil {
					t.Fatal(err)
				}
				address, done := r5AdvanceHeaderPeer(t, queued.RcptTo)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if mode == "direct" {
					err = deliverDirectMX(ctx, "127.0.0.1", address.String(), queued.MailFrom, queued.RcptTo, raw, false)
				} else {
					tcp := address.(*net.TCPAddr)
					err = DeliverRelay(ctx, config.Outbound{RelayHost: tcp.IP.String(), RelayPort: tcp.Port, RelayTLS: "none"}, queued.MailFrom, queued.RcptTo, raw)
				}
				if err != nil {
					t.Errorf("submitted MIME rejected by bounded SMTP peer: %v", err)
				}
				select {
				case result := <-done:
					if result.err != nil {
						t.Fatalf("SMTP peer failed: %v", result.err)
					}
					r5AdvanceCheckHeaderRecipients(t, result.raw, queued.To, queued.CC, queued.BCC, true)
				case <-ctx.Done():
					t.Fatal("header validation peer did not finish")
				}
			})
		}
	}
}
