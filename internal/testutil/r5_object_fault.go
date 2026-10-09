package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"tabmail/internal/store"
)

var ErrR5ObjectFault = errors.New("explicit R5 object port fault")

type R5ObjectCall struct {
	Operation, KeySHA256 string
	Faulted              bool
}

// R5ObjectFault wraps an actual ObjectStore port, not an HTTP response mock.
// Call metadata never records payload or raw object keys.
type R5ObjectFault struct {
	base      store.ObjectStore
	mu        sync.Mutex
	next      map[string]error
	calls     []R5ObjectCall
	shortRead *int64
}

func NewR5ObjectFault(base store.ObjectStore) *R5ObjectFault {
	return &R5ObjectFault{base: base, next: map[string]error{}}
}
func (f *R5ObjectFault) FailNext(operation string, err error) error {
	if operation != "put" && operation != "get" && operation != "delete" && operation != "exists" {
		return errors.New("unknown R5 object operation")
	}
	if err == nil {
		return errors.New("R5 object fault must be non-nil")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next[operation] = err
	return nil
}
func (f *R5ObjectFault) ShortNextGet(limit int64) error {
	if limit < 0 {
		return errors.New("negative R5 short-read limit")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shortRead = &limit
	return nil
}
func (f *R5ObjectFault) Calls() []R5ObjectCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]R5ObjectCall(nil), f.calls...)
}
func (f *R5ObjectFault) begin(ctx context.Context, operation, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := ctx.Err()
	if err == nil {
		err = f.next[operation]
		delete(f.next, operation)
	}
	if err == nil && f.base == nil {
		err = errors.New("R5 object port requires an underlying store")
	}
	hash := sha256.Sum256([]byte(key))
	f.calls = append(f.calls, R5ObjectCall{Operation: operation, KeySHA256: hex.EncodeToString(hash[:]), Faulted: err != nil})
	return err
}
func (f *R5ObjectFault) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := f.begin(ctx, "put", key); err != nil {
		return err
	}
	return f.base.Put(ctx, key, r, size)
}
func (f *R5ObjectFault) Delete(ctx context.Context, key string) error {
	if err := f.begin(ctx, "delete", key); err != nil {
		return err
	}
	return f.base.Delete(ctx, key)
}
func (f *R5ObjectFault) Exists(ctx context.Context, key string) (bool, error) {
	if err := f.begin(ctx, "exists", key); err != nil {
		return false, err
	}
	return f.base.Exists(ctx, key)
}
func (f *R5ObjectFault) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := f.begin(ctx, "get", key); err != nil {
		return nil, err
	}
	reader, err := f.base.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	limit := f.shortRead
	f.shortRead = nil
	f.mu.Unlock()
	if limit != nil {
		return &r5LimitedReadCloser{Reader: io.LimitReader(reader, *limit), Closer: reader}, nil
	}
	return reader, nil
}

type r5LimitedReadCloser struct {
	io.Reader
	io.Closer
}

var _ store.ObjectStore = (*R5ObjectFault)(nil)
