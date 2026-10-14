// Package recovery verifies immutable ingress evidence before replay. It does
// not expose raw storage keys or bytes as an employee read capability.
package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"

	"tabmail/internal/company"
)

// Get must honor ctx, and Close must unblock an in-flight Read. Verification
// owns the returned reader; it never starts an unbounded replacement read.
type Objects interface {
	Get(context.Context, string) (io.ReadCloser, error)
}

func Verify(ctx context.Context, objects Objects, v *company.RecoveryReceipt) bool {
	if ctx.Err() != nil || objects == nil || v == nil || v.RawKey == "" || v.RawSize < 0 || v.RawSize > 25*1024*1024 || len(v.RawHash) != sha256.Size*2 {
		return false
	}
	if _, err := hex.DecodeString(v.RawHash); err != nil {
		return false
	}
	observed := *v
	r, err := objects.Get(ctx, observed.RawKey)
	if r == nil {
		return false
	}
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = r.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() { stop(); closeReader() }()
	if err != nil || ctx.Err() != nil {
		return false
	}
	// Hash in a fixed buffer, retaining the extra byte that distinguishes an
	// exact source from a longer object with a matching prefix.
	hash := sha256.New()
	n, err := io.CopyBuffer(hash, io.LimitReader(&verificationProgressReader{ctx: ctx, reader: r}, observed.RawSize+1), make([]byte, 32*1024))
	stop()
	closeReader()
	return err == nil && closeErr == nil && n == observed.RawSize && hex.EncodeToString(hash.Sum(nil)) == observed.RawHash && ctx.Err() == nil
}

// Do not forward io.WriterTo: every read must observe cancellation and the
// finite no-progress budget, including readers supplied by storage adapters.
type verificationProgressReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *verificationProgressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.reader.Read(p)
		if canceled := r.ctx.Err(); canceled != nil {
			clear(p)
			return 0, canceled
		}
		if n < 0 || n > len(p) {
			clear(p)
			return 0, io.ErrUnexpectedEOF
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
