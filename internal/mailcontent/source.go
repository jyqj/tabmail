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
	if r != nil {
		var once sync.Once
		closeReader := func() { once.Do(func() { _ = r.Close() }) }
		stop := context.AfterFunc(ctx, closeReader)
		defer func() { stop(); closeReader() }()
	}
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("object reader unavailable")
	}
	// Deliberately wrap Read without forwarding WriterTo or ReaderFrom. Those
	// fast paths cannot bypass byte, cancellation, or no-progress checks.
	raw, err := io.ReadAll(io.LimitReader(&sourceProgressReader{ctx: ctx, reader: r}, MaxBytes+1))
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
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
			return 0, cancelled
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
