package rawobject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"tabmail/internal/models"
)

// BlobStore is the blob backend that holds raw .eml objects. It is the subset of
// the application's object store that the lifecycle needs, declared with the
// same io.Reader Put signature so the real ObjectStore satisfies it directly.
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
}

// ReferenceStore is the metadata side of the lifecycle: it creates rows that
// reference a raw object — holding an advisory lock on the content key and
// invoking the supplied ensure callback to re-put a concurrently-reaped object
// before the referencing row commits — and reaps objects that no row
// references. Store composes it so callers never see the lock or the callback.
type ReferenceStore interface {
	CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int, ensure func(context.Context) error) (bool, error)
	CreateIngestJob(ctx context.Context, job *models.IngestJob, ensure func(context.Context) error) error
	Reaper
}

// Store concentrates the raw-object lifecycle behind one interface: content-
// addressed deduplicated writes, atomic creation of referencing rows (re-putting
// the object under the store's advisory lock if a concurrent sweep reaped it),
// and reference-counted release. Callers hand it bytes and rows; the dedup rule
// and the advisory-lock re-put protocol stay internal seams they never touch.
type Store struct {
	blob BlobStore
	refs ReferenceStore
}

// NewStore builds a lifecycle Store over a blob backend and the metadata
// reference store.
func NewStore(blob BlobStore, refs ReferenceStore) *Store {
	return &Store{blob: blob, refs: refs}
}

// Key returns the deterministic content-addressed object key for raw bytes.
func Key(raw []byte) string {
	sum := sha256.Sum256(raw)
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("sha256/%s/%s.eml", hexSum[:2], hexSum)
}

// Put writes raw under its content key, deduplicating: an object already present
// (same content) is left untouched. Returns the content key.
func (s *Store) Put(ctx context.Context, raw []byte) (string, error) {
	key := Key(raw)
	exists, err := s.blob.Exists(ctx, key)
	if err != nil {
		return "", fmt.Errorf("checking raw object existence: %w", err)
	}
	if !exists {
		if err := s.blob.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))); err != nil {
			return "", fmt.Errorf("storing raw .eml: %w", err)
		}
	}
	return key, nil
}

// StoreMessage inserts a message row referencing raw, enforcing the mailbox
// message quota, and returns whether the row was created. If a concurrent sweep
// reaped the object, it is re-put under the store's advisory lock — the caller
// never constructs that callback.
func (s *Store) StoreMessage(ctx context.Context, m *models.Message, raw []byte, maxMessages int) (bool, error) {
	return s.refs.CreateMessageWithQuota(ctx, m, maxMessages, s.ensure(raw))
}

// StoreIngestJob inserts an ingest job referencing raw, with the same re-put
// guarantee as StoreMessage.
func (s *Store) StoreIngestJob(ctx context.Context, job *models.IngestJob, raw []byte) error {
	return s.refs.CreateIngestJob(ctx, job, s.ensure(raw))
}

// Release deletes the object identified by key, but only when no message or
// ingest job still references it; the decision is made atomically by the
// reference store under an advisory lock on the key.
func (s *Store) Release(ctx context.Context, key string) (Outcome, error) {
	return Release(ctx, s.refs, s.blob, key)
}

// ensure returns the re-put callback the reference store invokes under its
// advisory lock to guarantee the object is present before a referencing row
// commits.
func (s *Store) ensure(raw []byte) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := s.Put(ctx, raw)
		return err
	}
}
