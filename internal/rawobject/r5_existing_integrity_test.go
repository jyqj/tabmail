package rawobject_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/testutil"
)

type integrityReader struct {
	read          func([]byte) (int, error)
	close         func() error
	reads, closes atomic.Int32
	bytes         atomic.Int64
}

func (r *integrityReader) Read(p []byte) (int, error) {
	r.reads.Add(1)
	n, err := r.read(p)
	if n > 0 && n <= len(p) {
		r.bytes.Add(int64(n))
	}
	return n, err
}
func (r *integrityReader) Close() error {
	r.closes.Add(1)
	if r.close != nil {
		return r.close()
	}
	return nil
}

type integrityBlob struct {
	*testutil.MemoryObjectStore
	reader                      io.ReadCloser
	getErr, existsErr, putErr   error
	gets, checks, puts, deletes int
}

func (s *integrityBlob) Get(context.Context, string) (io.ReadCloser, error) {
	s.gets++
	return s.reader, s.getErr
}
func (s *integrityBlob) Exists(ctx context.Context, key string) (bool, error) {
	s.checks++
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.MemoryObjectStore.Exists(ctx, key)
}
func (s *integrityBlob) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	s.puts++
	if s.putErr != nil {
		return s.putErr
	}
	return s.MemoryObjectStore.Put(ctx, key, r, size)
}
func (s *integrityBlob) Delete(ctx context.Context, key string) error {
	s.deletes++
	return s.MemoryObjectStore.Delete(ctx, key)
}

func TestContinueRawExistingIntegrity(t *testing.T) {
	for _, name := range []string{
		"missing", "matching", "different-same-length", "truncated", "oversized", "empty-matching",
		"bounded-endless", "data-with-EOF", "read-error", "close-error", "read-and-close-error",
		"get-error", "exists-error", "repair-error", "nil-reader", "no-progress",
		"negative-count", "impossible-count", "already-cancelled", "cancel-during-read",
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte("owned original bytes")
			stored := append([]byte(nil), raw...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			readErr, closeErr, getErr, putErr := errors.New("synthetic read"), errors.New("synthetic close"), errors.New("synthetic get"), errors.New("synthetic put")
			blobs := &integrityBlob{MemoryObjectStore: testutil.NewMemoryObjectStore()}
			wantPut, wantGet, wantClose, wantCheck := 0, 1, 1, 1
			var wantErrors []error
			wantAnyError := false
			switch name {
			case "missing":
				wantPut, wantGet, wantClose = 1, 0, 0
			case "different-same-length":
				stored[0] ^= 1
				wantPut = 1
			case "truncated":
				stored = stored[:len(stored)-1]
				wantPut = 1
			case "oversized":
				stored = append(stored, 'x')
				wantPut = 1
			case "empty-matching":
				raw, stored = nil, nil
			case "bounded-endless":
				wantPut = 1
			case "read-error":
				wantErrors = []error{readErr}
			case "close-error":
				wantErrors = []error{closeErr}
			case "read-and-close-error":
				wantErrors = []error{readErr, closeErr}
			case "get-error":
				blobs.getErr = getErr
				wantErrors = []error{getErr}
				wantClose = 0
			case "exists-error":
				blobs.existsErr = getErr
				wantErrors = []error{getErr}
				wantGet, wantClose = 0, 0
			case "repair-error":
				stored[0] ^= 1
				blobs.putErr = putErr
				wantErrors = []error{putErr}
				wantPut = 1
			case "nil-reader":
				wantAnyError = true
				wantClose = 0
			case "no-progress":
				wantErrors = []error{io.ErrNoProgress}
			case "negative-count", "impossible-count":
				wantErrors = []error{io.ErrUnexpectedEOF, readErr}
			case "already-cancelled":
				cancel()
				wantErrors = []error{context.Canceled}
				wantGet, wantClose, wantCheck = 0, 0, 0
			case "cancel-during-read":
				wantErrors = []error{context.Canceled, readErr, closeErr}
			}
			key := rawobject.Key(raw)
			if name != "missing" {
				if err := blobs.MemoryObjectStore.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
					t.Fatal(err)
				}
			}
			reader := &integrityReader{read: bytes.NewReader(stored).Read}
			if name == "bounded-endless" {
				reader.read = func(p []byte) (int, error) {
					for i := range p {
						p[i] = 'x'
					}
					return len(p), nil
				}
			}
			if name == "data-with-EOF" {
				reader.read = func(p []byte) (int, error) { return copy(p, stored), io.EOF }
			}
			if name == "read-error" || name == "read-and-close-error" {
				reader.read = func(p []byte) (int, error) { return copy(p, stored), readErr }
			}
			if name == "close-error" || name == "read-and-close-error" || name == "cancel-during-read" {
				reader.close = func() error { return closeErr }
			}
			if name == "no-progress" {
				reader.read = func([]byte) (int, error) { return 0, nil }
			}
			if name == "negative-count" {
				reader.read = func([]byte) (int, error) { return -1, readErr }
			}
			if name == "impossible-count" {
				reader.read = func(p []byte) (int, error) { return len(p) + 1, readErr }
			}
			if name == "cancel-during-read" {
				reader.read = func(p []byte) (int, error) { cancel(); return copy(p, stored), readErr }
			}
			blobs.reader = reader
			if name == "nil-reader" || name == "get-error" {
				blobs.reader = nil
			}
			got, err := rawobject.NewStore(blobs, nil).Put(ctx, raw)
			if len(wantErrors) > 0 || wantAnyError {
				if err == nil || got != "" {
					t.Errorf("uncertain original accepted: key=%q error=%v", got, err)
				}
				for _, cause := range wantErrors {
					if !errors.Is(err, cause) {
						t.Errorf("lost error cause %v in %v", cause, err)
					}
				}
			} else if err != nil || got != key {
				t.Errorf("put key=%q error=%v, want=%q", got, err, key)
			}
			if blobs.puts != wantPut || blobs.gets != wantGet || blobs.checks != wantCheck || reader.closes.Load() != int32(wantClose) || blobs.deletes != 0 {
				t.Errorf("ownership/I/O puts=%d gets=%d checks=%d closes=%d deletes=%d, want %d/%d/%d/%d/0", blobs.puts, blobs.gets, blobs.checks, reader.closes.Load(), blobs.deletes, wantPut, wantGet, wantCheck, wantClose)
			}
			if reader.bytes.Load() > int64(len(raw))+1 || reader.reads.Load() > 101 {
				t.Errorf("unbounded integrity read: bytes=%d reads=%d", reader.bytes.Load(), reader.reads.Load())
			}
			if name == "no-progress" && reader.reads.Load() != 100 {
				t.Errorf("zero-progress budget=%d want100", reader.reads.Load())
			}
			if name != "missing" || wantPut == 1 {
				r, readBackErr := blobs.MemoryObjectStore.Get(context.Background(), key)
				if readBackErr != nil {
					t.Fatal(readBackErr)
				}
				data, _ := io.ReadAll(r)
				_ = r.Close()
				want := stored
				if err == nil {
					want = raw
				}
				if !bytes.Equal(data, want) {
					t.Errorf("wrong persisted bytes: %q want %q", data, want)
				}
			}
		})
	}
}

func TestContinueRawCancellationWaitsForOwnedClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readStarted, readReturned, closeStarted, releaseClose := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce, closeSignal sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseClose) }) }
	readErr, closeErr := errors.New("blocked read released"), errors.New("close completion failure")
	reader := &integrityReader{
		read:  func([]byte) (int, error) { close(readStarted); <-closeStarted; close(readReturned); return 0, readErr },
		close: func() error { closeSignal.Do(func() { close(closeStarted) }); <-releaseClose; return closeErr },
	}
	blobs := &integrityBlob{MemoryObjectStore: testutil.NewMemoryObjectStore(), reader: reader}
	raw := []byte("owned blocked read")
	_ = blobs.MemoryObjectStore.Put(ctx, rawobject.Key(raw), bytes.NewReader(raw), int64(len(raw)))
	done := make(chan error, 1)
	joined := make(chan struct{})
	go func() { defer close(joined); _, err := rawobject.NewStore(blobs, nil).Put(ctx, raw); done <- err }()
	t.Cleanup(func() {
		cancel()
		release()
		if reader.closes.Load() == 0 {
			_ = reader.Close()
		}
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("verification goroutine did not join")
		}
	})
	select {
	case <-readStarted:
	case err := <-done:
		t.Fatalf("existing object was never verified: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not start")
	}
	cancel()
	select {
	case <-closeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not close blocked reader")
	}
	select {
	case <-readReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release Read")
	}
	// Mutex waits are not durably blocked in testing/synctest. Use owned
	// channels and a bounded observation window for the real Close join.
	select {
	case err := <-done:
		t.Fatalf("returned before Close completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not finish after Close")
	}
	for _, cause := range []error{context.Canceled, readErr, closeErr} {
		if !errors.Is(err, cause) {
			t.Errorf("missing joined cause %v: %v", cause, err)
		}
	}
	if reader.closes.Load() != 1 || blobs.puts != 0 || blobs.deletes != 0 {
		t.Errorf("cancellation mutated object or repeated close: close=%d put=%d delete=%d", reader.closes.Load(), blobs.puts, blobs.deletes)
	}
}

type integrityFile struct {
	*fileobj.FileStore
	puts, gets int
}

func (s *integrityFile) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	s.puts++
	return s.FileStore.Put(ctx, key, r, n)
}
func (s *integrityFile) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	s.gets++
	return s.FileStore.Get(ctx, key)
}

// The actual filesystem publication and the reference store's ensure callback
// run here. Metadata is synthetic; this is not a PostgreSQL lock proof.
func TestContinueRawFileIntegrityAndReferenceEnsure(t *testing.T) {
	for _, kind := range []string{"direct", "message", "job"} {
		for _, damage := range []string{"matching", "same-size", "truncated", "oversized"} {
			t.Run(kind+"/"+damage, func(t *testing.T) {
				fs, err := fileobj.New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				blob := &integrityFile{FileStore: fs}
				refs := testutil.NewFakeStore()
				box := uuid.New()
				refs.SeedMailbox(&models.Mailbox{ID: box, TenantID: uuid.New(), ZoneID: uuid.New()})
				raw := []byte("Subject: synthetic original\r\n\r\nowned immutable body")
				key := rawobject.Key(raw)
				stored := append([]byte(nil), raw...)
				wantPut := 1
				switch damage {
				case "matching":
					wantPut = 0
				case "same-size":
					stored[0] ^= 1
				case "truncated":
					stored = stored[:len(stored)-1]
				case "oversized":
					stored = append(stored, 'x')
				}
				if err := fs.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
					t.Fatal(err)
				}
				// Keep an actual open descriptor across repair: atomic publication
				// must preserve that reader's old inode instead of truncating it.
				old, err := fs.Get(context.Background(), key)
				if err != nil {
					t.Fatal(err)
				}
				defer old.Close()
				s := rawobject.NewStore(blob, refs)
				switch kind {
				case "direct":
					_, err = s.Put(context.Background(), raw)
				case "message":
					var created bool
					created, err = s.StoreMessage(context.Background(), &models.Message{MailboxID: box, RawObjectKey: key, Size: int64(len(raw))}, raw, 10)
					if !created {
						t.Error("message reference not created")
					}
				case "job":
					err = s.StoreIngestJob(context.Background(), &models.IngestJob{RawObjectKey: key, Source: "smtp"}, raw)
				}
				if err != nil {
					t.Fatal(err)
				}
				r, err := fs.Get(context.Background(), key)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				_ = r.Close()
				if err != nil || !bytes.Equal(data, raw) {
					t.Errorf("reference points at corrupt bytes: %q %v", data, err)
				}
				oldData, err := io.ReadAll(old)
				if err != nil || !bytes.Equal(oldData, stored) {
					t.Errorf("repair truncated existing reader: %q %v", oldData, err)
				}
				if blob.puts != wantPut || blob.gets != 1 {
					t.Errorf("dedup puts=%d gets=%d want=%d/1", blob.puts, blob.gets, wantPut)
				}
				if kind != "direct" {
					outcome, err := s.Release(context.Background(), key)
					if err != nil || outcome != rawobject.StillReferenced {
						t.Errorf("live reference lost during GC: outcome=%v err=%v", outcome, err)
					}
				}
			})
		}
	}
}

func TestContinueRawEnsureUncertainReadCannotCommitReference(t *testing.T) {
	for _, kind := range []string{"message", "job"} {
		t.Run(kind, func(t *testing.T) {
			refs := testutil.NewFakeStore()
			box := uuid.New()
			refs.SeedMailbox(&models.Mailbox{ID: box, TenantID: uuid.New(), ZoneID: uuid.New()})
			raw := []byte("synthetic original")
			key := rawobject.Key(raw)
			closeErr := errors.New("uncertain source close")
			r := &integrityReader{read: strings.NewReader(string(raw)).Read, close: func() error { return closeErr }}
			blobs := &integrityBlob{MemoryObjectStore: testutil.NewMemoryObjectStore(), reader: r}
			_ = blobs.MemoryObjectStore.Put(context.Background(), key, bytes.NewReader(raw), int64(len(raw)))
			s := rawobject.NewStore(blobs, refs)
			var err error
			if kind == "message" {
				var created bool
				created, err = s.StoreMessage(context.Background(), &models.Message{MailboxID: box, RawObjectKey: key}, raw, 10)
				if created {
					t.Error("uncertain source committed message")
				}
			} else {
				err = s.StoreIngestJob(context.Background(), &models.IngestJob{RawObjectKey: key, Source: "smtp"}, raw)
			}
			if !errors.Is(err, closeErr) {
				t.Errorf("uncertain close accepted: %v", err)
			}
			count, countErr := refs.CountRawObjectReferences(context.Background(), key)
			if countErr != nil || count != 0 || r.closes.Load() != 1 || blobs.puts != 0 || blobs.deletes != 0 {
				t.Errorf("unexpected reference/I/O: refs=%d err=%v close=%d put=%d delete=%d", count, countErr, r.closes.Load(), blobs.puts, blobs.deletes)
			}
		})
	}
}
