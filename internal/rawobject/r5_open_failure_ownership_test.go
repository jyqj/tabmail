package rawobject_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"testing"
	"time"

	"tabmail/internal/rawobject"
	"tabmail/internal/testutil"
)

type advanceOpenBlob struct {
	*integrityBlob
	afterOpen func()
}

func (s *advanceOpenBlob) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := s.integrityBlob.Get(ctx, key)
	if s.afterOpen != nil {
		s.afterOpen()
	}
	return reader, err
}

func TestR5AdvanceRawOpenFailureOwnership(t *testing.T) {
	for _, storedContent := range []string{"matching", "corrupt"} {
		for _, failure := range []string{"open", "open-close", "cancel-open", "cancel-close"} {
			t.Run(storedContent+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				raw := []byte("owned original bytes")
				stored := append([]byte(nil), raw...)
				if storedContent == "corrupt" {
					stored[0] ^= 1
				}
				openCause := errors.New("synthetic storage acquisition failure")
				closeCause := errors.New("synthetic storage close failure")
				openErr := &fs.PathError{Op: "get", Path: "owned-fixture", Err: openCause}
				reader := &integrityReader{read: func([]byte) (int, error) {
					return 0, errors.New("must not read a failed acquisition")
				}}
				reader.close = func() error {
					if failure == "cancel-close" {
						cancel()
					}
					if failure == "open-close" || failure == "cancel-close" {
						return closeCause
					}
					return nil
				}
				blobs := &advanceOpenBlob{integrityBlob: &integrityBlob{
					MemoryObjectStore: testutil.NewMemoryObjectStore(), reader: reader, getErr: openErr,
				}}
				if failure == "cancel-open" {
					blobs.afterOpen = cancel
				}
				key := rawobject.Key(raw)
				if err := blobs.MemoryObjectStore.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
					t.Fatal(err)
				}
				got, err := rawobject.NewStore(blobs, nil).Put(ctx, raw)
				var pathErr *fs.PathError
				if got != "" || !errors.Is(err, openCause) || !errors.As(err, &pathErr) || pathErr != openErr {
					t.Errorf("failed acquisition released a key or lost original typed error: key=%q error=%v", got, err)
				}
				if (failure == "open-close" || failure == "cancel-close") && !errors.Is(err, closeCause) {
					t.Errorf("lost owned reader Close error: %v", err)
				}
				if (failure == "cancel-open" || failure == "cancel-close") && !errors.Is(err, context.Canceled) {
					t.Errorf("lost acquisition/cleanup cancellation: %v", err)
				}
				if reader.reads.Load() != 0 || reader.closes.Load() != 1 || blobs.gets != 1 || blobs.puts != 0 || blobs.deletes != 0 {
					t.Errorf("failed acquisition ownership or repair effects: reads=%d closes=%d gets=%d puts=%d deletes=%d", reader.reads.Load(), reader.closes.Load(), blobs.gets, blobs.puts, blobs.deletes)
				}
				original, err := blobs.MemoryObjectStore.Get(context.Background(), key)
				if err != nil {
					t.Fatal(err)
				}
				preserved, readErr := io.ReadAll(original)
				closeErr := original.Close()
				if readErr != nil || closeErr != nil || !bytes.Equal(preserved, stored) {
					t.Fatalf("uncertain original was overwritten: read=%v close=%v", readErr, closeErr)
				}
			})
		}
	}
}

func TestR5AdvanceRawOpenOwnershipControls(t *testing.T) {
	for _, outcome := range []string{"nil-reader-error", "nil-reader-success", "nil-reader-cancel", "matching", "repair"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			raw := []byte("owned original bytes")
			stored := append([]byte(nil), raw...)
			openErr := errors.New("synthetic storage acquisition failure")
			blobs := &advanceOpenBlob{integrityBlob: &integrityBlob{MemoryObjectStore: testutil.NewMemoryObjectStore()}}
			if outcome == "nil-reader-error" {
				blobs.getErr = openErr
			}
			if outcome == "nil-reader-cancel" {
				blobs.afterOpen = cancel
			}
			if outcome == "repair" {
				stored[0] ^= 1
			}
			reader := &integrityReader{read: bytes.NewReader(stored).Read}
			if outcome == "matching" || outcome == "repair" {
				blobs.reader = reader
			}
			key := rawobject.Key(raw)
			if err := blobs.MemoryObjectStore.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
				t.Fatal(err)
			}
			got, err := rawobject.NewStore(blobs, nil).Put(ctx, raw)
			switch outcome {
			case "nil-reader-error", "nil-reader-success", "nil-reader-cancel":
				if got != "" || err == nil || blobs.puts != 0 || reader.closes.Load() != 0 {
					t.Fatalf("missing reader became success or repair: key=%q error=%v", got, err)
				}
				if outcome == "nil-reader-error" && !errors.Is(err, openErr) {
					t.Errorf("lost nil-reader opening cause: %v", err)
				}
				if outcome == "nil-reader-cancel" && !errors.Is(err, context.Canceled) {
					t.Errorf("lost nil-reader cancellation: %v", err)
				}
			default:
				wantPut := 0
				if outcome == "repair" {
					wantPut = 1
				}
				if got != key || err != nil || reader.closes.Load() != 1 || blobs.puts != wantPut {
					t.Fatalf("clean dedup/repair changed: key=%q error=%v closes=%d puts=%d", got, err, reader.closes.Load(), blobs.puts)
				}
			}
		})
	}
}

func TestR5AdvanceRawOpenFailureJoinsCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := []byte("owned original bytes")
	openErr, closeErr := errors.New("synthetic open failure"), errors.New("synthetic cleanup failure")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	reader := &integrityReader{read: func([]byte) (int, error) { return 0, fmt.Errorf("must not read failed acquisition") }, close: func() error {
		close(entered)
		<-release
		return closeErr
	}}
	blobs := &integrityBlob{MemoryObjectStore: testutil.NewMemoryObjectStore(), reader: reader, getErr: openErr}
	if err := blobs.MemoryObjectStore.Put(ctx, rawobject.Key(raw), bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	type result struct {
		key string
		err error
	}
	done := make(chan result, 1)
	go func() {
		key, err := rawobject.NewStore(blobs, nil).Put(ctx, raw)
		done <- result{key, err}
	}()
	select {
	case <-entered:
	case out := <-done:
		t.Fatalf("returned without closing acquired resource: key=%q error=%v", out.key, out.err)
	case <-time.After(time.Second):
		t.Fatal("owned resource cleanup did not start")
	}
	cancel()
	select {
	case out := <-done:
		t.Fatalf("returned while cleanup still owned the reader: %+v", out)
	default:
	}
	finish()
	select {
	case out := <-done:
		if out.key != "" || !errors.Is(out.err, openErr) || !errors.Is(out.err, closeErr) || !errors.Is(out.err, context.Canceled) ||
			reader.closes.Load() != 1 || reader.reads.Load() != 0 || blobs.puts != 0 {
			t.Fatalf("cleanup ownership/cause mismatch: result=%+v reads=%d closes=%d puts=%d", out, reader.reads.Load(), reader.closes.Load(), blobs.puts)
		}
	case <-time.After(time.Second):
		t.Fatal("raw object operation did not join released cleanup")
	}
}
