package outbound

import (
	"context"
	"errors"
	"io"
	"sync"
)

// The worker owns object-store readers. Their Close must unblock an active
// Read, matching the existing object-reader contract used by the MIME parser.
func readQueuedAttachment(ctx context.Context, input io.ReadCloser, limit int64) (data []byte, err error) {
	if input == nil {
		return nil, errors.Join(errors.New("attachment object returned no reader"), ctx.Err())
	}
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = input.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() {
		stop()
		closeReader()
		err = errors.Join(err, closeErr, ctx.Err())
		if err != nil {
			data = nil
		}
	}()
	return io.ReadAll(io.LimitReader(&queuedAttachmentProgressReader{ctx: ctx, input: input}, limit))
}

type queuedAttachmentProgressReader struct {
	ctx   context.Context
	input io.Reader
}

func (r *queuedAttachmentProgressReader) Read(p []byte) (int, error) {
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
			return 0, errors.Join(io.ErrUnexpectedEOF, err)
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
