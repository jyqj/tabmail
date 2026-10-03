package mailcontent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"github.com/jhillyerd/enmime/v2"
)

// Root is node 1 at depth 1. Containers, body parts, attachments and inlines
// all consume nodes, not just the attachment projection. These admission
// limits intentionally reject formerly accepted pathological MIME trees.
const MaxMIMEDepth = 32
const MaxMIMENodes = 1024

var ErrMIMEDepth = enmime.ErrParseDepthLimit
var ErrMIMENodes = enmime.ErrParseNodeLimit
var ErrMIMEParts = enmime.ErrParsePartLimit
var ErrMIMEBytes = errors.New("message exceeds 25 MiB parser limit")
var ErrMIMEParse = errors.New("MIME parsing failed")

// ParseBoundedReader takes ownership of r, including on cancellation/error.
// Close must unblock Read for prompt cancellation. No unbounded worker is
// started, and Reader's optional WriterTo cannot bypass byte/context checks.
func ParseBoundedReader(ctx context.Context, r io.ReadCloser) (*enmime.Envelope, error) {
	if r == nil {
		return nil, errors.New("object reader unavailable")
	}
	var once sync.Once
	closeReader := func() { once.Do(func() { _ = r.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() { stop(); closeReader() }()
	raw, err := io.ReadAll(io.LimitReader(&sourceProgressReader{ctx: ctx, reader: r}, MaxBytes+1))
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, err
	}
	return parseBoundedContext(ctx, raw)
}

func parseBoundedContext(ctx context.Context, raw []byte) (*enmime.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxBytes {
		return nil, ErrMIMEBytes
	}
	// Guard the real parser's Part allocation/header/recursion events. Do not
	// interpret a second MIME tree: ancestor readers can resume after a
	// temporary logical EOF, so fixed raw slices cannot budget that structure.
	env, err := enmime.ReadEnvelopeBounded(ctx, &sourceProgressReader{ctx: ctx, reader: bytes.NewReader(raw)}, enmime.ParseLimits{
		MaxDepth: MaxMIMEDepth, MaxNodes: MaxMIMENodes, MaxParts: maxParts, MaxBoundaryBytes: 4090,
	})
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		if errors.Is(err, ErrMIMEDepth) || errors.Is(err, ErrMIMENodes) || errors.Is(err, ErrMIMEParts) {
			return nil, err
		}
		return nil, ErrMIMEParse
	}
	if len(Parts(env)) > maxParts {
		return nil, ErrMIMEParts
	}
	return env, nil
}
