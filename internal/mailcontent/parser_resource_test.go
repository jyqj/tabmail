package mailcontent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Virtual time and channel barriers exercise the existing Parser API without
// network, arbitrary real sleeps, a database, or production test hooks.
type resourceObjectFunc func(context.Context, string) (io.ReadCloser, error)

func (f resourceObjectFunc) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return f(ctx, key)
}

type resourceReader struct {
	reader     io.Reader
	before     func()
	afterClose func()
	closes     atomic.Int32
	once       sync.Once
}

func (r *resourceReader) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
	}
	return r.reader.Read(p)
}
func (r *resourceReader) Close() error {
	r.closes.Add(1)
	r.once.Do(func() {
		if r.afterClose != nil {
			r.afterClose()
		}
	})
	return nil
}

func TestParserResourceAdmissionAndCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var active, peak, opened atomic.Int32
		objects := resourceObjectFunc(func(_ context.Context, key string) (io.ReadCloser, error) {
			if key == "cached" {
				return io.NopCloser(strings.NewReader(multipartFixture)), nil
			}
			opened.Add(1)
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			return &resourceReader{reader: strings.NewReader(multipartFixture), before: func() { <-release }, afterClose: func() { active.Add(-1) }}, nil
		})
		p := New(objects)
		if _, err := p.Envelope(context.Background(), "cached"); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 12)
		for i := 0; i < 12; i++ {
			go func(i int) { _, err := p.Envelope(context.Background(), fmt.Sprintf("distinct-%d", i)); done <- err }(i)
		}
		synctest.Wait()
		if got := opened.Load(); got != 4 {
			t.Errorf("opened %d distinct objects before capacity release; want 4", got)
		}
		// A hot cache entry must not queue behind unrelated slow objects.
		cached := make(chan error, 1)
		go func() { _, err := p.Envelope(context.Background(), "cached"); cached <- err }()
		synctest.Wait()
		select {
		case err := <-cached:
			if err != nil {
				t.Error(err)
			}
		default:
			t.Error("cache hit queued behind parser capacity")
		}
		close(release)
		for i := 0; i < 12; i++ {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
		synctest.Wait()
		if peak.Load() > 4 || active.Load() != 0 {
			t.Errorf("peak=%d active=%d", peak.Load(), active.Load())
		}
	})
}

func TestParserResourceTimeoutClosesDeferredReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		closed := make(chan struct{})
		r := &resourceReader{reader: strings.NewReader(multipartFixture), before: func() { <-closed }, afterClose: func() { close(closed) }}
		p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, nil }))
		done := make(chan error, 1)
		go func() { _, err := p.Envelope(context.Background(), "slow"); done <- err }()
		synctest.Wait()
		time.Sleep(31 * time.Second) // synctest's clock, not wall-clock waiting.
		synctest.Wait()
		var result error
		select {
		case result = <-done:
		default:
			t.Error("expired parse kept its reader and shared task alive")
			_ = r.Close() // Drain the old implementation, even on the red baseline.
			result = <-done
		}
		if !errors.Is(result, context.DeadlineExceeded) {
			t.Errorf("expired parse returned %v", result)
		}
		if r.closes.Load() != 1 {
			t.Errorf("reader closed %d times", r.closes.Load())
		}
		if p.lru.Len() != 0 {
			t.Error("expired parse populated cache")
		}
	})
}

func TestParserResourceLateOpenDoesNotPopulateCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &resourceReader{reader: strings.NewReader(multipartFixture)}
		p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
			time.Sleep(31 * time.Second)
			return r, nil // Adapter completed after the shared deadline.
		}))
		value, err := p.Envelope(context.Background(), "late-open")
		if value != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("late open returned value=%t err=%v", value != nil, err)
		}
		if r.closes.Load() != 1 || p.lru.Len() != 0 {
			t.Error("late open leaked reader or populated cache")
		}
	})
}

type resourceEmptyReader struct {
	empty, reads int
	body         *strings.Reader
}

func (r *resourceEmptyReader) Read(p []byte) (int, error) {
	r.reads++
	if r.empty > 0 {
		r.empty--
		return 0, nil
	}
	return r.body.Read(p)
}
func TestParserResourceNoProgressFailsAndRetries(t *testing.T) {
	empty := &resourceEmptyReader{empty: 101, body: strings.NewReader(multipartFixture)}
	r := &resourceReader{reader: empty}
	var opens int
	p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
		opens++
		if opens == 1 {
			return r, nil
		}
		return io.NopCloser(strings.NewReader(multipartFixture)), nil
	}))
	value, err := p.Envelope(context.Background(), "retryable")
	if value != nil || !errors.Is(err, io.ErrNoProgress) {
		t.Errorf("no-progress source returned value=%t err=%v", value != nil, err)
	}
	if empty.reads > 100 || r.closes.Load() != 1 {
		t.Errorf("reads=%d closes=%d", empty.reads, r.closes.Load())
	}
	if _, err = p.Envelope(context.Background(), "retryable"); err != nil {
		t.Error(err)
	}
	if opens != 2 {
		t.Errorf("failed shared load became cached; opens=%d", opens)
	}
}

func TestParserResourceOpenErrorClosesReader(t *testing.T) {
	r := &resourceReader{reader: strings.NewReader(multipartFixture)}
	failure := errors.New("synthetic open failure")
	p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, failure }))
	value, err := p.Envelope(context.Background(), "error-reader")
	if value != nil || !errors.Is(err, failure) {
		t.Errorf("open failure returned %v", err)
	}
	if r.closes.Load() != 1 {
		t.Errorf("open returned a reader and error but reader was closed %d times", r.closes.Load())
	}
}

func TestParserResourceCancelledWaiterPreservesSharedLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var opens atomic.Int32
		r := &resourceReader{reader: strings.NewReader(multipartFixture), before: func() { <-release }}
		p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { opens.Add(1); return r, nil }))
		ctx, cancel := context.WithCancel(context.Background())
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { _, err := p.Envelope(ctx, "shared"); first <- err }()
		synctest.Wait()
		go func() { _, err := p.Envelope(context.Background(), "shared"); second <- err }()
		synctest.Wait()
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled waiter returned %v", err)
		}
		if r.closes.Load() != 0 {
			t.Error("one waiter cancelled another's reader")
		}
		close(release)
		if err := <-second; err != nil {
			t.Error(err)
		}
		synctest.Wait()
		if opens.Load() != 1 || r.closes.Load() != 1 {
			t.Errorf("opens=%d closes=%d", opens.Load(), r.closes.Load())
		}
		if _, err := p.Envelope(context.Background(), "shared"); err != nil || opens.Load() != 1 {
			t.Error("shared result was not cached")
		}
	})
}

func TestParserResourceQueuedDeadlineDoesNotOpenObject(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var opens atomic.Int32
		p := New(resourceObjectFunc(func(ctx context.Context, _ string) (io.ReadCloser, error) {
			opens.Add(1)
			// Deliberately uncooperative opens keep all four slots occupied.
			// Even so, expired queued work must not open a fifth object.
			<-release
			return nil, ctx.Err()
		}))
		done := make(chan error, 5)
		for i := 0; i < 4; i++ {
			go func(i int) { _, err := p.Envelope(context.Background(), fmt.Sprintf("busy-%d", i)); done <- err }(i)
		}
		synctest.Wait()
		go func() { _, err := p.Envelope(context.Background(), "queued"); done <- err }()
		synctest.Wait()
		if opens.Load() != 4 {
			t.Errorf("queued object opened without a slot: %d", opens.Load())
		}
		time.Sleep(31 * time.Second)
		synctest.Wait()
		close(release)
		for i := 0; i < 5; i++ {
			if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("deadline returned %v", err)
			}
		}
		if opens.Load() != 4 {
			t.Errorf("expired queued task opened an object: %d", opens.Load())
		}
	})
}
