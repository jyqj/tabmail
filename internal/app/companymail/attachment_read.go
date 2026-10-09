package companymail

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Upload readers remain caller-owned. Their Read must return to observe
// cancellation; no detached worker is created around an arbitrary io.Reader.
func readAttachment(ctx context.Context, input io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(&attachmentProgressReader{ctx: ctx, input: input}, limit))
}

// Object-store readers are owned by the download operation. As in the MIME
// parser's ObjectReader contract, Close must unblock an in-flight Read.
func readOwnedAttachment(ctx context.Context, input io.ReadCloser, limit int64) (data []byte, err error) {
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = input.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() {
		stop()
		// Once also waits for a cancellation-triggered Close still in progress.
		closeReader()
		err = errors.Join(err, closeErr, ctx.Err())
		if err != nil {
			data = nil
		}
	}()
	return readAttachment(ctx, input, limit)
}

type attachmentProgressReader struct {
	ctx   context.Context
	input io.Reader
}

func (r *attachmentProgressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.input.Read(p)
		if canceled := r.ctx.Err(); canceled != nil {
			clear(p)
			return 0, errors.Join(err, canceled)
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
