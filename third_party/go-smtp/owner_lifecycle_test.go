package smtp

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// These regression tests use in-memory transports only. Deadlines below are
// failure watchdogs, never scheduling barriers. No test uses time.Sleep.
type ownerConn struct {
	r         *io.PipeReader
	w         *io.PipeWriter
	responses chan string
	closed    chan struct{}
	once      sync.Once
}

func newOwnerConn() *ownerConn {
	r, w := io.Pipe()
	return &ownerConn{r: r, w: w, responses: make(chan string, 128), closed: make(chan struct{})}
}
func (c *ownerConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *ownerConn) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimSuffix(string(p), "\r\n"), "\r\n") {
		c.responses <- line
	}
	return len(p), nil
}
func (c *ownerConn) Close() error {
	c.once.Do(func() { close(c.closed); c.r.CloseWithError(net.ErrClosed); c.w.CloseWithError(net.ErrClosed) })
	return nil
}
func (*ownerConn) LocalAddr() net.Addr              { return ownerAddr("local") }
func (*ownerConn) RemoteAddr() net.Addr             { return ownerAddr("remote") }
func (*ownerConn) SetDeadline(time.Time) error      { return nil }
func (*ownerConn) SetReadDeadline(time.Time) error  { return nil }
func (*ownerConn) SetWriteDeadline(time.Time) error { return nil }

type ownerAddr string

func (a ownerAddr) Network() string { return "memory" }
func (a ownerAddr) String() string  { return string(a) }

type ownerListener struct {
	conn   chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *ownerListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conn:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *ownerListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*ownerListener) Addr() net.Addr { return ownerAddr("listener") }

type ownerSession struct {
	data   func(io.Reader) error
	reset  func()
	logout func()
}

func (*ownerSession) Mail(string, *MailOptions) error { return nil }
func (*ownerSession) Rcpt(string, *RcptOptions) error { return nil }
func (s *ownerSession) Data(r io.Reader) error        { return s.data(r) }
func (s *ownerSession) Reset() {
	if s.reset != nil {
		s.reset()
	}
}
func (s *ownerSession) Logout() error {
	if s.logout != nil {
		s.logout()
	}
	return nil
}

func ownerAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("owner barrier timed out")
	}
}
func ownerError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("owner result timed out")
		return nil
	}
}
func ownerAbsent(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	// All owners are in this bubble. A runnable cleanup/Shutdown cannot be
	// mistaken for a waiter blocked on a still-owned DATA gate.
	synctest.Wait()
	select {
	case <-ch:
		t.Fatalf("%s occurred before DATA owner exited", label)
	default:
	}
}
func ownerRelease(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	fn := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(fn)
	return ch, fn
}
func ownerResponse(t *testing.T, c *ownerConn, prefix string) {
	t.Helper()
	for {
		select {
		case line := <-c.responses:
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("response %q, want %s", line, prefix)
			}
			if len(line) < 4 || line[3] != '-' {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("response %s timed out", prefix)
		}
	}
}
func ownerCommand(t *testing.T, c *ownerConn, command, response string) {
	t.Helper()
	if _, err := io.WriteString(c.w, command+"\r\n"); err != nil {
		t.Fatal(err)
	}
	ownerResponse(t, c, response)
}
func ownerStart(t *testing.T, session Session) (*Server, *ownerConn, <-chan error) {
	t.Helper()
	c := newOwnerConn()
	l := &ownerListener{conn: make(chan net.Conn, 1), closed: make(chan struct{})}
	l.conn <- c
	s := NewServer(BackendFunc(func(*Conn) (Session, error) { return session, nil }))
	s.ErrorLog = log.New(io.Discard, "", 0)
	serveDone := make(chan error, 1)
	go func() { serveDone <- s.Serve(l) }()
	t.Cleanup(func() { c.Close(); s.Close() })
	ownerResponse(t, c, "220")
	ownerCommand(t, c, "EHLO example.org", "250")
	ownerCommand(t, c, "MAIL FROM:<sender@example.org>", "250")
	ownerCommand(t, c, "RCPT TO:<recipient@example.org>", "250")
	return s, c, serveDone
}

func TestOwnerBDATDelayedEntryShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entry, releaseEntry := ownerRelease(t)
		dispatched := make(chan struct{})
		entered := make(chan struct{})
		loggedOut := make(chan struct{})
		readErr := make(chan error, 1)
		session := &ownerSession{data: func(r io.Reader) error {
			close(dispatched)
			<-entry // Delay all backend work, including any backend-local registration.
			close(entered)
			_, err := io.Copy(io.Discard, r)
			readErr <- err
			return err
		}, logout: func() { close(loggedOut) }}
		s, c, serveDone := ownerStart(t, session)
		ownerCommand(t, c, "BDAT 0", "250")
		ownerAwait(t, dispatched)
		c.Close()
		stopped := make(chan error, 1)
		go func() { stopped <- s.Shutdown(context.Background()) }()
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
		ownerAbsent(t, entered, "backend entry")
		ownerAbsent(t, loggedOut, "Logout")
		select {
		case err := <-stopped:
			t.Fatalf("Shutdown prematurely returned %v", err)
		default:
		}
		releaseEntry()
		ownerAwait(t, entered)
		if err := ownerError(t, readErr); !errors.Is(err, ErrDataReset) {
			t.Fatalf("read error %v", err)
		}
		if err := ownerError(t, stopped); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, loggedOut)
	})
}

func TestOwnerBDATResetJoinsBeforeSessionReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		returned, release := ownerRelease(t)
		readReset := make(chan struct{})
		reset := make(chan struct{}, 1)
		session := &ownerSession{data: func(r io.Reader) error {
			_, err := io.Copy(io.Discard, r)
			if !errors.Is(err, ErrDataReset) {
				return errors.New("missing reset error")
			}
			close(readReset)
			<-returned
			return err
		}, reset: func() { reset <- struct{}{} }}
		s, c, serveDone := ownerStart(t, session)
		ownerCommand(t, c, "BDAT 0", "250")
		if _, err := io.WriteString(c.w, "RSET\r\n"); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, readReset)
		ownerAbsent(t, reset, "Session.Reset")
		release()
		ownerResponse(t, c, "250")
		ownerAwait(t, reset)
		ownerCommand(t, c, "MAIL FROM:<second@example.org>", "250")
		ownerCommand(t, c, "RCPT TO:<second@example.org>", "250")
		c.Close()
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
	})
}

// A changed/full Conn.dataResult must never redirect an old worker's send.
// This white-box stress case isolates the old closure's permanent-block bug;
// legal reset/repeated transactions are independently covered through Serve.
func TestOwnerBDATCapturesPrivateResultEvenWhenNextChannelFull(t *testing.T) {
	gate, release := ownerRelease(t)
	entered := make(chan struct{})
	session := &ownerSession{data: func(io.Reader) error { close(entered); <-gate; return nil }}
	s := NewServer(nil)
	c := newConn(newOwnerConn(), s)
	c.setSession(session)
	c.fromReceived = true
	c.recipients = []string{"first@example.org"}
	c.handleBdat("0")
	ownerAwait(t, entered)
	original := c.dataResult
	next := make(chan error, 1)
	sentinel := errors.New("next transaction result")
	next <- sentinel
	c.dataResult = next
	release()
	joined := make(chan struct{})
	go func() { c.dataWG.Wait(); close(joined) }()
	ownerAwait(t, joined)
	if err := ownerError(t, original); err != nil {
		t.Fatal(err)
	}
	if err := <-next; err != sentinel {
		t.Fatalf("next result overwritten: %v", err)
	}
	c.Close()
	c.logout()
}

type ownerTailLogger struct {
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (l *ownerTailLogger) Printf(string, ...interface{}) {
	// Only the entry notification is one-shot; every logging tail owns the gate.
	l.once.Do(func() { close(l.entered) })
	<-l.release
}
func (*ownerTailLogger) Println(...interface{}) {}

func TestOwnerTailLoggerRepeatedConcurrentCallsWaitForRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, release := ownerRelease(t)
		logger := &ownerTailLogger{entered: make(chan struct{}), release: gate}
		const concurrentCalls = 8
		returned := make(chan struct{}, 1+concurrentCalls)
		call := func() {
			logger.Printf("tail")
			returned <- struct{}{}
		}
		go call()
		ownerAwait(t, logger.entered)
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("first Printf returned before release")
		default:
		}
		// Entry has already been signaled: subsequent concurrent calls must
		// still wait on the release gate.
		for i := 0; i < concurrentCalls; i++ {
			go call()
		}
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("Printf returned before release")
		default:
		}
		release()
		for i := 0; i < 1+concurrentCalls; i++ {
			ownerAwait(t, returned)
		}
		// Serve can log again after the DATA recovery tail exits.
		logger.Printf("later tail")
		logger.Printf("repeated later tail")
	})
}

func TestOwnerBDATPanicTailIncludedInShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, release := ownerRelease(t)
		tail := make(chan struct{})
		loggedOut := make(chan struct{})
		session := &ownerSession{data: func(io.Reader) error { panic("tail barrier") }, logout: func() { close(loggedOut) }}
		// Install the logger before Serve: no mutable server configuration races.
		c := newOwnerConn()
		l := &ownerListener{conn: make(chan net.Conn, 1), closed: make(chan struct{})}
		l.conn <- c
		s := NewServer(BackendFunc(func(*Conn) (Session, error) { return session, nil }))
		s.ErrorLog = &ownerTailLogger{entered: tail, release: gate}
		serveDone := make(chan error, 1)
		go func() { serveDone <- s.Serve(l) }()
		t.Cleanup(func() { c.Close(); s.Close() })
		ownerResponse(t, c, "220")
		ownerCommand(t, c, "EHLO example.org", "250")
		ownerCommand(t, c, "MAIL FROM:<a@example.org>", "250")
		ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
		ownerCommand(t, c, "BDAT 0", "250")
		ownerAwait(t, tail) // Data already panicked; its recovery/result/pipe tail is live.
		c.Close()
		stopped := make(chan error, 1)
		go func() { stopped <- s.Shutdown(context.Background()) }()
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
		ownerAbsent(t, loggedOut, "Logout")
		select {
		case err := <-stopped:
			t.Fatalf("tail escaped join: %v", err)
		default:
		}
		release()
		if err := ownerError(t, stopped); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, loggedOut)
	})
}

func TestOwnerBDATRepeatedTransactions(t *testing.T) {
	const transactions = 12
	readErrors := make(chan error, transactions)
	session := &ownerSession{data: func(r io.Reader) error { _, err := io.Copy(io.Discard, r); readErrors <- err; return err }}
	s, c, serveDone := ownerStart(t, session)
	for i := 0; i < transactions; i++ {
		if i > 0 {
			ownerCommand(t, c, "MAIL FROM:<a@example.org>", "250")
			ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
		}
		ownerCommand(t, c, "BDAT 0", "250")
		if i%2 == 0 {
			ownerCommand(t, c, "RSET", "250")
			if err := ownerError(t, readErrors); !errors.Is(err, ErrDataReset) {
				t.Fatalf("transaction %d: %v", i, err)
			}
		} else {
			ownerCommand(t, c, "BDAT 0 LAST", "250")
			if err := ownerError(t, readErrors); err != nil {
				t.Fatalf("transaction %d: %v", i, err)
			}
		}
	}
	c.Close()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ownerError(t, serveDone); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerBDATBackendCloseDoesNotSelfJoin(t *testing.T) {
	closed := make(chan struct{})
	loggedOut := make(chan struct{})
	var captured *Conn
	session := &ownerSession{data: func(io.Reader) error { captured.Close(); close(closed); return ErrDataReset }, logout: func() { close(loggedOut) }}
	c := newOwnerConn()
	l := &ownerListener{conn: make(chan net.Conn, 1), closed: make(chan struct{})}
	l.conn <- c
	s := NewServer(BackendFunc(func(conn *Conn) (Session, error) { captured = conn; return session, nil }))
	s.ErrorLog = log.New(io.Discard, "", 0)
	serveDone := make(chan error, 1)
	go func() { serveDone <- s.Serve(l) }()
	t.Cleanup(func() { c.Close(); s.Close() })
	ownerResponse(t, c, "220")
	ownerCommand(t, c, "EHLO example.org", "250")
	ownerCommand(t, c, "MAIL FROM:<a@example.org>", "250")
	ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
	ownerCommand(t, c, "BDAT 0", "250")
	ownerAwait(t, closed)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	ownerAwait(t, loggedOut)
	if err := ownerError(t, serveDone); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerBDATLastErrorAndPanicStatuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   error
		panic  bool
		prefix string
	}{
		{name: "success", prefix: "250"},
		{name: "backend_error", fail: &SMTPError{Code: 451, EnhancedCode: EnhancedCode{4, 3, 0}, Message: "retry"}, prefix: "451"},
		{name: "panic", panic: true, prefix: "421"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &ownerSession{data: func(r io.Reader) error {
				io.Copy(io.Discard, r)
				if tc.panic {
					panic("backend")
				}
				return tc.fail
			}}
			s, c, serveDone := ownerStart(t, session)
			ownerCommand(t, c, "BDAT 0 LAST", tc.prefix)
			c.Close()
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := ownerError(t, serveDone); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOwnerBDATEarlyErrorResetsAndPreservesStatus(t *testing.T) {
	session := &ownerSession{data: func(io.Reader) error {
		return &SMTPError{Code: 451, EnhancedCode: EnhancedCode{4, 3, 0}, Message: "retry"}
	}}
	s, c, serveDone := ownerStart(t, session)
	if _, err := io.WriteString(c.w, "BDAT 4\r\ndata"); err != nil {
		t.Fatal(err)
	}
	ownerResponse(t, c, "451")
	ownerCommand(t, c, "MAIL FROM:<retry@example.org>", "250")
	c.Close()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ownerError(t, serveDone); err != nil {
		t.Fatal(err)
	}
}

// The listener returns a successful Accept only after Shutdown seals admission.
// Its late socket close remains owned by Serve, and therefore by Shutdown.
type ownerLateListener struct {
	acceptEntered chan struct{}
	closed        chan struct{}
	release       <-chan struct{}
	conn          net.Conn
	once          sync.Once
}

func (l *ownerLateListener) Accept() (net.Conn, error) {
	close(l.acceptEntered)
	<-l.release
	return l.conn, nil
}
func (l *ownerLateListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*ownerLateListener) Addr() net.Addr { return ownerAddr("late") }
func TestOwnerShutdownWaitsForLateAcceptDisposal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, release := ownerRelease(t)
		c := newOwnerConn()
		l := &ownerLateListener{acceptEntered: make(chan struct{}), closed: make(chan struct{}), release: gate, conn: c}
		s := NewServer(BackendFunc(func(*Conn) (Session, error) { t.Error("late connection admitted"); return nil, errors.New("late") }))
		serveDone := make(chan error, 1)
		go func() { serveDone <- s.Serve(l) }()
		ownerAwait(t, l.acceptEntered)
		stopped := make(chan error, 1)
		go func() { stopped <- s.Shutdown(context.Background()) }()
		ownerAwait(t, l.closed)
		synctest.Wait() // Shutdown has reached its join, not merely closed l.
		select {
		case err := <-stopped:
			t.Fatalf("late Accept escaped join: %v", err)
		default:
		}
		release()
		if err := ownerError(t, stopped); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, c.closed)
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOwnerShutdownRejectsLaterServe(t *testing.T) {
	s := NewServer(nil)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := &ownerListener{conn: make(chan net.Conn), closed: make(chan struct{})}
	if err := s.Serve(l); !errors.Is(err, ErrServerClosed) {
		t.Fatalf("Serve: %v", err)
	}
	ownerAwait(t, l.closed)
}

func TestOwnerBDATTailClosesReaderBeforeJoin(t *testing.T) {
	gate, release := ownerRelease(t)
	entered := make(chan struct{})
	failure := errors.New("backend terminal error")
	session := &ownerSession{data: func(io.Reader) error { close(entered); <-gate; return failure }}
	s := NewServer(nil)
	c := newConn(newOwnerConn(), s)
	c.setSession(session)
	c.fromReceived = true
	c.recipients = []string{"first@example.org"}
	c.handleBdat("0")
	ownerAwait(t, entered)
	writer, result := c.bdatPipe, c.dataResult
	release()
	joined := make(chan struct{})
	go func() { c.dataWG.Wait(); close(joined) }()
	ownerAwait(t, joined)
	// The completion send remains buffered and unconsumed at join. The tail
	// must nevertheless have closed r, otherwise this Write cannot return.
	if len(result) != 1 {
		t.Fatal("missing terminal result before join")
	}
	writeDone := make(chan error, 1)
	go func() { _, err := writer.Write([]byte("x")); writeDone <- err }()
	if err := ownerError(t, writeDone); err != failure {
		t.Fatalf("pipe tail error: %v", err)
	}
	if err := ownerError(t, result); err != failure {
		t.Fatalf("terminal result: %v", err)
	}
	c.Close()
	c.logout()
}

type ownerLMTPSession struct {
	*ownerSession
	lmtpData func(io.Reader, StatusCollector) error
}

func (s *ownerLMTPSession) LMTPData(r io.Reader, status StatusCollector) error {
	return s.lmtpData(r, status)
}

func TestOwnerBDATLMTPRepeatedResetAndLast(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "native"}[native], func(t *testing.T) {
			base := &ownerSession{data: func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }}
			var session Session = base
			if native {
				session = &ownerLMTPSession{ownerSession: base, lmtpData: func(r io.Reader, _ StatusCollector) error { _, err := io.Copy(io.Discard, r); return err }}
			}
			c := newOwnerConn()
			l := &ownerListener{conn: make(chan net.Conn, 1), closed: make(chan struct{})}
			l.conn <- c
			s := NewServer(BackendFunc(func(*Conn) (Session, error) { return session, nil }))
			s.LMTP = true
			s.ErrorLog = log.New(io.Discard, "", 0)
			serveDone := make(chan error, 1)
			go func() { serveDone <- s.Serve(l) }()
			t.Cleanup(func() { c.Close(); s.Close() })
			ownerResponse(t, c, "220")
			ownerCommand(t, c, "LHLO example.org", "250")
			for i := 0; i < 4; i++ {
				ownerCommand(t, c, "MAIL FROM:<a@example.org>", "250")
				ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
				ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
				ownerCommand(t, c, "BDAT 0", "250")
				if i%2 == 0 {
					ownerCommand(t, c, "RSET", "250")
				} else {
					ownerCommand(t, c, "BDAT 0 LAST", "250")
					ownerResponse(t, c, "250")
				}
			}
			c.Close()
			if err := s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := ownerError(t, serveDone); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOwnerBDATCloseResetRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Public Close races the command owner's RSET, never a second command loop.
		// A race-detector run of this selector exercises field ownership as well as
		// completion; callback barriers avoid timing-based ordering assumptions.
		gate, release := ownerRelease(t)
		readReset := make(chan struct{})
		loggedOut := make(chan struct{})
		var conn *Conn
		session := &ownerSession{data: func(r io.Reader) error { _, err := io.Copy(io.Discard, r); close(readReset); <-gate; return err }, logout: func() { close(loggedOut) }}
		c := newOwnerConn()
		l := &ownerListener{conn: make(chan net.Conn, 1), closed: make(chan struct{})}
		l.conn <- c
		s := NewServer(BackendFunc(func(c *Conn) (Session, error) { conn = c; return session, nil }))
		s.ErrorLog = log.New(io.Discard, "", 0)
		serveDone := make(chan error, 1)
		go func() { serveDone <- s.Serve(l) }()
		t.Cleanup(func() { c.Close(); s.Close() })
		ownerResponse(t, c, "220")
		ownerCommand(t, c, "EHLO example.org", "250")
		ownerCommand(t, c, "MAIL FROM:<a@example.org>", "250")
		ownerCommand(t, c, "RCPT TO:<b@example.org>", "250")
		ownerCommand(t, c, "BDAT 0", "250")
		if _, err := io.WriteString(c.w, "RSET\r\n"); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, readReset)
		closeDone := make(chan struct{})
		go func() { conn.Close(); close(closeDone) }()
		ownerAwait(t, closeDone)
		ownerAbsent(t, loggedOut, "Logout")
		release()
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		ownerAwait(t, loggedOut)
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
	})
}

// Read supplies the entire script in one call, reproducing textproto's buffered
// input surviving a transport close. No producer goroutine can mask the bug by
// failing a second transport write after Close.
type ownerPrebufferConn struct {
	*ownerConn
	payload []byte
	reads   int
}

func (c *ownerPrebufferConn) Read(p []byte) (int, error) {
	c.reads++
	if c.reads != 1 {
		return 0, io.EOF
	}
	if len(p) < len(c.payload) {
		panic("regression script must fit one transport Read")
	}
	return copy(p, c.payload), nil
}

type ownerCountSession struct {
	reset, mail, rcpt, logout, data int
}

func (s *ownerCountSession) Reset()                          { s.reset++ }
func (s *ownerCountSession) Logout() error                   { s.logout++; return nil }
func (s *ownerCountSession) Mail(string, *MailOptions) error { s.mail++; return nil }
func (s *ownerCountSession) Rcpt(string, *RcptOptions) error { s.rcpt++; return nil }
func (s *ownerCountSession) Data(io.Reader) error            { s.data++; return nil }

func TestOwnerClosedParseErrorsDoNotDispatchBufferedCommands(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new_session", true: "existing_session"}[existing], func(t *testing.T) {
			initial := ""
			if existing {
				initial = "EHLO initial.example.org\r\n"
			}
			payload := initial + strings.Repeat("x\r\n", 4) + "EHLO example.org\r\nMAIL FROM:<a@example.org>\r\nRCPT TO:<b@example.org>\r\nRSET\r\nBDAT 0 LAST\r\n"
			wire := &ownerPrebufferConn{ownerConn: newOwnerConn(), payload: []byte(payload)}
			session := &ownerCountSession{}
			newSessions := 0
			s := NewServer(BackendFunc(func(*Conn) (Session, error) { newSessions++; return session, nil }))
			s.ErrorLog = log.New(io.Discard, "", 0)
			if err := s.handleConn(newConn(wire, s)); err != nil {
				t.Fatal(err)
			}
			wantSessions := 0
			if existing {
				wantSessions = 1
			}
			if newSessions != wantSessions {
				t.Fatalf("closed buffered EHLO dispatched: NewSession=%d, want %d", newSessions, wantSessions)
			}
			if session.reset != 0 || session.mail != 0 || session.rcpt != 0 || session.data != 0 {
				t.Fatalf("closed buffered business command dispatched: %+v", session)
			}
			if session.logout != wantSessions {
				t.Fatalf("Logout=%d, want %d", session.logout, wantSessions)
			}
			if wire.reads != 1 {
				t.Fatalf("transport reads=%d, want exactly one prebuffered read", wire.reads)
			}
			ownerAwait(t, wire.closed)
			// Keep the existing protocol responses: four syntax errors, then the
			// established threshold close, and no ACK for the buffered commands.
			ownerResponse(t, wire.ownerConn, "220")
			if existing {
				ownerResponse(t, wire.ownerConn, "250")
			}
			for i := 0; i < 4; i++ {
				ownerResponse(t, wire.ownerConn, "501")
			}
			ownerResponse(t, wire.ownerConn, "500")
			if len(wire.responses) != 0 {
				t.Fatalf("unexpected post-close responses: %d", len(wire.responses))
			}
		})
	}
}

// The public fork Close/second Shutdown remain interrupt-only. A retained first
// Shutdown must cover the actual Logout tail before allowing resource release.
func TestOwnerR5CloudLogoutTailRetainedJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, release := ownerRelease(t)
		entered := make(chan struct{})
		session := &ownerSession{data: func(r io.Reader) error { _, err := io.Copy(io.Discard, r); return err }, logout: func() { close(entered); <-gate }}
		s, c, serveDone := ownerStart(t, session)
		ownerCommand(t, c, "DATA", "354")
		ownerCommand(t, c, "Subject: synthetic\r\n\r\nbody\r\n.", "250")
		c.Close()
		ownerAwait(t, entered)
		stopped := make(chan error, 1)
		go func() { stopped <- s.Shutdown(context.Background()) }()
		if err := ownerError(t, serveDone); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := s.Shutdown(context.Background()); !errors.Is(err, ErrServerClosed) {
			t.Fatalf("second Shutdown: %v", err)
		}
		if err := s.Close(); !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Close: %v", err)
		}
		select {
		case err := <-stopped:
			t.Fatalf("Logout tail escaped retained join: %v", err)
		default:
		}
		release()
		if err := ownerError(t, stopped); err != nil {
			t.Fatal(err)
		}
	})
}
