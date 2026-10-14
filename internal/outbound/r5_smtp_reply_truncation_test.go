package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"testing"
	"time"

	"tabmail/internal/store"
)

// bufio.ReadLine returns a final fragment even when its reader failed before
// the newline. Such a fragment must not become an authoritative SMTP status.
// These additional cases preserve the first 79-test file byte-for-byte.
func TestR5SMTPReplyByteBudgetTruncatedRejectionIsUncertain(t *testing.T) {
	for _, code := range []int{450, 550} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			p := r5NewReplyBudgetPeer(t, r5ReplyBudgetModes[3], r5ReplyBudgetSpec{finalLimit: r5SMTPReplyByteLimit + 1, finalCode: code})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sessions := 0
			var firstErr error
			err := deliverDirectWith(ctx, "sender@example.test", []string{"reader@recipient.test"}, []byte("body\r\n"), false,
				func(context.Context, string) ([]*net.MX, error) {
					return []*net.MX{{Host: "first.test."}, {Host: "second.test."}}, nil
				},
				func(ctx context.Context, host, _ string, _ string, to []string, body []byte, _ bool) error {
					sessions++
					if host != "first.test" {
						return nil // Detect an unauthorized repeat after possible acceptance.
					}
					firstErr = p.deliver(ctx, to, body)
					return firstErr
				})
			r5ReplyBudgetError(t, firstErr, true)
			var reply *textproto.Error
			if errors.As(firstErr, &reply) {
				t.Errorf("truncated %d remained a definitive SMTP rejection in the error chain", code)
			}
			if !errors.Is(err, store.ErrOutboundUncertain) || sessions != 1 {
				t.Errorf("truncated DATA rejection allowed another MX: sessions=%d error=%v", sessions, err)
			}
			if out := r5ReplyBudgetJoin(t, p); !out.bodyComplete {
				t.Errorf("truncated final response did not follow DATA: %+v", out)
			}
		})
	}
}

func TestR5SMTPReplyByteBudgetTruncatedPreliminaryStopsBody(t *testing.T) {
	for _, code := range []int{354, 450, 550} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer ln.Close()
			type observation struct {
				bodyBytes int
				err       error
			}
			peerDone := make(chan observation, 1)
			go func() {
				out := observation{}
				out.err = func() error {
					conn, err := ln.Accept()
					if err != nil {
						return err
					}
					defer conn.Close()
					stopped := make(chan struct{})
					stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(stopped) })
					defer func() {
						if !stop() {
							<-stopped
						}
					}()
					reader := bufio.NewReader(conn)
					sent := 0
					write := func(s string) error {
						n, err := io.WriteString(conn, s)
						sent += n
						return err
					}
					if err := write("220 fixture\r\n"); err != nil {
						return err
					}
					for _, exchange := range []struct{ command, reply string }{
						{"EHLO localhost\r\n", "250 fixture\r\n"},
						{"MAIL FROM:<sender@example.test>\r\n", "250 sender\r\n"},
						{"RCPT TO:<reader@recipient.test>\r\n", "250 recipient\r\n"},
						{"DATA\r\n", ""},
					} {
						line, err := reader.ReadString('\n')
						if err != nil || line != exchange.command {
							return fmt.Errorf("preliminary command=%q want=%q error=%v", line, exchange.command, err)
						}
						if err := write(exchange.reply); err != nil {
							return err
						}
					}
					// The missing byte at the limit is this reply's terminating LF.
					if err := write(r5SizedSMTPReply(code, r5SMTPReplyByteLimit+1-sent)); err != nil {
						return err
					}
					for {
						line, err := reader.ReadString('\n')
						if line == ".\r\n" {
							if err := write("250 accepted\r\n"); err != nil {
								return err
							}
							continue
						}
						if line == "QUIT\r\n" {
							return write("221 bye\r\n")
						}
						out.bodyBytes += len(line)
						if err != nil {
							// Closing before any body is the required failure path.
							return nil
						}
					}
				}()
				peerDone <- out
			}()
			err = deliverDirectMX(ctx, "127.0.0.1", ln.Addr().String(), "sender@example.test", []string{"reader@recipient.test"}, []byte("must not be transmitted\r\n"), false)
			r5ReplyBudgetError(t, err, false)
			var reply *textproto.Error
			if errors.As(err, &reply) {
				t.Errorf("truncated preliminary %d became an authoritative rejection", code)
			}
			select {
			case out := <-peerDone:
				if out.bodyBytes != 0 || out.err != nil {
					t.Errorf("body was sent after truncated preliminary response: %+v", out)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("truncated preliminary peer did not join")
			}
		})
	}
}
