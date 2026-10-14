package smtp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosmtp "github.com/emersion/go-smtp"
)

// Real TCP is intentionally outside synctest. Positive channel barriers and a
// returned deadline error establish ordering; timers only bound failed joins.
func r5CloudWire(t *testing.T, c net.Conn, r *bufio.Reader, command, prefix string) {
	t.Helper()
	if command != "" {
		if _, err := io.WriteString(c, command+"\r\n"); err != nil {
			t.Fatal(err)
		}
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("response %q, want %s", line, prefix)
		}
		if len(line) < 4 || line[3] != '-' {
			return
		}
	}
}

func r5CloudOpen(t *testing.T, s *Server) (net.Conn, *bufio.Reader) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
	started := r5Start(s, context.Background())
	r5Await(t, s.ready)
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("cleanup join: %v", err)
		}
		if err := r5Result(t, started); err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	r := bufio.NewReader(c)
	r5CloudWire(t, c, r, "", "220")
	r5CloudWire(t, c, r, "EHLO loopback.example.test", "250")
	r5CloudWire(t, c, r, "MAIL FROM:<sender@example.test>", "250")
	r5CloudWire(t, c, r, "RCPT TO:<recipient@example.test>", "250")
	return c, r
}

func r5CloudDeadline(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v; want deadline", err)
	}
	select {
	case <-s.drained:
		t.Fatal("deadline falsely declared drained")
	default:
	}
	if !errors.Is(s.backend.ctx.Err(), context.Canceled) {
		t.Fatalf("work context: %v", s.backend.ctx.Err())
	}
	// This models the caller's completion contract, not a production PG/Redis
	// close: an error must leave dependencies retained until the subsequent join.
}

func TestR5CloudLoopbackSlowReadCancellation(t *testing.T) {
	for _, mode := range []string{"DATA", "BDAT"} {
		t.Run(mode, func(t *testing.T) {
			s := r5LifecycleServer()
			first, canceled, logout := make(chan struct{}), make(chan struct{}), make(chan struct{})
			gate := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			defer release()
			readResult := make(chan error, 1)
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) {
				return &r5CallbackSession{
					data: func(r io.Reader) error {
						if !s.backend.beginWork() {
							return errors.New("sealed")
						}
						defer s.backend.workWG.Done()
						if _, err := io.ReadFull(r, make([]byte, 1)); err != nil {
							return err
						}
						close(first)
						_, err := io.Copy(io.Discard, r)
						readResult <- err
						<-s.backend.ctx.Done()
						close(canceled)
						<-gate
						return s.backend.ctx.Err()
					}, logout: func() { close(logout) },
				}, nil
			})
			c, r := r5CloudOpen(t, s)
			if mode == "DATA" {
				r5CloudWire(t, c, r, "DATA", "354")
				_, err := io.WriteString(c, "x\r\n")
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := io.WriteString(c, "BDAT 2 LAST\r\nx")
				if err != nil {
					t.Fatal(err)
				}
			}
			r5Await(t, first)
			r5CloudDeadline(t, s)
			r5Await(t, canceled)
			if err := r5Result(t, readResult); err == nil {
				t.Fatal("incomplete body falsely reached EOF success")
			}
			select {
			case <-logout:
				t.Fatal("Logout preceded Data return")
			default:
			}
			release()
			r5CloudJoin(t, s)
			r5Await(t, logout)
		})
	}
}

func TestR5CloudLoopbackAcceptanceDisconnectLogoutJoin(t *testing.T) {
	for _, mode := range []string{"DATA", "BDAT"} {
		t.Run(mode, func(t *testing.T) {
			s := r5LifecycleServer()
			accepted, logout := make(chan struct{}), make(chan struct{})
			gate := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			defer release()
			var calls, logoutCalls atomic.Int32
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) {
				return &r5CallbackSession{
					data: func(r io.Reader) error {
						calls.Add(1)
						if _, err := io.Copy(io.Discard, r); err != nil {
							return err
						}
						close(accepted)
						return nil
					},
					logout: func() { logoutCalls.Add(1); close(logout); <-gate },
				}, nil
			})
			c, r := r5CloudOpen(t, s)
			body := "Subject: synthetic\r\n\r\nhello\r\n"
			if mode == "DATA" {
				r5CloudWire(t, c, r, "DATA", "354")
				r5CloudWire(t, c, r, body+".", "250")
			} else {
				r5CloudWire(t, c, r, "BDAT "+strconv.Itoa(len(body))+" LAST\r\n"+strings.TrimSuffix(body, "\r\n"), "250")
			}
			r5Await(t, accepted) // Backend success plus observed 250, no durable ledger claim.
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			r5Await(t, logout)
			r5CloudDeadline(t, s)
			if calls.Load() != 1 || logoutCalls.Load() != 1 {
				t.Fatalf("Data=%d Logout=%d", calls.Load(), logoutCalls.Load())
			}
			release()
			r5CloudJoin(t, s)
			select {
			case <-s.drained:
			default:
				t.Fatal("successful join missing drained")
			}
		})
	}
}

// A synthetic backend can decide success before the client observes a reply.
// This verifies ownership through disconnect, not durable acceptance/retry rules.
func TestR5CloudLoopbackAcceptedBeforeReplyDisconnect(t *testing.T) {
	for _, mode := range []string{"DATA", "BDAT"} {
		t.Run(mode, func(t *testing.T) {
			s := r5LifecycleServer()
			decided, logout := make(chan struct{}), make(chan struct{})
			dataGate, logoutGate := make(chan struct{}), make(chan struct{})
			var dataOnce, logoutOnce sync.Once
			releaseData := func() { dataOnce.Do(func() { close(dataGate) }) }
			releaseLogout := func() { logoutOnce.Do(func() { close(logoutGate) }) }
			defer releaseData()
			defer releaseLogout()
			var calls atomic.Int32
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) {
				return &r5CallbackSession{
					data: func(r io.Reader) error {
						calls.Add(1)
						if _, err := io.Copy(io.Discard, r); err != nil {
							return err
						}
						close(decided)
						<-dataGate
						return nil
					},
					logout: func() { close(logout); <-logoutGate },
				}, nil
			})
			c, r := r5CloudOpen(t, s)
			body := "Subject: synthetic\r\n\r\nhello\r\n"
			if mode == "DATA" {
				r5CloudWire(t, c, r, "DATA", "354")
				if _, err := io.WriteString(c, body+".\r\n"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := io.WriteString(c, "BDAT "+strconv.Itoa(len(body))+" LAST\r\n"+body); err != nil {
					t.Fatal(err)
				}
			}
			r5Await(t, decided)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			} // No final reply was read.
			r5CloudDeadline(t, s)
			select {
			case <-logout:
				t.Fatal("Logout escaped live success decision owner")
			default:
			}
			releaseData()
			r5Await(t, logout)
			r5CloudDeadline(t, s) // Data returned nil; Logout still owns its tail.
			releaseLogout()
			r5CloudJoin(t, s)
			if calls.Load() != 1 {
				t.Fatalf("Data called %d times", calls.Load())
			}
		})
	}
}

func r5CloudJoin(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
