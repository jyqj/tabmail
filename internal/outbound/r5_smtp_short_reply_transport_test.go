package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/store"
)

func TestR5AdvanceSMTPShortFinalTimeout(t *testing.T) {
	for _, code := range []int{250, 451, 550} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			err := r5SMTPCauseRun(t, context.Background(), "final", func(client, peer net.Conn, reader *bufio.Reader) error {
				if _, err := fmt.Fprintf(peer, "%d incomplete", code); err != nil {
					return err
				}
				if err := client.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
					return err
				}
				_, err := io.Copy(io.Discard, reader)
				return err
			}, nil)
			var timeout net.Error
			var protocol *textproto.Error
			if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, os.ErrDeadlineExceeded) ||
				!errors.As(err, &timeout) || !timeout.Timeout() || errors.As(err, &protocol) {
				t.Fatalf("truncated DATA timeout lost its actual transport cause: %v", err)
			}
		})
	}
}

type r5AdvanceReplyEndReader struct {
	data string
	err  error
}

func (r *r5AdvanceReplyEndReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if r.data == "" {
		return n, r.err
	}
	return n, nil
}

// A reader can return data and an error together; bufio may prefetch part of
// the next reply. Only the consumed reply's termination decides its authority.
func TestR5AdvanceSMTPReplyReadAhead(t *testing.T) {
	for _, code := range []int{250, 451, 550} {
		for _, ending := range []string{"", "\r\n", "\r\n450 next fragment"} {
			t.Run(fmt.Sprintf("%d/%q", code, ending), func(t *testing.T) {
				cause := errors.New("synthetic transport failure")
				source := &r5AdvanceReplyEndReader{data: fmt.Sprintf("%d reply%s", code, ending), err: cause}
				reader, classify := guardSMTPReplyReader(bufio.NewReader(source))
				_, _, err := textproto.NewReader(reader).ReadResponse(250)
				err = classify(err)
				var protocol *textproto.Error
				if ending == "" {
					if !errors.Is(err, cause) || errors.As(err, &protocol) {
						t.Fatalf("fragment promoted to SMTP status or lost underlying cause: %v", err)
					}
					return
				}
				if code == 250 {
					if err != nil {
						t.Fatalf("complete acceptance superseded by read-ahead error: %v", err)
					}
				} else if !errors.As(err, &protocol) || protocol.Code != code || errors.Is(err, cause) {
					t.Fatalf("complete rejection superseded by read-ahead error: %v", err)
				}
			})
		}
	}
}

// Exercise the exported relay and production direct-session paths on real
// owned TCP sockets. A half-close exposes EOF while still observing forbidden
// commands or message bytes after a truncated pre-DATA response.
func TestR5AdvanceSMTPShortReplyTCP(t *testing.T) {
	for _, mode := range []string{"relay", "direct"} {
		for _, spec := range []struct {
			phase string
			code  int
		}{
			{"mail", 250}, {"rcpt", 250}, {"data", 354},
			{"final", 250}, {"final", 451}, {"final", 550},
		} {
			t.Run(fmt.Sprintf("%s/%s/%d", mode, spec.phase, spec.code), func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				type result struct {
					bodyComplete bool
					extra        string
					err          error
				}
				done := make(chan result, 1)
				go func() {
					out := result{}
					out.err = func() error {
						conn, err := listener.Accept()
						if err != nil {
							return err
						}
						defer conn.Close()
						if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
							return err
						}
						reader := bufio.NewReader(conn)
						write := func(s string) error { _, err := io.WriteString(conn, s); return err }
						fragment := func() error {
							if err := write(fmt.Sprintf("%d fragment", spec.code)); err != nil {
								return err
							}
							if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
								return err
							}
							bytes, err := io.ReadAll(reader)
							out.extra = string(bytes)
							return err
						}
						if err := write("220 loopback.test ESMTP\r\n"); err != nil {
							return err
						}
						for _, step := range []struct{ phase, prefix, response string }{
							{"hello", "EHLO ", "250 loopback.test\r\n"},
							{"mail", "MAIL FROM:", "250 sender accepted\r\n"},
							{"rcpt", "RCPT TO:", "250 recipient accepted\r\n"},
							{"data", "DATA\r\n", "354 send body\r\n"},
						} {
							line, err := reader.ReadString('\n')
							if err != nil || !strings.HasPrefix(line, step.prefix) {
								return fmt.Errorf("expected %s; received %q: %w", step.prefix, line, err)
							}
							if spec.phase == step.phase {
								return fragment()
							}
							if err := write(step.response); err != nil {
								return err
							}
						}
						for {
							line, err := reader.ReadString('\n')
							if err != nil {
								return err
							}
							if line == ".\r\n" {
								out.bodyComplete = true
								return fragment()
							}
						}
					}()
					done <- out
				}()
				if mode == "relay" {
					address := listener.Addr().(*net.TCPAddr)
					err = DeliverRelay(ctx, config.Outbound{RelayHost: address.IP.String(), RelayPort: address.Port, RelayTLS: "none"}, "sender@example.test", []string{"reader@example.test"}, []byte("body\r\n"))
				} else {
					err = deliverDirectMX(ctx, "127.0.0.1", listener.Addr().String(), "sender@example.test", []string{"reader@example.test"}, []byte("body\r\n"), false)
				}
				var protocol *textproto.Error
				if !errors.Is(err, io.EOF) || errors.As(err, &protocol) || errors.Is(err, store.ErrOutboundUncertain) != (spec.phase == "final") {
					t.Errorf("short TCP %s response classification: %v", spec.phase, err)
				}
				select {
				case out := <-done:
					if out.err != nil || out.extra != "" || out.bodyComplete != (spec.phase == "final") {
						t.Errorf("continued after short response: body=%v extra=%q error=%v", out.bodyComplete, out.extra, out.err)
					}
				case <-ctx.Done():
					t.Fatal("short-reply TCP peer did not join")
				}
			})
		}
	}
}
