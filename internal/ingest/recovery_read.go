package ingest

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
)

// Replay needs the original bytes for all targets, bounded by the accepted
// receipt plus one byte to detect a longer object with the same prefix. Tenant
// size policy and the tolerant MIME parse remain separate delivery decisions.
func (s *Service) readReceiptOriginal(ctx context.Context, key string, size int64) (raw []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size < 0 || size == math.MaxInt64 {
		return nil, permanentIngress("invalid original object size; receipt held for recovery")
	}
	input, err := s.obj.Get(ctx, key)
	if input == nil {
		return nil, errors.Join(err, ctx.Err(), errors.New("original object returned no reader"))
	}
	// Get may return a reader together with an error. Every returned reader is
	// owned here; its Close must also unblock an in-flight Read on cancellation.
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = input.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() {
		stop()
		closeReader()
		err = errors.Join(err, closeErr, ctx.Err())
		if err != nil {
			raw = nil
		}
	}()
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(&replayProgressReader{ctx: ctx, input: input}, size+1))
}

// Keep every read behind the context, count and progress checks. In particular,
// do not promote an adapter's WriterTo implementation past these boundaries.
type replayProgressReader struct {
	ctx   context.Context
	input io.Reader
}

func (r *replayProgressReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.input.Read(p)
		if cancelled := r.ctx.Err(); cancelled != nil {
			clear(p)
			return 0, errors.Join(cancelled, err)
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
