package companymail

import (
	"context"
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
	if current.RawObjectKey != observed.RawObjectKey {
		return app.Conflict("message source changed; reload")
	}
	return nil
}

// authorizedSource protects the first successful read as well as Source's
// object-open boundary: some object adapters defer their slow work until Read.
// Once bytes have been released, the stream keeps its established authorization
// rather than doing database queries for every chunk. Read has the usual Reader
// single-caller contract; Close is idempotent and may be called for cancellation.
type authorizedSource struct {
	io.ReadCloser
	check     func() error
	terminal  error
	closeOnce sync.Once
	closeErr  error
}

func (r *authorizedSource) Read(p []byte) (int, error) {
	if r.terminal != nil {
		return 0, r.terminal
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err := r.ReadCloser.Read(p)
	if n > 0 && r.check != nil {
		if denied := r.check(); denied != nil {
			// io.Copy must receive neither a successful byte count nor stale
			// bytes left in the destination buffer after failed authorization.
			clear(p[:n])
			r.terminal = denied
			_ = r.Close()
			return 0, denied
		}
		r.check = nil
	}
	return n, err
}

func (r *authorizedSource) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.ReadCloser.Close() })
	return r.closeErr
}
