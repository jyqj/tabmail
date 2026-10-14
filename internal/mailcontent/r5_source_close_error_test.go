package mailcontent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type r5MIMECloseReader struct {
	read   func([]byte) (int, error)
	close  func() error
	closes atomic.Int32
}

func (r *r5MIMECloseReader) Read(p []byte) (int, error) { return r.read(p) }
func (r *r5MIMECloseReader) Close() error {
	r.closes.Add(1)
	return r.close()
}

func r5MIMECloseEntry(ctx context.Context, entry string, r io.ReadCloser) (bool, error) {
	if entry == "source" {
		p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, nil }))
		raw, err := p.readSource(ctx, "owned-source")
		return raw != nil, err
	}
	env, err := ParseBoundedReader(ctx, r)
	return env != nil, err
}

func TestR5MIMESourceClosePreservesFailures(t *testing.T) {
	for _, entry := range []string{"source", "bounded"} {
		for _, mode := range []string{"normal", "close-only", "empty-close", "read-and-close", "cancel-read-and-close", "cancel-during-close", "cancel-close-only"} {
			t.Run(entry+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				readErr, closeErr := errors.New("owned read failure"), errors.New("owned close failure")
				raw := multipartFixture
				if mode == "empty-close" {
					raw = ""
				}
				reader := &r5MIMECloseReader{read: strings.NewReader(raw).Read, close: func() error { return closeErr }}
				wanted := []error{closeErr}
				switch mode {
				case "normal":
					reader.close = func() error { return nil }
					wanted = nil
				case "read-and-close":
					reader.read = func(p []byte) (int, error) { return copy(p, raw), readErr }
					wanted = append(wanted, readErr)
				case "cancel-read-and-close":
					reader.read = func(p []byte) (int, error) { cancel(); return copy(p, raw), readErr }
					wanted = append(wanted, readErr, context.Canceled)
				case "cancel-during-close":
					reader.close = func() error { cancel(); return closeErr }
					wanted = append(wanted, context.Canceled)
				case "cancel-close-only":
					reader.close = func() error { cancel(); return nil }
					wanted = []error{context.Canceled}
				}
				value, err := r5MIMECloseEntry(ctx, entry, reader)
				if mode == "normal" {
					if !value || err != nil {
						t.Errorf("normal owned source changed: value=%v err=%v", value, err)
					}
				} else {
					if value || err == nil {
						t.Errorf("failed owned source released content: value=%v err=%v", value, err)
					}
					for _, cause := range wanted {
						if !errors.Is(err, cause) {
							t.Errorf("owned source lost cause %v: %v", cause, err)
						}
					}
				}
				if reader.closes.Load() != 1 {
					t.Errorf("owned reader closed %d times, want exactly once", reader.closes.Load())
				}
			})
		}
	}
}

func TestR5MIMESourceClosePreservesOpenErrors(t *testing.T) {
	for _, mode := range []string{"open-and-close", "cancel-open-and-close", "nil-open-error", "cancel-nil-open-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			openErr, closeErr := errors.New("owned open failure"), errors.New("owned close failure")
			reader := &r5MIMECloseReader{read: func([]byte) (int, error) {
				t.Error("read after object open already failed")
				return 0, io.EOF
			}, close: func() error { return closeErr }}
			p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
				if strings.HasPrefix(mode, "cancel-") {
					cancel()
				}
				if strings.Contains(mode, "nil-") {
					return nil, openErr
				}
				return reader, openErr
			}))
			raw, err := p.readSource(ctx, "open-failed")
			if raw != nil || !errors.Is(err, openErr) {
				t.Errorf("source open cause or failed result changed: raw=%v err=%v", raw != nil, err)
			}
			if strings.HasPrefix(mode, "cancel-") && !errors.Is(err, context.Canceled) {
				t.Errorf("open cancellation cause lost: %v", err)
			}
			if !strings.Contains(mode, "nil-") {
				if !errors.Is(err, closeErr) || reader.closes.Load() != 1 {
					t.Errorf("failed open lost owned close: err=%v closes=%d", err, reader.closes.Load())
				}
			} else if reader.closes.Load() != 0 {
				t.Error("closed a reader that was never returned")
			}
		})
	}
}

func TestR5MIMESourceCloseFailureNeverCaches(t *testing.T) {
	for _, entry := range []string{"envelope", "document", "attachment"} {
		t.Run(entry, func(t *testing.T) {
			ctx := context.Background()
			message := uuid.New()
			// A separate healthy source establishes a real stable attachment ID.
			healthy := New(&fixtureObjects{raw: []byte(multipartFixture)})
			doc, err := healthy.Document(ctx, message, "owned-source")
			if err != nil || len(doc.Parts) != 1 {
				t.Fatalf("healthy MIME fixture: %v", err)
			}
			closeErr := errors.New("owned close failure")
			var opens, closes atomic.Int32
			p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
				attempt := opens.Add(1)
				return &r5MIMECloseReader{read: strings.NewReader(multipartFixture).Read, close: func() error {
					closes.Add(1)
					if attempt == 1 {
						return closeErr
					}
					return nil
				}}, nil
			}))
			call := func() (bool, error) {
				switch entry {
				case "envelope":
					env, err := p.Envelope(ctx, "owned-source")
					return env != nil, err
				case "document":
					d, err := p.Document(ctx, message, "owned-source")
					return d != nil, err
				default:
					part, body, err := p.Attachment(ctx, message, "owned-source", doc.Parts[0].ID)
					return part != nil || body != nil, err
				}
			}
			if value, err := call(); value || !errors.Is(err, closeErr) {
				t.Errorf("parser accepted a failed object close: value=%v err=%v", value, err)
			}
			p.mu.Lock()
			cached, retained := p.lru.Len(), p.bytes
			p.mu.Unlock()
			if cached != 0 || retained != 0 || len(p.parses) != 0 {
				t.Errorf("failed close retained cached content or admission: entries=%d bytes=%d slots=%d", cached, retained, len(p.parses))
			}
			for i := 0; i < 2; i++ {
				if value, err := call(); !value || err != nil {
					t.Errorf("healthy retry/cache read failed: value=%v err=%v", value, err)
				}
			}
			if opens.Load() != 2 || closes.Load() != 2 {
				t.Errorf("failed source was cached or healthy result reopened: opens=%d closes=%d", opens.Load(), closes.Load())
			}
		})
	}
}

func TestR5MIMESourceCloseWaitsForCancellationCallback(t *testing.T) {
	for _, entry := range []string{"source", "bounded"} {
		t.Run(entry, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, readReleased, closeReleased := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(closeReleased) })
			unblockRead := sync.OnceFunc(func() { close(readReleased) })
			defer release()
			defer unblockRead()
			readErr, closeErr := errors.New("read interrupted by close"), errors.New("cancellation close failure")
			reader := &r5MIMECloseReader{
				read:  func([]byte) (int, error) { close(started); <-readReleased; return 0, readErr },
				close: func() error { unblockRead(); <-closeReleased; return closeErr },
			}
			type result struct {
				value bool
				err   error
			}
			done := make(chan result, 1)
			go func() { value, err := r5MIMECloseEntry(ctx, entry, reader); done <- result{value, err} }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("owned source did not enter Read")
			}
			cancel()
			// sync.Once waits use a Mutex, which is not durable blocking in
			// synctest. Keep this real callback check explicitly time-bounded.
			var got result
			early := false
			select {
			case got = <-done:
				early = true
				t.Error("source returned before cancellation Close completed")
			case <-time.After(20 * time.Millisecond):
			}
			release()
			if !early {
				select {
				case got = <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("owned source did not finish after Close was released")
				}
			}
			if got.value || !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, readErr) || !errors.Is(got.err, closeErr) || reader.closes.Load() != 1 {
				t.Errorf("callback ownership/error boundary failed: value=%v err=%v closes=%d", got.value, got.err, reader.closes.Load())
			}
		})
	}
}
