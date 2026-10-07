package companymail

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Object I/O and shared parsing may outlive the initial authorization. Reload
// through the same authority, without keeping a DB transaction across I/O.
// This establishes a pre-release decision, not revocation of delivered bytes.
func (s *Service) recheckMessage(ctx context.Context, actor authz.Actor, mailbox uuid.UUID, observed *models.Message) error {
	current, err := s.message(ctx, actor, mailbox, observed.ID)
	if err != nil {
		return err
	}
	if current.RawObjectKey != observed.RawObjectKey || current.ZoneID != observed.ZoneID || !current.ReceivedAt.Equal(observed.ReceivedAt) {
		return app.Conflict("message source changed; reload")
	}
	return nil
}

// authorizedSource protects the first successful read and a terminal empty EOF
// (which also causes HTTP success headers), as well as Source's
// object-open boundary: some object adapters defer their slow work until Read.
// Once bytes have been released, the stream keeps its established authorization
// rather than doing database queries for every chunk. Read has the usual Reader
// single-caller contract; Close is idempotent and may be called for cancellation.
type authorizedSource struct {
	io.ReadCloser
	ctx       context.Context
	check     func() error
	terminal  error
	closeOnce sync.Once
	closeErr  error
	stopClose func() bool
}

func newAuthorizedSource(ctx context.Context, input io.ReadCloser, check func() error) *authorizedSource {
	r := &authorizedSource{ReadCloser: input, ctx: ctx, check: check}
	// Close must unblock the adapter's Read. The callback only accesses the
	// once-protected closer, so immediate cancellation cannot race constructor
	// initialization or the single caller's Read state.
	r.stopClose = context.AfterFunc(ctx, func() { _ = r.closeUnderlying() })
	return r
}

func (r *authorizedSource) Read(p []byte) (int, error) {
	if r.terminal != nil {
		return 0, r.terminal
	}
	if err := r.ctx.Err(); err != nil {
		clear(p)
		return 0, r.finish(err)
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err := r.ReadCloser.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		clear(p)
		return 0, r.finish(errors.Join(cancelled, err))
	}
	if n < 0 || n > len(p) {
		clear(p)
		return 0, r.finish(errors.Join(io.ErrUnexpectedEOF, err))
	}
	if (n > 0 || err == io.EOF) && r.check != nil {
		if denied := r.check(); denied != nil {
			// io.Copy must receive neither a successful byte count nor stale
			// bytes left in the destination buffer after failed authorization.
			clear(p)
			return 0, r.finish(errors.Join(denied, err))
		}
		r.check = nil
	}
	if err != nil {
		err = r.finish(err)
	}
	// Closing a completed stream may itself cross the cancellation boundary.
	if cancelled := r.ctx.Err(); cancelled != nil {
		clear(p)
		return 0, r.finish(errors.Join(cancelled, err))
	}
	return n, err
}

func (r *authorizedSource) finish(err error) error {
	// Preserve the exact clean EOF convention. A failed close cannot certify
	// successful completion; the HTTP adapter retains its abort-on-error path.
	if closeErr := r.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	r.terminal = err
	return err
}

func (r *authorizedSource) closeUnderlying() error {
	r.closeOnce.Do(func() { r.closeErr = r.ReadCloser.Close() })
	return r.closeErr
}

func (r *authorizedSource) Close() error {
	r.stopClose()
	return r.closeUnderlying()
}
