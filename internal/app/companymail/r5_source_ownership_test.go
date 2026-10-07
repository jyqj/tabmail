package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/app"
)

type nextSourceReader struct {
	read     func([]byte) (int, error)
	closeErr error
	onClose  func()
	closes   atomic.Int64
	once     sync.Once
	closed   chan struct{}
}

func (r *nextSourceReader) Read(p []byte) (int, error) { return r.read(p) }
func (r *nextSourceReader) Close() error {
	r.closes.Add(1)
	if r.onClose != nil {
		r.onClose()
	}
	if r.closed != nil {
		r.once.Do(func() { close(r.closed) })
	}
	return r.closeErr
}

type nextSourceObjects struct {
	ObjectStore
	reader io.ReadCloser
	err    error
	onGet  func()
	gets   atomic.Int64
}

func (o *nextSourceObjects) Get(context.Context, string) (io.ReadCloser, error) {
	o.gets.Add(1)
	if o.onGet != nil {
		o.onGet()
	}
	return o.reader, o.err
}

func TestNextSourceOpenFailureOwnsEveryReturnedReader(t *testing.T) {
	getErr, closeErr := errors.New("object get fault"), errors.New("object close fault")
	for _, mode := range []string{"get-error", "get-and-close-error", "cancel-with-reader", "cancel-and-get-error", "missing-reader", "cancel-missing-reader"} {
		t.Run(mode, func(t *testing.T) {
			_, repo, _, actor, mailbox, message := boundaryFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &nextSourceReader{read: bytes.NewReader(nil).Read}
			objects := &nextSourceObjects{reader: reader}
			wantGet, wantClose, wantCancel := false, false, false
			switch mode {
			case "get-error":
				objects.err, wantGet = getErr, true
			case "get-and-close-error":
				objects.err, reader.closeErr, wantGet, wantClose = getErr, closeErr, true, true
			case "cancel-with-reader":
				objects.onGet, wantCancel = cancel, true
			case "cancel-and-get-error":
				objects.onGet, objects.err, wantGet, wantCancel = cancel, getErr, true, true
			case "missing-reader":
				objects.reader = nil
			case "cancel-missing-reader":
				objects.reader, objects.onGet, wantCancel = nil, cancel, true
			}
			out, err := NewService(repo, objects).Source(ctx, actor, mailbox, message)
			if out != nil {
				_ = out.Close()
				t.Fatal("failed object open returned a source")
			}
			if err == nil || (wantGet && !errors.Is(err, getErr)) || (wantClose && !errors.Is(err, closeErr)) || (wantCancel && !errors.Is(err, context.Canceled)) {
				t.Errorf("missing failure cause: %v", err)
			}
			wantCloses := int64(1)
			if objects.reader == nil {
				wantCloses = 0
			}
			if reader.closes.Load() != wantCloses {
				t.Errorf("reader closes=%d, want %d", reader.closes.Load(), wantCloses)
			}
		})
	}
}

func TestNextSourceCancellationClosesBlockedRead(t *testing.T) {
	for _, firstChunk := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-read", true: "after-first-bytes"}[firstChunk], func(t *testing.T) {
			_, repo, _, actor, mailbox, message := boundaryFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			reader := &nextSourceReader{closed: make(chan struct{})}
			calls := 0
			reader.read = func(p []byte) (int, error) {
				calls++
				if firstChunk && calls == 1 {
					return copy(p, "first bytes"), nil
				}
				close(entered)
				<-reader.closed
				return copy(p, "late private bytes"), io.EOF
			}
			out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(ctx, actor, mailbox, message)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			if firstChunk {
				p := make([]byte, 64)
				if n, e := out.Read(p); e != nil || string(p[:n]) != "first bytes" {
					t.Fatalf("initial authorized bytes: %d %v", n, e)
				}
			}
			type result struct {
				n      int
				err    error
				buffer []byte
			}
			done := make(chan result, 1)
			go func() {
				p := make([]byte, 64)
				n, e := out.Read(p)
				done <- result{n, e, p}
			}()
			<-entered
			cancel()
			select {
			case got := <-done:
				if got.n != 0 || !errors.Is(got.err, context.Canceled) || !bytes.Equal(got.buffer, make([]byte, 64)) {
					t.Errorf("cancelled read released bytes: n=%d err=%v", got.n, got.err)
				}
			case <-time.After(time.Second):
				t.Error("cancellation did not close the owned blocked reader")
				_ = out.Close()
				<-done // Always join the controlled reader, including on the red baseline.
			}
			_ = out.Close()
			if reader.closes.Load() != 1 {
				t.Errorf("reader closes=%d", reader.closes.Load())
			}
		})
	}
}

func TestNextSourceCancellationBeforeReadClosesOwnedStream(t *testing.T) {
	_, repo, _, actor, mailbox, message := boundaryFixture()
	ctx, cancel := context.WithCancel(context.Background())
	reader := &nextSourceReader{closed: make(chan struct{}), read: bytes.NewReader([]byte("mail")).Read}
	out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(ctx, actor, mailbox, message)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cancel()
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Error("unused cancelled source kept its reader open")
	}
}

func TestNextSourceRejectsInvalidReadCountsBeforeAuthorization(t *testing.T) {
	for _, count := range []int{-1, 65} {
		t.Run(map[int]string{-1: "negative", 65: "over-buffer"}[count], func(t *testing.T) {
			_, repo, _, actor, mailbox, message := boundaryFixture()
			reader := &nextSourceReader{read: func(p []byte) (int, error) { copy(p, "invalid private bytes"); return count, nil }}
			out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(context.Background(), actor, mailbox, message)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			// Invalid adapter counts must be rejected before slicing or a later authority check.
			repo.change("revoked")
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("invalid reader count panicked: %v", p)
				}
			}()
			buffer := make([]byte, 64)
			n, err := out.Read(buffer)
			if n != 0 || !errors.Is(err, io.ErrUnexpectedEOF) || !bytes.Equal(buffer, make([]byte, 64)) {
				t.Errorf("invalid read released a count or stale bytes: n=%d err=%v", n, err)
			}
			if reader.closes.Load() != 1 {
				t.Error("invalid source remained open")
			}
		})
	}
}

func TestNextSourceCleanEOFAndCloseRemainCompatible(t *testing.T) {
	for _, raw := range []string{"", "authorized mail bytes"} {
		t.Run(map[bool]string{true: "empty", false: "nonempty"}[raw == ""], func(t *testing.T) {
			_, repo, _, actor, mailbox, message := boundaryFixture()
			reader := &nextSourceReader{read: bytes.NewReader([]byte(raw)).Read}
			out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(context.Background(), actor, mailbox, message)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(out)
			if err != nil || string(got) != raw {
				t.Fatalf("source changed: %q %v", got, err)
			}
			if err = out.Close(); err != nil {
				t.Fatal(err)
			}
			if err = out.Close(); err != nil {
				t.Fatal(err)
			}
			if reader.closes.Load() != 1 {
				t.Error("close was not idempotent")
			}
		})
	}
}

func TestNextSourceCloseFailureDoesNotLookLikeCleanEOF(t *testing.T) {
	_, repo, _, actor, mailbox, message := boundaryFixture()
	closeErr := errors.New("source close fault")
	reader := &nextSourceReader{read: bytes.NewReader([]byte("complete bytes")).Read, closeErr: closeErr}
	out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(context.Background(), actor, mailbox, message)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	_, err = io.ReadAll(out)
	if !errors.Is(err, closeErr) {
		t.Errorf("close failure became successful EOF: %v", err)
	}
}

func TestNextSourceDeniedReadKeepsReadAndCloseCauses(t *testing.T) {
	_, repo, _, actor, mailbox, message := boundaryFixture()
	readErr, closeErr := errors.New("read fault"), errors.New("close fault")
	denied := app.Forbidden("current read denied")
	reader := &nextSourceReader{read: func(p []byte) (int, error) { return copy(p, "private bytes"), readErr }, closeErr: closeErr}
	out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(context.Background(), actor, mailbox, message)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	repo.denied = denied
	buffer := make([]byte, 64)
	n, err := out.Read(buffer)
	if n != 0 || !bytes.Equal(buffer, make([]byte, 64)) {
		t.Error("denied bytes released")
	}
	for _, cause := range []error{denied, readErr, closeErr} {
		if !errors.Is(err, cause) {
			t.Errorf("missing cause %v from %v", cause, err)
		}
	}
	expectKind(t, err, app.KindForbidden)
	if reader.closes.Load() != 1 {
		t.Error("reader not closed exactly once")
	}
}

func TestNextSourceCancellationDuringCloseDoesNotReleaseTerminalBytes(t *testing.T) {
	_, repo, _, actor, mailbox, message := boundaryFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &nextSourceReader{read: func(p []byte) (int, error) { return copy(p, "terminal bytes"), io.EOF }, onClose: cancel}
	out, err := NewService(repo, &nextSourceObjects{reader: reader}).Source(ctx, actor, mailbox, message)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	buffer := make([]byte, 64)
	n, err := out.Read(buffer)
	if n != 0 || !errors.Is(err, context.Canceled) || !bytes.Equal(buffer, make([]byte, 64)) {
		t.Errorf("cancellation during final close released a successful read: n=%d err=%v", n, err)
	}
	if reader.closes.Load() != 1 {
		t.Error("reader not closed exactly once")
	}
}
