package outbound

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"tabmail/internal/store"
)

// A real TCP peer fails only after the selected greeting command is observed.
// HELO cases first reject EHLO so the standard library's original fallback is
// exercised. No TLS, MAIL or DATA command is permitted after a failed greeting.
func r5DirectGreetingPeer(t *testing.T, phase, outcome string) (string, <-chan struct{}, <-chan error) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	done := make(chan error, 1)
	stop := make(chan struct{})
	finished := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		_ = l.Close()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("greeting fixture did not stop")
		}
	})
	go func() {
		defer close(finished)
		done <- func() error {
			conn, err := l.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				return err
			}
			closed := make(chan struct{})
			defer close(closed)
			go func() {
				select {
				case <-stop:
					_ = conn.Close()
				case <-closed:
				}
			}()
			r := bufio.NewReader(conn)
			write := func(s string) error { _, err := io.WriteString(conn, s); return err }
			read := func(want string) error {
				line, err := r.ReadString('\n')
				if err != nil {
					return err
				}
				if line != want {
					return fmt.Errorf("command = %q, want %q", line, want)
				}
				return nil
			}
			if err := write("220 local.test ESMTP\r\n"); err != nil {
				return err
			}
			if err := read("EHLO localhost\r\n"); err != nil {
				return err
			}
			if phase == "helo" {
				if err := write("500 EHLO unavailable\r\n"); err != nil {
					return err
				}
				if err := read("HELO localhost\r\n"); err != nil {
					return err
				}
			}
			close(reached)
			if outcome == "eof" {
				return nil
			}
			if outcome != "stall" {
				if err := write(outcome + " greeting refused\r\n"); err != nil {
					return err
				}
			}
			line, err := r.ReadString('\n')
			if line != "" || !errors.Is(err, io.EOF) {
				return fmt.Errorf("failed greeting continued or socket remained open: command=%q error=%v", line, err)
			}
			return nil
		}()
	}()
	return l.Addr().String(), reached, done
}

func r5WaitGreetingPeer(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("greeting peer did not observe connection close")
	}
}

func TestR5DirectGreetingReplyCauses(t *testing.T) {
	for _, requireTLS := range []bool{false, true} {
		for _, outcome := range []string{"451", "550", "eof"} {
			t.Run(fmt.Sprintf("requireTLS=%v/%s", requireTLS, outcome), func(t *testing.T) {
				addr, _, peerDone := r5DirectGreetingPeer(t, "helo", outcome)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				err := deliverDirectMX(ctx, "127.0.0.1", addr, "from@example.test", []string{"to@example.test"}, nil, requireTLS)
				r5WaitGreetingPeer(t, peerDone)
				if err == nil || errors.Is(err, store.ErrOutboundUncertain) {
					t.Fatalf("pre-envelope failure classification changed: %v", err)
				}
				if outcome == "eof" {
					if !errors.Is(err, io.EOF) {
						t.Fatalf("greeting EOF cause lost: %v", err)
					}
				} else {
					var reply *textproto.Error
					if !errors.As(err, &reply) || fmt.Sprint(reply.Code) != outcome || reply.Msg != "greeting refused" {
						t.Fatalf("greeting reply cause lost: %v", err)
					}
				}
			})
		}
	}
}

func TestR5DirectGreetingContextCauses(t *testing.T) {
	for _, requireTLS := range []bool{false, true} {
		for _, mode := range []string{"cancel_ehlo", "cancel_helo", "deadline_helo"} {
			t.Run(fmt.Sprintf("requireTLS=%v/%s", requireTLS, mode), func(t *testing.T) {
				phase := "helo"
				if strings.HasSuffix(mode, "ehlo") {
					phase = "ehlo"
				}
				addr, reached, peerDone := r5DirectGreetingPeer(t, phase, "stall")
				deadline := strings.HasPrefix(mode, "deadline")
				budget := 2 * time.Second
				if deadline {
					budget = 150 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					done <- deliverDirectMX(ctx, "127.0.0.1", addr, "from@example.test", []string{"to@example.test"}, nil, requireTLS)
				}()
				select {
				case <-reached:
				case err := <-done:
					t.Fatalf("greeting barrier missing: %v", err)
				case <-time.After(time.Second):
					t.Fatal("greeting barrier missing")
				}
				want := context.DeadlineExceeded
				if !deadline {
					want = context.Canceled
					cancel()
				}
				select {
				case err := <-done:
					var transport *net.OpError
					if !errors.Is(err, want) || !errors.As(err, &transport) || errors.Is(err, store.ErrOutboundUncertain) {
						t.Fatalf("greeting must retain context and transport causes before DATA: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("context did not interrupt greeting")
				}
				r5WaitGreetingPeer(t, peerDone)
			})
		}
	}
}
