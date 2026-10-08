package rawobject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
)

// A backend reader is owned here; Close must unblock its in-flight Read. The
// callback is joined before returning, including its error, so cancellation
// cannot turn a partial verification into a successful dedup or repair.
func (s *Store) matchesExisting(ctx context.Context, key string, raw []byte) (matches bool, err error) {
	input, err := s.blob.Get(ctx, key)
	if input == nil {
		return false, errors.Join(err, ctx.Err(), errors.New("raw object reader is nil"))
	}
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = input.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() {
		stop()
		closeReader() // Once waits if cancellation is still closing the reader.
		err = errors.Join(err, closeErr, ctx.Err())
		if err != nil {
			matches = false
		}
	}()
	// Acquisition can return an owned partial resource alongside its error.
	// Close and join it without consuming any bytes or authorizing a repair.
	if err != nil {
		return false, err
	}

	// One excess byte proves an oversized object; do not drain it. The memory
	// cost is io.Copy's fixed buffer, independent of the stored object's size.
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(&integrityReader{ctx: ctx, input: input}, int64(len(raw))+1))
	if err != nil {
		return false, err
	}
	want := sha256.Sum256(raw)
	return n == int64(len(raw)) && bytes.Equal(hash.Sum(nil), want[:]), nil
}

type integrityReader struct {
	ctx   context.Context
	input io.Reader
}

func (r *integrityReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.input.Read(p)
		if canceled := r.ctx.Err(); canceled != nil {
			return 0, errors.Join(err, canceled)
		}
		if n < 0 || n > len(p) {
			return 0, errors.Join(io.ErrUnexpectedEOF, err)
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
