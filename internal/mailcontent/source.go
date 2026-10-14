package mailcontent

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

func (p *Parser) cached(key string) *parsed {
	p.mu.Lock()
	defer p.mu.Unlock()
	el := p.entries[key]
	if el == nil {
		return nil
	}
	v := el.Value.(*parsed)
	if time.Now().Before(v.until) {
		p.lru.MoveToFront(el)
		return v
	}
	p.bytes -= v.size
	p.lru.Remove(el)
	delete(p.entries, key)
	return nil
}

// ObjectReader.Get must honor ctx; Close must unblock a concurrent Read for
// prompt cancellation. An arbitrary adapter ignoring both cannot be forcibly
// interrupted here. Keep its slot occupied rather than spawning replacement
// reads without a bound. In-memory MIME decoding remains synchronous.
func (p *Parser) readSource(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := p.objects.Get(ctx, key)
	return readOwnedSource(ctx, r, err)
}

// Closing is part of a successful owned read. Join its result before returning
// bytes to MIME parsing or cache publication, including when Get already failed
// or cancellation has started the closer concurrently.
func readOwnedSource(ctx context.Context, r io.ReadCloser, openErr error) (raw []byte, err error) {
	if r == nil {
		if err = errors.Join(openErr, ctx.Err()); err == nil {
			err = errors.New("object reader unavailable")
		}
		return nil, err
	}
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = r.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() {
		stop()
		// Once waits for an in-flight cancellation callback before closeErr is
		// observed, and never calls the underlying Close a second time.
		closeReader()
		err = errors.Join(err, closeErr, ctx.Err())
		if err != nil {
			raw = nil
		}
	}()
	if openErr != nil {
		return nil, openErr
	}
	// Deliberately wrap Read without forwarding WriterTo or ReaderFrom. Those
	// fast paths cannot bypass byte, cancellation, or no-progress checks.
	return io.ReadAll(io.LimitReader(&sourceProgressReader{ctx: ctx, reader: r}, MaxBytes+1))
}

type sourceProgressReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *sourceProgressReader) Read(buf []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.reader.Read(buf)
		if cancelled := r.ctx.Err(); cancelled != nil {
			clear(buf)
			return 0, errors.Join(err, cancelled)
		}
		if n < 0 || n > len(buf) {
			clear(buf)
			return 0, io.ErrUnexpectedEOF
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
