package s3obj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// This is the pinned S3 SDK's maximum object size. For unknown-length inputs,
// reaching the ceiling still requires EOF; exhausting multipart slots must not
// silently turn a longer caller stream into a successfully truncated object.
const maxUploadSize int64 = 5 * 1024 * 1024 * 1024 * 1024

type uploadReadEnd struct{ err error }

// uploadReader validates source bytes before handing them to the SDK. It keeps
// no object-sized buffer, exposes neither ReadAt nor Close, and never rewinds or
// closes the caller's reader. In particular the SDK cannot replace this reader
// with a SectionReader that hides excess bytes beyond the declared length.
type uploadReader struct {
	ctx       context.Context
	source    io.Reader
	remaining int64
	exact     bool
	seeked    atomic.Bool
	end       atomic.Pointer[uploadReadEnd]
}

func newUploadReader(ctx context.Context, source io.Reader, size int64) *uploadReader {
	remaining := size
	if size == -1 {
		remaining = maxUploadSize
	}
	return &uploadReader{ctx: ctx, source: source, remaining: remaining, exact: size >= 0}
}

// MinIO's hookReader advertises io.Seeker even for non-seekable input and makes
// its seek a no-op. Allow its initial request setup, but refuse a retry that
// would otherwise send bytes after the first stream's EOF as a new object.
// Multipart retries use SDK-owned part buffers and do not seek this source.
func (r *uploadReader) Seek(offset int64, whence int) (int64, error) {
	if offset != 0 || whence != io.SeekStart || !r.seeked.CompareAndSwap(false, true) {
		return 0, fmt.Errorf("s3obj: consumed upload source cannot be replayed")
	}
	return 0, nil
}

func (r *uploadReader) completionError() error {
	end := r.end.Load()
	if end == nil {
		return fmt.Errorf("s3obj: source end was not verified: %w", io.ErrUnexpectedEOF)
	}
	if end.err == io.EOF {
		return nil
	}
	return end.err
}

func (r *uploadReader) fail(p []byte, err error) (int, error) {
	clear(p)
	// MinIO treats a literal io.ErrUnexpectedEOF as normal multipart EOF.
	// Preserve errors.Is while ensuring every non-EOF source failure aborts.
	err = fmt.Errorf("s3obj: read upload source: %w", err)
	r.end.Store(&uploadReadEnd{err: err})
	return 0, err
}

func (r *uploadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return r.fail(p, err)
	}
	if len(p) == 0 {
		return 0, nil
	}
	if end := r.end.Load(); end != nil {
		return 0, end.err
	}
	if r.remaining == 0 {
		return r.finish(p, 0)
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.readSource(p)
	if err != nil && err != io.EOF {
		return r.fail(p, err)
	}
	r.remaining -= int64(n)
	if err == io.EOF {
		if r.exact && r.remaining != 0 {
			return r.fail(p, io.ErrUnexpectedEOF)
		}
		r.end.Store(&uploadReadEnd{err: io.EOF})
		return n, io.EOF
	}
	if r.remaining == 0 {
		// Withhold the final bytes until a one-byte probe proves EOF. Doing
		// this after PutObject returns would already have published a prefix.
		return r.finish(p, n)
	}
	return n, nil
}

func (r *uploadReader) finish(p []byte, n int) (int, error) {
	var probe [1]byte
	extra, err := r.readSource(probe[:])
	if extra != 0 {
		if err == io.EOF {
			err = nil
		}
		return r.fail(p, errors.Join(fmt.Errorf("s3obj: source exceeds declared or maximum object size"), err))
	}
	if err != io.EOF {
		return r.fail(p, err)
	}
	r.end.Store(&uploadReadEnd{err: io.EOF})
	return n, io.EOF
}

func (r *uploadReader) readSource(p []byte) (int, error) {
	for empty := 0; empty < 100; empty++ {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := r.source.Read(p)
		if cancelled := r.ctx.Err(); cancelled != nil {
			return 0, cancelled
		}
		if n < 0 || n > len(p) {
			return 0, io.ErrUnexpectedEOF
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
