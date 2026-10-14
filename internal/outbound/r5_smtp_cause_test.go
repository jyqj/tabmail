package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"tabmail/internal/store"
)

// These fixtures exchange actual SMTP commands with the standard library client
// over an in-memory pipe. No resolver, TCP dialer, listener, or SMTP account is
// involved. A fault runs only after its named protocol boundary is observed.
type r5SMTPCauseFault func(client, peer net.Conn, reader *bufio.Reader) error

func r5SMTPCauseRun(t *testing.T, ctx context.Context, phase string, fault r5SMTPCauseFault, body []byte) error {
	t.Helper()
	clientConn, peer := net.Pipe()
	done := make(chan struct{})
	var peerErr error
	t.Cleanup(func() { _ = clientConn.Close(); _ = peer.Close() })
	if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, release, err := dialSMTPWithConnector(ctx, "memory.invalid:25", func(context.Context, string, string) (net.Conn, error) {
		return clientConn, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = peer.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SMTP fixture goroutine did not terminate")
		}
	})
	go func() {
		defer close(done)
		defer peer.Close()
		peerErr = func() error {
			r := bufio.NewReader(peer)
			write := func(reply string) error {
				_, err := io.WriteString(peer, reply)
				return err
			}
			read := func(prefix string) error {
				line, err := r.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read %s: %w", prefix, err)
				}
				if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "\r\n") {
					return fmt.Errorf("expected %s, got %q", prefix, line)
				}
				return nil
			}
			if err := write("220 memory.invalid ESMTP\r\n"); err != nil {
				return err
			}
			if err := read("EHLO "); err != nil {
				return err
			}
			if err := write("250 memory.invalid\r\n"); err != nil {
				return err
			}
			for _, command := range []struct{ phase, prefix, reply string }{
				{"mail", "MAIL FROM:<from@example.test>", "250 sender OK\r\n"},
				{"rcpt", "RCPT TO:<to@example.test>", "250 recipient OK\r\n"},
				{"data", "DATA\r\n", "354 send body\r\n"},
			} {
				if err := read(command.prefix); err != nil {
					return err
				}
				if phase == command.phase {
					return fault(clientConn, peer, r)
				}
				if err := write(command.reply); err != nil {
					return err
				}
			}
			if phase == "write" {
				return fault(clientConn, peer, r)
			}
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read DATA body: %w", err)
				}
				if line == ".\r\n" {
					break
				}
			}
			if phase == "final" {
				return fault(clientConn, peer, r)
			}
			if err := write("250 accepted\r\n"); err != nil {
				return err
			}
			if err := read("QUIT\r\n"); err != nil {
				return err
			}
			if phase == "quit" {
				return fault(clientConn, peer, r)
			}
			return write("221 bye\r\n")
		}()
	}()
	client, err := smtp.NewClient(conn, "memory.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if body == nil {
		body = []byte("Subject: in-memory SMTP\r\n\r\nbody\r\n")
	}
	err = sendSMTP(client, "from@example.test", []string{"to@example.test"}, body)
	_ = client.Close()
	release()
	select {
	case <-done:
		if peerErr != nil {
			t.Fatalf("SMTP fixture: %v", peerErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP fixture did not terminate")
	}
	return smtpContextError(ctx, err)
}

func TestR5SMTPCauseFinalEOF(t *testing.T) {
	err := r5SMTPCauseRun(t, context.Background(), "final", func(net.Conn, net.Conn, *bufio.Reader) error { return nil }, nil)
	if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, io.EOF) {
		t.Fatalf("lost DATA final reply must retain uncertainty and EOF: %v", err)
	}
}

// An expired deadline can precede publication of ctx.Err(). Keeping it separate
// from the transport context makes that narrow race deterministic without sleeps.
type r5SMTPDeadlinePendingContext struct {
	context.Context
	deadline time.Time
}

func (ctx r5SMTPDeadlinePendingContext) Deadline() (time.Time, bool) { return ctx.deadline, true }

func TestR5SMTPCauseFinalDeadlineRace(t *testing.T) {
	deadline := time.Now().Add(-time.Second)
	err := r5SMTPCauseRun(t, context.Background(), "final", func(client, peer net.Conn, r *bufio.Reader) error {
		if err := client.SetReadDeadline(deadline); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, r)
		return err
	}, nil)
	ctx := r5SMTPDeadlinePendingContext{Context: context.Background(), deadline: deadline}
	if ctx.Err() != nil {
		t.Fatal("fixture must exercise the deadline-before-Err publication branch")
	}
	err = smtpContextError(ctx, err)
	var timeout net.Error
	var transport *net.OpError
	if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, os.ErrDeadlineExceeded) ||
		!errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &timeout) || !timeout.Timeout() ||
		!errors.As(err, &transport) || transport.Op != "read" || transport.Net != "pipe" {
		t.Fatalf("DATA final timeout must retain all transport and context causes: %v", err)
	}
}

func TestR5SMTPCauseFinalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := r5SMTPCauseRun(t, ctx, "final", func(client, peer net.Conn, r *bufio.Reader) error {
		cancel()
		_, err := io.Copy(io.Discard, r)
		return err
	}, nil)
	if !errors.Is(err, store.ErrOutboundUncertain) || !errors.Is(err, context.Canceled) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("canceled DATA final read must retain uncertainty and both causes: %v", err)
	}
}

func TestR5SMTPCauseDefinitiveReplies(t *testing.T) {
	for _, phase := range []string{"mail", "rcpt", "data", "final"} {
		for _, code := range []int{451, 550} {
			t.Run(fmt.Sprintf("%s/%d", phase, code), func(t *testing.T) {
				err := r5SMTPCauseRun(t, context.Background(), phase, func(client, peer net.Conn, r *bufio.Reader) error {
					_, err := fmt.Fprintf(peer, "%d definitive rejection\r\n", code)
					return err
				}, nil)
				var reply *textproto.Error
				if !errors.As(err, &reply) || reply.Code != code || errors.Is(err, store.ErrOutboundUncertain) {
					t.Fatalf("definitive %s %d reply lost its classification: %v", phase, code, err)
				}
			})
		}
	}
}

func TestR5SMTPCausePreFinalDisconnects(t *testing.T) {
	for _, phase := range []string{"mail", "rcpt", "data", "write"} {
		t.Run(phase, func(t *testing.T) {
			var body []byte
			if phase == "write" {
				body = []byte(strings.Repeat("body line\r\n", 1024))
			}
			err := r5SMTPCauseRun(t, context.Background(), phase, func(net.Conn, net.Conn, *bufio.Reader) error { return nil }, body)
			if err == nil || errors.Is(err, store.ErrOutboundUncertain) {
				t.Fatalf("pre-final %s failure must remain an ordinary delivery error: %v", phase, err)
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("pre-final %s disconnect lost transport cause: %v", phase, err)
			}
		})
	}
}

func TestR5SMTPCauseAcknowledgedDataIgnoresCleanup(t *testing.T) {
	for _, cleanup := range []string{"quit_eof", "quit_rejection", "quit_cancel"} {
		t.Run(cleanup, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := r5SMTPCauseRun(t, ctx, "quit", func(client, peer net.Conn, r *bufio.Reader) error {
				if cleanup == "quit_rejection" {
					_, err := io.WriteString(peer, "421 cleanup unavailable\r\n")
					return err
				}
				if cleanup == "quit_cancel" {
					cancel()
					_, err := io.Copy(io.Discard, r)
					return err
				}
				return nil
			}, nil)
			if err != nil {
				t.Fatalf("acknowledged DATA must survive %s: %v", cleanup, err)
			}
		})
	}
}
