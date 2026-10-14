package outbound

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	smtpSessionTimeout = 2 * time.Minute
	// Bound cumulative input before net/smtp's unbounded textproto response
	// parsing. Count raw received bytes, including TLS handshake/ciphertext;
	// outgoing MIME bytes do not consume this per-TCP-connection allowance.
	smtpResponseByteLimit int64 = 1 << 20
)

var errSMTPResponseByteLimit = errors.New("SMTP response byte limit exceeded")

type smtpResponseConn struct {
	net.Conn
	readMu    sync.Mutex
	remaining int64
	limitHit  bool
}

func (c *smtpResponseConn) Read(p []byte) (int, error) {
	// Keep net.Conn's concurrent-read contract without holding a lock needed
	// by Close or the existing cancellation watcher while a read is blocked.
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.remaining == 0 {
		c.limitHit = true
		return 0, errSMTPResponseByteLimit
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}

// Setup operations can hide the read error behind a parsed reply or an
// EHLO-to-HELO fallback. Do not continue to another operation once an actual
// read exceeded the allowance. Merely using the last allowed byte is valid.
func (c *smtpResponseConn) responseError(err error) error {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.limitHit {
		return errSMTPResponseByteLimit
	}
	return err
}

type smtpReplyReader struct {
	io.Reader
	tail     []byte
	received int64
	readErr  error
}

func (r *smtpReplyReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil {
		r.readErr = err
	}
	// Retain only enough plaintext to find the byte immediately before the
	// outer bufio.Reader's unread suffix, even when a read prefetches replies.
	data := p[:n]
	if len(data) > len(r.tail) {
		data = data[len(data)-len(r.tail):]
	}
	start := (r.received + int64(n-len(data))) % int64(len(r.tail))
	first := copy(r.tail[start:], data)
	copy(r.tail, data[first:])
	r.received += int64(n)
	return n, err
}

// Install after TLS setup, which replaces net/smtp's textproto reader. Raw
// socket bytes remain limited below TLS; this fixed-size plaintext tail only
// checks whether the response actually consumed its terminating LF. ReadLine
// can otherwise accept an unterminated fragment after an underlying error.
func guardSMTPReplyReader(reader *bufio.Reader) (*bufio.Reader, func(error) error) {
	observed := &smtpReplyReader{Reader: reader}
	buffered := bufio.NewReader(observed)
	observed.tail = make([]byte, buffered.Size()+1)
	return buffered, func(err error) error {
		consumed := observed.received - int64(buffered.Buffered())
		if observed.readErr != nil && (consumed == 0 || observed.tail[(consumed-1)%int64(len(observed.tail))] != '\n') {
			// A truncated 4xx/5xx is not an authoritative rejection either;
			// replace it rather than keeping a textproto.Error in the chain.
			// EOF, timeout and other failures can end a short fragment just
			// as the byte limit can. Preserve the actual transport cause.
			return observed.readErr
		}
		// TLS may deliver a complete authenticated reply together with an
		// error while prefetching the next record. A real final LF still
		// makes that reply authoritative, especially DATA's successful 250.
		return err
	}
}

// dialSMTPContext owns the socket until release. Closing the transport on
// cancellation interrupts all net/smtp operations, including implicit reads in
// NewClient, STARTTLS, DATA.Close and QUIT. release waits for a running callback
// so no cancellation watcher outlives its session or acts on a later MX socket.
func dialSMTPContext(ctx context.Context, addr string) (net.Conn, func(), error) {
	return dialSMTPWithConnector(ctx, addr, (&net.Dialer{Timeout: 30 * time.Second}).DialContext)
}

// A per-call connector keeps ownership testable without mutable global hooks.
// Production always supplies the standard context-aware TCP dialer above.
func dialSMTPWithConnector(ctx context.Context, addr string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	deadline := time.Now().Add(smtpSessionTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.Close(); close(stopped) })
	var once sync.Once
	release := func() {
		once.Do(func() {
			if !stop() {
				<-stopped
			}
			_ = conn.Close()
		})
	}
	// Every NewClient and STARTTLS read retains this wrapper. A reconnect
	// obtains a new socket with its own budget through this same boundary.
	return &smtpResponseConn{Conn: conn, remaining: smtpResponseByteLimit}, release, nil
}

// Preserve transport/protocol errors and the context cause. A nil error means
// DATA was acknowledged; cancellation during QUIT must not turn that into retry.
func smtpContextError(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}

	if err != nil {
		var timeout net.Error
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) && errors.As(err, &timeout) && timeout.Timeout() {
			return errors.Join(err, context.DeadlineExceeded)
		}
	}
	return err
}
