package outbound

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/textproto"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/config"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// RFC 5321 section 4.1.3 literals route to the stated IP, without DNS. The
// expectations here are fixed input/output data, not the product's parser.
var r5NextLiteralCases = []struct{ name, input, envelope, identity, host string }{
	{"ipv4", "Reader@[127.0.0.1]", "reader@[127.0.0.1]", "reader@[127.0.0.1]", "127.0.0.1"},
	{"ipv6-compressed", "Reader@[IPv6:::1]", "reader@[ipv6:::1]", "reader@[ipv6:::1]", "::1"},
	{"ipv6-full", "Reader@[IPv6:2001:0DB8:0:0:0:0:0:1]", "reader@[ipv6:2001:0db8:0:0:0:0:0:1]", "reader@[ipv6:2001:0db8:0:0:0:0:0:1]", "2001:db8::1"},
	{"ipv6-embedded-ipv4", "Reader@[iPv6:::ffff:192.0.2.10]", "reader@[ipv6:::ffff:192.0.2.10]", "reader@[ipv6:::ffff:192.0.2.10]", "::ffff:192.0.2.10"},
	{"display", "A Person <Reader@[IPv6:::1]>", "reader@[ipv6:::1]", "reader@[ipv6:::1]", "::1"},
	{"quoted-local", `"Team@Desk"@[IPv6:::1]`, `"team@desk"@[ipv6:::1]`, "team@desk@[ipv6:::1]", "::1"},
	{"quoted-and-comments", `Display <"Team Desk"@[IPv6:::1]> (receipt)`, `"team desk"@[ipv6:::1]`, "team desk@[ipv6:::1]", "::1"},
	{"legacy-untagged-ipv6", "Reader@[::1]", "reader@[::1]", "reader@[::1]", "::1"},
}

func TestR5NextAddressLiteralParseAndRoute(t *testing.T) {
	for _, tc := range r5NextLiteralCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseRecipientAddress(tc.input)
			if err != nil || parsed.Envelope != tc.envelope || parsed.Identity != tc.identity {
				t.Errorf("literal address = %+v (%v), want envelope %q identity %q", parsed, err, tc.envelope, tc.identity)
			}
			lookups, attempts := 0, 0
			err = deliverDirectWith(context.Background(), "sender@example.test", []string{tc.envelope}, []byte("wire"), true,
				func(context.Context, string) ([]*net.MX, error) {
					lookups++
					return nil, errors.New("literal must not query DNS")
				},
				func(_ context.Context, host, addr, from string, to []string, raw []byte, required bool) error {
					attempts++
					if host != tc.host || addr != net.JoinHostPort(tc.host, "25") || from != "sender@example.test" || !reflect.DeepEqual(to, []string{tc.envelope}) || string(raw) != "wire" || !required {
						return fmt.Errorf("changed route or delivery policy: host=%q addr=%q from=%q to=%q tls=%v", host, addr, from, to, required)
					}
					return nil
				})
			if err != nil || lookups != 0 || attempts != 1 {
				t.Fatalf("literal routing: err=%v DNS=%d sessions=%d", err, lookups, attempts)
			}
		})
	}
}

func TestR5NextAddressLiteralInvalidBoundary(t *testing.T) {
	for _, domain := range []string{"[127.0.0.999]", "[127.01.0.1]", "[127.0.0.1:2525]", "[IPv6:127.0.0.1]", "[IPv6:::gg]", "[IPv6:::1%lo]", "[IPv6:]", "[IPv7:::1]", "[host.test]", "[::1", "::1]", "[IPv6:::1]extra", "[IPv6:::1]\r\nRCPT TO:<attacker@example.test>"} {
		t.Run(domain, func(t *testing.T) {
			if _, err := ParseRecipientAddress("reader@" + domain); err == nil {
				t.Error("malformed literal admitted by recipient parser")
			}
			lookups, attempts := 0, 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"reader@" + domain}, nil, false,
				func(context.Context, string) ([]*net.MX, error) { lookups++; return nil, nil },
				func(context.Context, string, string, string, []string, []byte, bool) error { attempts++; return nil })
			var reply *textproto.Error
			if !errors.As(err, &reply) || reply.Code != 553 || lookups != 0 || attempts != 0 {
				t.Fatalf("malformed literal must be permanent before DNS or SMTP: err=%v DNS=%d sessions=%d", err, lookups, attempts)
			}
		})
	}
	for _, input := range []string{`"not@[IPv6:::1]" <reader@example.test>, second@example.test`, "reader@[IPv6:::1] trailing", "<reader@[IPv6:::1]", "reader@[IPv6:::1]>"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseRecipientAddress(input); err == nil {
				t.Fatal("IPv6 support bypassed whole-mailbox syntax validation")
			}
		})
	}
}

func TestR5NextAddressLiteralSubmitLedgerAndMIME(t *testing.T) {
	for _, tc := range r5NextLiteralCases {
		for role, roleName := range []string{"to", "cc", "bcc"} {
			t.Run(tc.name+"/"+roleName, func(t *testing.T) {
				st := testutil.NewFakeStore()
				req := quotaTestSendRequest(uuid.New(), uuid.New())
				seedQuotaSender(t, st, &req)
				req.To, req.CC, req.BCC = nil, nil, nil
				groups := []*[]string{&req.To, &req.CC, &req.BCC}
				*groups[role] = []string{tc.input}
				svc := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop())
				job, err := svc.Submit(context.Background(), req)
				if err != nil || job == nil || !reflect.DeepEqual(job.RcptTo, []string{tc.envelope}) {
					t.Fatalf("literal submission lost envelope: job=%+v err=%v", job, err)
				}
				if (*groups[role])[0] != tc.input {
					t.Fatal("recipient input mutated")
				}
				stored, err := st.GetOutboundJob(context.Background(), job.ID)
				if err != nil || stored == nil || !reflect.DeepEqual(stored.RcptTo, []string{tc.envelope}) {
					t.Fatalf("literal not preserved in queue: %v", err)
				}
				raw, err := svc.buildQueuedMIME(context.Background(), stored)
				if err != nil {
					t.Fatal(err)
				}
				message, err := mail.ReadMessage(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				if role != 2 && message.Header.Get(roleName) != tc.envelope {
					t.Fatalf("visible recipient changed: %q", message.Header.Get(roleName))
				}
				if message.Header.Get("Bcc") != "" || (role == 2 && bytes.Contains(raw, []byte(tc.envelope))) {
					t.Fatal("literal BCC leaked into MIME")
				}
			})
		}
	}
	t.Run("invalid submission returns HTTP400", func(t *testing.T) {
		st := testutil.NewFakeStore()
		req := quotaTestSendRequest(uuid.New(), uuid.New())
		seedQuotaSender(t, st, &req)
		req.To = []string{"reader@[IPv6:::1%lo]"}
		job, err := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop()).Submit(context.Background(), req)
		if typed, ok := app.As(err); job != nil || !ok || typed.Kind != app.KindBadRequest {
			t.Fatalf("invalid address result: job=%v err=%v", job, err)
		}
	})
}

type r5NextLiteralWire struct {
	raw      []byte
	commands []string
	err      error
}

func r5NextLiteralPeer(t *testing.T, ip, recipient string, requireTLS bool) (string, <-chan r5NextLiteralWire) {
	t.Helper()
	listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatal(err)
	}
	done, joined := make(chan r5NextLiteralWire, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(joined)
		out := r5NextLiteralWire{}
		out.err = func() error {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			reader := textproto.NewReader(bufio.NewReader(conn))
			write := func(line string) error { _, err := fmt.Fprint(conn, line+"\r\n"); return err }
			read := func(want, reply string) error {
				line, err := reader.ReadLine()
				out.commands = append(out.commands, line)
				if err != nil || line != want {
					return fmt.Errorf("command=%q want=%q err=%v", line, want, err)
				}
				return write(reply)
			}
			if err := write("220 literal.test ESMTP"); err != nil {
				return err
			}
			if err := read("EHLO localhost", "250 literal.test"); err != nil {
				return err
			}
			if requireTLS {
				line, err := reader.ReadLine()
				if err == nil {
					return fmt.Errorf("required TLS emitted command over plaintext: %q", line)
				}
				return nil
			}
			if err := read("MAIL FROM:<sender@example.test>", "250 ok"); err != nil {
				return err
			}
			if err := read("RCPT TO:<"+recipient+">", "250 ok"); err != nil {
				return err
			}
			if err := read("DATA", "354 continue"); err != nil {
				return err
			}
			out.raw, err = reader.ReadDotBytes()
			if err != nil {
				return err
			}
			if err := write("250 accepted"); err != nil {
				return err
			}
			return read("QUIT", "221 bye")
		}()
		done <- out
	}()
	t.Cleanup(func() { cancel(); _ = listener.Close(); <-joined })
	return listener.Addr().String(), done
}

func TestR5NextAddressLiteralActualIPv4IPv6SMTP(t *testing.T) {
	for _, tc := range []struct{ ip, recipient string }{{"127.0.0.1", "reader@[127.0.0.1]"}, {"::1", "reader@[ipv6:::1]"}} {
		for _, required := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/TLS-%v", tc.ip, required), func(t *testing.T) {
				addr, done := r5NextLiteralPeer(t, tc.ip, tc.recipient, required)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				lookups := 0
				err := deliverDirectWith(ctx, "sender@example.test", []string{tc.recipient}, []byte("To: "+tc.recipient+"\r\nSubject: literal\r\n\r\nbody\r\n"), required,
					func(context.Context, string) ([]*net.MX, error) {
						lookups++
						return nil, errors.New("DNS must not run")
					},
					func(ctx context.Context, host, route, from string, to []string, raw []byte, tls bool) error {
						if route != net.JoinHostPort(tc.ip, "25") || host != tc.ip {
							return fmt.Errorf("incorrect literal destination: %s/%s", host, route)
						}
						return deliverDirectMX(ctx, host, addr, from, to, raw, tls)
					})
				if lookups != 0 || (err == nil) == required {
					t.Fatalf("literal SMTP err=%v TLS=%v DNS=%d", err, required, lookups)
				}
				peer := <-done
				if peer.err != nil {
					t.Fatal(peer.err)
				}
				if !required && !bytes.Contains(peer.raw, []byte(tc.recipient)) {
					t.Fatal("literal did not reach actual SMTP DATA")
				}
			})
		}
	}
}

func TestR5NextAddressLiteralNormalMXAndTerminalControls(t *testing.T) {
	t.Run("DNS MX unchanged", func(t *testing.T) {
		lookups, attempts := 0, 0
		err := deliverDirectWith(context.Background(), "sender@example.test", []string{"reader@example.test"}, nil, false,
			func(_ context.Context, domain string) ([]*net.MX, error) {
				lookups++
				if domain != "example.test" {
					t.Fatal(domain)
				}
				return []*net.MX{{Host: "mx.example.test."}}, nil
			},
			func(_ context.Context, host, addr, _ string, _ []string, _ []byte, _ bool) error {
				attempts++
				if host != "mx.example.test" || addr != "mx.example.test:25" {
					t.Fatal(host, addr)
				}
				return nil
			})
		if err != nil || lookups != 1 || attempts != 1 {
			t.Fatal(err, lookups, attempts)
		}
	})
	for name, outcome := range map[string]error{"permanent": &textproto.Error{Code: 550, Msg: "recipient refused"}, "uncertain": store.ErrOutboundUncertain, "temporary": errors.New("temporary connection failure")} {
		t.Run(name, func(t *testing.T) {
			attempts := 0
			err := deliverDirectWith(context.Background(), "sender@example.test", []string{"reader@[IPv6:::1]"}, nil, false,
				func(context.Context, string) ([]*net.MX, error) { t.Error("literal queried DNS"); return nil, nil },
				func(context.Context, string, string, string, []string, []byte, bool) error {
					attempts++
					return outcome
				})
			if !errors.Is(err, outcome) || attempts != 1 {
				t.Fatalf("literal outcome lost or retried: %v attempts=%d", err, attempts)
			}
		})
	}
	t.Run("cancellation before route", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := deliverDirectWith(ctx, "sender@example.test", []string{"reader@[127.0.0.1]"}, nil, false,
			func(context.Context, string) ([]*net.MX, error) {
				t.Error("canceled delivery queried DNS")
				return nil, nil
			},
			func(context.Context, string, string, string, []string, []byte, bool) error {
				t.Error("canceled delivery opened SMTP")
				return nil
			})
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
