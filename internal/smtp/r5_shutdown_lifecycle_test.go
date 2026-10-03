package smtp

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
)

// Requires Go 1.25.7+ and GODEBUG=asynctimerchan=0 for synctest's timer model.
// Tests use the replaced third_party/go-smtp through the production Server path
// with channel-controlled in-memory transports: no sockets, processes or PG.
// Each leaf owns a synctest bubble; negative assertions first run every runnable
// owner to a durable blocking point. Timeouts are watchdogs, never ordering.
// This does not claim zero process goroutines: metrics.init starts a package-
// global ticker goroutine outside these bubbles before any test runs.
func TestR5SMTPShutdownLifecycle(t *testing.T) {
	t.Run("shutdown_before_start_seals_admission", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := r5LifecycleServer()
			s.listen = func(context.Context, string, string) (net.Listener, error) {
				t.Error("a shutdown server attempted to bind")
				return nil, errors.New("unexpected listen")
			}
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.Start(context.Background()); !errors.Is(err, gosmtp.ErrServerClosed) {
				t.Fatalf("late Start = %v", err)
			}
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatalf("repeated Shutdown = %v", err)
			}
		})
	})

	t.Run("pending_start_is_owned_and_cancellable", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := r5LifecycleServer()
			entered, stopped := make(chan struct{}), make(chan struct{})
			s.listen = func(ctx context.Context, _, _ string) (net.Listener, error) {
				close(entered)
				<-ctx.Done()
				close(stopped)
				return nil, ctx.Err()
			}
			start := r5Start(s, context.Background())
			r5Await(t, entered)
			if err := s.Start(context.Background()); !errors.Is(err, ErrServerStarted) {
				t.Fatalf("duplicate Start = %v", err)
			}
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			r5Await(t, stopped)
			if err := r5Result(t, start); !errors.Is(err, context.Canceled) {
				t.Fatalf("pending Start = %v", err)
			}
		})
	})

	t.Run("noncooperative_start_timeout_retains_owner", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := r5LifecycleServer()
			entered, release := make(chan struct{}), make(chan struct{})
			defer r5Close(release)
			ln := r5NewListener()
			s.listen = func(context.Context, string, string) (net.Listener, error) {
				close(entered)
				<-release // Simulate a listener factory ignoring cancellation.
				return ln, nil
			}
			start := r5Start(s, context.Background())
			r5Await(t, entered)
			ctx, cancel := context.WithDeadline(context.Background(), time.Time{})
			defer cancel()
			if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("noncooperative pending admission = %v", err)
			}
			r5NotClosed(t, s.drained, "pending bind was declared drained")
			if err := s.Start(context.Background()); !errors.Is(err, gosmtp.ErrServerClosed) {
				t.Fatalf("undrained server allowed a new generation: %v", err)
			}
			close(release)
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
			if ln.accepts.Load() != 0 {
				t.Fatal("late bind admitted a connection after shutdown")
			}
			r5Await(t, ln.closed)
		})
	})

	t.Run("startup_failure_and_independent_instance", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			want := errors.New("fixture bind failure")
			s := r5LifecycleServer()
			s.listen = func(context.Context, string, string) (net.Listener, error) { return nil, want }
			if err := s.Start(context.Background()); !errors.Is(err, want) {
				t.Fatalf("Start = %v", err)
			}
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			fresh, ln := r5LifecycleServer(), r5NewListener()
			fresh.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			start := r5Start(fresh, context.Background())
			r5Await(t, ln.acceptEntered)
			if err := fresh.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("real_listener_close_error_is_not_success", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, ln := r5LifecycleServer(), r5NewListener()
			want := errors.New("fixture listener close error")
			ln.closeErr = want
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			start := r5Start(s, context.Background())
			r5Await(t, ln.acceptEntered)
			if err := s.Shutdown(context.Background()); !errors.Is(err, want) {
				t.Fatalf("close failure suppressed: %v", err)
			}
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
			if err := s.Shutdown(context.Background()); !errors.Is(err, want) {
				t.Fatalf("repeated Shutdown lost failure: %v", err)
			}
		})
	})

	t.Run("backend_session_uses_owned_work_context", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sess := &session{backend: &backend{ctx: ctx}}
			if sess.workContext() != ctx {
				t.Fatal("production session replaced its owned backend context")
			}
			cancel()
			if !errors.Is(sess.workContext().Err(), context.Canceled) {
				t.Fatal("production session context lost shutdown cancellation")
			}
		})
	})

	t.Run("full_connection_limit_close_unblocks_accept", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, raw := r5LifecycleServer(), r5NewListener()
			s.cfg.MaxConnections = 1
			ln := s.wrapListener(raw)
			conn := r5NewConn("")
			raw.incoming <- conn
			first, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			accepted := make(chan error, 1)
			go func() {
				close(entered)
				_, err := ln.Accept()
				accepted <- err
			}()
			r5Await(t, entered)
			synctest.Wait() // The second Accept has run and is blocked on capacity.
			if raw.accepts.Load() != 1 || len(ln.sem) != 1 {
				t.Fatal("capacity waiter did not block before the underlying Accept")
			}
			if err := ln.Close(); err != nil {
				t.Fatal(err)
			}
			if err := r5Result(t, accepted); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("capacity waiter = %v", err)
			}
			var closes sync.WaitGroup
			closes.Add(2)
			for range 2 {
				go func() { defer closes.Done(); _ = first.Close() }()
			}
			closes.Wait()
			if len(ln.sem) != 0 || conn.closeCount.Load() != 1 {
				t.Fatalf("permit count = %d, raw closes = %d", len(ln.sem), conn.closeCount.Load())
			}
			if raw.accepts.Load() != 1 {
				t.Fatal("full-capacity waiter reached the underlying listener")
			}
		})
	})

	t.Run("successful_accept_racing_close_is_not_admitted", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, raw := r5LifecycleServer(), r5NewListener()
			s.cfg.MaxConnections = 1
			raw.acceptRelease = make(chan struct{})
			defer r5Close(raw.acceptRelease)
			conn := r5NewConn("")
			raw.incoming <- conn
			ln := s.wrapListener(raw)
			accepted := make(chan error, 1)
			go func() { _, err := ln.Accept(); accepted <- err }()
			r5Await(t, raw.acceptEntered)
			if err := ln.Close(); err != nil {
				t.Fatal(err)
			}
			close(raw.acceptRelease)
			if err := r5Result(t, accepted); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("late successful Accept = %v", err)
			}
			r5Await(t, conn.closed)
			if len(ln.sem) != 0 || conn.closeCount.Load() != 1 {
				t.Fatal("late Accept leaked or double-released its permit")
			}
		})
	})

	for _, cooperative := range []bool{false, true} {
		name := "noncooperative_DATA_and_Logout_are_really_joined"
		if cooperative {
			name = "run_cancel_preserves_DATA_until_shutdown_budget_expires"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, ln := r5LifecycleServer(), r5NewListener()
				s.cfg.MaxConnections = 1
				s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
				sess := &r5LifecycleSession{
					entered: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{}),
					logout: make(chan struct{}), logoutRelease: make(chan struct{}), cooperative: cooperative,
					workContext: func() context.Context { return s.backend.ctx },
					workOwner:   s.backend,
				}
				defer r5Close(sess.release)
				defer r5Close(sess.logoutRelease)
				s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) { return sess, nil })
				conn := r5NewConn("EHLO fixture\r\nMAIL FROM:<sender@example.test>\r\nRCPT TO:<recipient@example.test>\r\nDATA\r\nSubject: fixture\r\n\r\nbody\r\n.\r\n")
				ln.incoming <- conn
				runCtx, cancelRun := context.WithCancel(context.Background())
				defer cancelRun()
				start := r5Start(s, runCtx)
				r5Await(t, sess.entered)
				cancelRun()
				r5Await(t, conn.closed)
				if sess.workContext().Err() != nil {
					t.Fatal("run cancellation destroyed the independent DATA shutdown budget")
				}
				if err := r5Result(t, start); err != nil {
					t.Fatal(err)
				}
				r5NotClosed(t, s.drained, "Serve/socket close was mistaken for DATA completion")
				r5NotClosed(t, sess.logout, "socket close invoked concurrent Logout")
				shutdownCtx, expire := context.WithCancel(context.Background())
				expire() // An explicit barrier, not a sleep or a new grace period.
				if err := s.Shutdown(shutdownCtx); !errors.Is(err, context.Canceled) {
					t.Fatalf("active DATA/Logout shutdown = %v", err)
				}
				if !errors.Is(sess.workContext().Err(), context.Canceled) {
					t.Fatal("expired shutdown budget did not cancel cooperative backend work")
				}
				if !cooperative {
					r5NotClosed(t, sess.reset, "noncooperative DATA was marked complete")
					close(sess.release)
				}
				r5Await(t, sess.reset)
				r5Await(t, sess.logout)
				r5NotClosed(t, s.drained, "blocked Logout was declared drained")
				if err := s.Start(context.Background()); !errors.Is(err, gosmtp.ErrServerClosed) {
					t.Fatalf("new generation while Logout is active = %v", err)
				}
				close(sess.logoutRelease)
				if err := s.Shutdown(context.Background()); err != nil {
					t.Fatalf("continued real join = %v", err)
				}
				if sess.logoutCount.Load() != 1 || conn.closeCount.Load() != 1 {
					t.Fatalf("Logout = %d, raw Close = %d", sess.logoutCount.Load(), conn.closeCount.Load())
				}
			})
		})
	}

	t.Run("BDAT_data_outliving_transport_is_joined", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, ln := r5LifecycleServer(), r5NewListener()
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			sess := &r5LifecycleSession{
				entered: make(chan struct{}), release: make(chan struct{}), reset: make(chan struct{}),
				logout: make(chan struct{}), logoutRelease: make(chan struct{}), chunked: true,
				workContext: func() context.Context { return s.backend.ctx }, workOwner: s.backend,
			}
			defer r5Close(sess.release)
			close(sess.logoutRelease)
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) { return sess, nil })
			conn := r5NewConn("EHLO fixture\r\nMAIL FROM:<sender@example.test>\r\nRCPT TO:<recipient@example.test>\r\nBDAT 1\r\nx")
			ln.incoming <- conn
			start := r5Start(s, context.Background())
			r5Await(t, sess.entered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("in-flight BDAT = %v", err)
			}
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
			r5NotClosed(t, sess.logout, "fork Logout escaped its live BDAT owner")
			r5NotClosed(t, s.drained, "connection shutdown hid the unjoined BDAT goroutine")
			close(sess.release)
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			r5Await(t, sess.logout)
			if s.backend.beginWork() {
				s.backend.workWG.Done()
				t.Fatal("late BDAT callback admitted new DB work after drain")
			}
			// Exercise the production Data gate, not only the fake session: reject
			// before touching the deliberately nil ingest dependency.
			late := &session{backend: s.backend}
			var smtpError *gosmtp.SMTPError
			if err := late.Data(strings.NewReader("fixture")); !errors.As(err, &smtpError) || smtpError.Code != 451 {
				t.Fatalf("late production DATA = %v", err)
			}
		})
	})

	t.Run("fork_BDAT_delayed_business_entry_is_owned", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, ln := r5LifecycleServer(), r5NewListener()
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			dispatched, entry := make(chan struct{}), make(chan struct{})
			loggedOut := make(chan struct{})
			dataResult := make(chan error, 1)
			defer r5Close(entry)
			production := &session{backend: s.backend, logger: zerolog.Nop()}
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) {
				return &r5CallbackSession{data: func(r io.Reader) error {
					close(dispatched)
					<-entry // Before production Data's backend.beginWork registration.
					err := production.Data(r)
					dataResult <- err
					return err
				}, logout: func() { close(loggedOut) }}, nil
			})
			ln.incoming <- r5NewConn("EHLO fixture\r\nMAIL FROM:<a@example.test>\r\nRCPT TO:<b@example.test>\r\nBDAT 0\r\n")
			start := r5Start(s, context.Background())
			r5Await(t, dispatched)
			stopped := make(chan error, 1)
			go func() { stopped <- s.Shutdown(context.Background()) }()
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
			r5NotClosed(t, loggedOut, "Logout ran before delayed BDAT business entry")
			r5NotClosed(t, s.drained, "outer shutdown relied only on backend active calls")
			r5ResultPending(t, stopped, "retained Shutdown returned before delayed entry")
			s.backend.workMu.Lock()
			sealed := s.backend.workClosed
			s.backend.workMu.Unlock()
			if sealed {
				t.Fatal("backend fence sealed before the fork's delayed-entry owner joined")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("repeat outer Shutdown bypassed the retained join: %v", err)
			}
			close(entry)
			if err := r5Result(t, dataResult); !errors.Is(err, gosmtp.ErrDataReset) {
				t.Fatalf("delayed production Data did not observe the closed transaction pipe: %v", err)
			}
			if err := r5Result(t, stopped); err != nil {
				t.Fatal(err)
			}
			r5Await(t, loggedOut)
		})
	})

	t.Run("fork_BDAT_post_Data_panic_tail_is_owned", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, ln := r5LifecycleServer(), r5NewListener()
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			dataReturned, tailEntered := make(chan struct{}), make(chan struct{})
			tailRelease, loggedOut := make(chan struct{}), make(chan struct{})
			defer r5Close(tailRelease)
			s.inner.ErrorLog = &r5TailLogger{entered: tailEntered, release: tailRelease}
			s.inner.Backend = gosmtp.BackendFunc(func(*gosmtp.Conn) (gosmtp.Session, error) {
				return &r5CallbackSession{data: func(io.Reader) error {
					defer close(dataReturned)
					panic("fixture fork panic-tail barrier")
				}, logout: func() { close(loggedOut) }}, nil
			})
			ln.incoming <- r5NewConn("EHLO fixture\r\nMAIL FROM:<a@example.test>\r\nRCPT TO:<b@example.test>\r\nBDAT 0\r\n")
			start := r5Start(s, context.Background())
			r5Await(t, dataReturned)
			r5Await(t, tailEntered) // Inside the dependency's recovery/result/pipe tail.
			stopped := make(chan error, 1)
			go func() { stopped <- s.Shutdown(context.Background()) }()
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
			r5NotClosed(t, loggedOut, "Logout escaped the live dependency tail")
			r5NotClosed(t, s.drained, "Data return was mistaken for complete fork ownership")
			r5ResultPending(t, stopped, "Shutdown returned while dependency tail remained live")
			close(tailRelease)
			if err := r5Result(t, stopped); err != nil {
				t.Fatal(err)
			}
			r5Await(t, loggedOut)
		})
	})

	t.Run("production_DATA_Reset_while_reader_blocked", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			be := &backend{ctx: context.Background()}
			sess := &session{backend: be, from: "a@example.test", recipients: []string{"b@example.test"}, logger: zerolog.Nop()}
			r := &r5BlockedReader{reader: strings.NewReader("fixture"), entered: make(chan struct{}), release: make(chan struct{})}
			defer r5Close(r.release)
			data := make(chan error, 1)
			go func() { data <- sess.Data(r) }()
			r5Await(t, r.entered) // Production Data is admitted and blocked in ReadAll.
			reset := make(chan struct{})
			go func() { sess.Reset(); close(reset) }()
			r5Await(t, reset)
			r5ResultPending(t, data, "production Data finished before its reader was released")
			sealed := make(chan struct{})
			go func() { be.sealAndWait(); close(sealed) }()
			r5NotClosed(t, sealed, "backend owner ignored the live production Data call")
			close(r.release)
			var smtpError *gosmtp.SMTPError
			if err := r5Result(t, data); !errors.As(err, &smtpError) || smtpError.Code != 554 {
				t.Fatalf("Data did not observe the completed Reset snapshot: %v", err)
			}
			r5Await(t, sealed)
		})
	})

	t.Run("noncooperative_socket_close_is_owned", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s, ln := r5LifecycleServer(), r5NewListener()
			conn := r5NewConn("")
			conn.closeRelease = make(chan struct{})
			defer r5Close(conn.closeRelease)
			ln.incoming <- conn
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			start := r5Start(s, context.Background())
			r5Await(t, conn.readEntered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("blocked raw Close = %v", err)
			}
			r5Await(t, conn.closeEntered)
			r5NotClosed(t, s.drained, "blocking Close owner detached")
			close(conn.closeRelease)
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := r5Result(t, start); err != nil {
				t.Fatal(err)
			}
		})
	})
}

func r5LifecycleServer() *Server {
	return NewServer(config.SMTP{Domain: "mx.fixture", MaxRecipients: 10, MaxMessageBytes: 1024}, nil, nil, zerolog.Nop())
}

func r5Start(s *Server, ctx context.Context) <-chan error {
	result := make(chan error, 1)
	go func() { result <- s.Start(ctx) }()
	return result
}

func r5Await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle barrier not reached")
	}
}

func r5Result(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle result not returned")
		return nil
	}
}

func r5NotClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	synctest.Wait()
	select {
	case <-ch:
		t.Fatal(msg)
	default:
	}
}

func r5ResultPending(t *testing.T, ch <-chan error, msg string) {
	t.Helper()
	synctest.Wait()
	select {
	case err := <-ch:
		t.Fatalf("%s: %v", msg, err)
	default:
	}
}

func r5Close(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

type r5LifecycleListener struct {
	incoming      chan net.Conn
	closed        chan struct{}
	acceptEntered chan struct{}
	acceptRelease chan struct{}
	acceptOnce    sync.Once
	closeOnce     sync.Once
	accepts       atomic.Int32
	closeErr      error
}

func r5NewListener() *r5LifecycleListener {
	return &r5LifecycleListener{incoming: make(chan net.Conn, 1), closed: make(chan struct{}), acceptEntered: make(chan struct{})}
}

func (l *r5LifecycleListener) Accept() (net.Conn, error) {
	l.accepts.Add(1)
	l.acceptOnce.Do(func() { close(l.acceptEntered) })
	if l.acceptRelease != nil {
		<-l.acceptRelease
		return <-l.incoming, nil // Deliberately return success after Close.
	}
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *r5LifecycleListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return l.closeErr
}
func (l *r5LifecycleListener) Addr() net.Addr { return r5LifecycleAddr("fixture") }

type r5LifecycleAddr string

func (a r5LifecycleAddr) Network() string { return "in-memory" }
func (a r5LifecycleAddr) String() string  { return string(a) }

type r5LifecycleConn struct {
	input        *strings.Reader
	closed       chan struct{}
	readEntered  chan struct{}
	closeEntered chan struct{}
	closeRelease chan struct{}
	readOnce     sync.Once
	closeOnce    sync.Once
	closeCount   atomic.Int32
}

func r5NewConn(input string) *r5LifecycleConn {
	return &r5LifecycleConn{input: strings.NewReader(input), closed: make(chan struct{}), readEntered: make(chan struct{}), closeEntered: make(chan struct{})}
}

func (c *r5LifecycleConn) Read(p []byte) (int, error) {
	c.readOnce.Do(func() { close(c.readEntered) })
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	if c.input.Len() != 0 {
		return c.input.Read(p)
	}
	<-c.closed
	return 0, net.ErrClosed
}
func (c *r5LifecycleConn) Write(p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
		return len(p), nil
	}
}
func (c *r5LifecycleConn) Close() error {
	c.closeCount.Add(1)
	c.closeOnce.Do(func() {
		close(c.closeEntered)
		if c.closeRelease != nil {
			<-c.closeRelease
		}
		close(c.closed)
	})
	return nil
}
func (c *r5LifecycleConn) LocalAddr() net.Addr              { return r5LifecycleAddr("local") }
func (c *r5LifecycleConn) RemoteAddr() net.Addr             { return r5LifecycleAddr("remote") }
func (c *r5LifecycleConn) SetDeadline(time.Time) error      { return nil }
func (c *r5LifecycleConn) SetReadDeadline(time.Time) error  { return nil }
func (c *r5LifecycleConn) SetWriteDeadline(time.Time) error { return nil }

type r5LifecycleSession struct {
	entered, release, reset, logout, logoutRelease chan struct{}
	cooperative                                    bool
	chunked                                        bool
	workOwner                                      *backend
	workContext                                    func() context.Context
	dataEntered                                    bool // Published to Reset by the fork's DATA join.
	resetOnce                                      sync.Once
	logoutCount                                    atomic.Int32
}

func (s *r5LifecycleSession) Mail(string, *gosmtp.MailOptions) error { return nil }
func (s *r5LifecycleSession) Rcpt(string, *gosmtp.RcptOptions) error { return nil }
func (s *r5LifecycleSession) Data(r io.Reader) error {
	if !s.workOwner.beginWork() {
		return errors.New("fixture backend sealed")
	}
	defer s.workOwner.workWG.Done()
	var err error
	if s.chunked {
		_, err = io.ReadFull(r, make([]byte, 1))
	} else {
		_, err = io.Copy(io.Discard, r)
	}
	if err != nil {
		return err
	}
	s.dataEntered = true
	close(s.entered)
	if s.cooperative {
		<-s.workContext().Done()
		return s.workContext().Err()
	}
	<-s.release
	// A durable result is deliberately still success after context cancellation.
	// This test does not assert receipt delivery or replay any ingest operation.
	return nil
}
func (s *r5LifecycleSession) Reset() {
	if s.dataEntered {
		s.resetOnce.Do(func() { close(s.reset) })
	}
}
func (s *r5LifecycleSession) Logout() error {
	s.logoutCount.Add(1)
	close(s.logout)
	<-s.logoutRelease
	return nil
}

type r5CallbackSession struct {
	data   func(io.Reader) error
	logout func()
}

func (*r5CallbackSession) Mail(string, *gosmtp.MailOptions) error { return nil }
func (*r5CallbackSession) Rcpt(string, *gosmtp.RcptOptions) error { return nil }
func (s *r5CallbackSession) Data(r io.Reader) error               { return s.data(r) }
func (*r5CallbackSession) Reset()                                 {}
func (s *r5CallbackSession) Logout() error                        { s.logout(); return nil }

type r5TailLogger struct {
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (l *r5TailLogger) Printf(string, ...interface{}) {
	l.once.Do(func() { close(l.entered); <-l.release })
}
func (*r5TailLogger) Println(...interface{}) {}

type r5BlockedReader struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *r5BlockedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.reader.Read(p)
}
