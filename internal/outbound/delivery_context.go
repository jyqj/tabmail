package outbound

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const smtpSessionTimeout = 2 * time.Minute

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
	return conn, release, nil
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
