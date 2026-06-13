// Package rawobject concentrates the reference-counted deletion protocol for
// raw .eml objects. A single stored object is shared across messages and ingest
// jobs (deduplicated by content SHA-256), so it may only be deleted once nothing
// references it. Release is the one home for that count-then-delete decision;
// every caller routes through it and reacts to the returned Outcome rather than
// re-implementing the rule.
//
// The count and the delete are made atomic by the Reaper: the store holds an
// advisory lock on the content key across both, and concurrent inserts that
// re-use the key take the same lock before committing their referencing row, so
// the object cannot be reaped out from under a row that is about to reference it.
package rawobject

import (
	"context"
	"fmt"
	"strings"
)

// Reaper performs the atomic "delete the object iff unreferenced" step. The
// store implements it under an advisory lock keyed by the object key; del is
// invoked only when the live reference count is zero and must be idempotent.
type Reaper interface {
	ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error)
}

// ObjectDeleter removes a raw object from the blob store.
type ObjectDeleter interface {
	Delete(ctx context.Context, key string) error
}

// Outcome describes what Release did, so callers can apply their own metrics,
// retry tracking, and logging without re-deciding the protocol.
type Outcome int

const (
	// Noop means there was nothing to do (empty key or a missing dependency).
	Noop Outcome = iota
	// StillReferenced means the object is kept because rows still reference it.
	StillReferenced
	// Deleted means the object was unreferenced and has been removed.
	Deleted
	// CountFailed means the count/lock step failed; the returned error is set.
	CountFailed
	// DeleteFailed means the object was unreferenced but deletion failed; the
	// returned error is set.
	DeleteFailed
)

// Release deletes the raw object identified by key, but only when no message or
// ingest job still references it. The count-then-delete decision is performed
// atomically by the Reaper (under an advisory lock on the key), giving the
// otherwise-racy window between the reference count and the delete a single,
// serialized home.
func Release(ctx context.Context, reaper Reaper, obj ObjectDeleter, key string) (Outcome, error) {
	key = strings.TrimSpace(key)
	if key == "" || reaper == nil || obj == nil {
		return Noop, nil
	}
	var deleteErr error
	deleted, err := reaper.ReleaseRawObjectIfUnreferenced(ctx, key, func(c context.Context) error {
		if e := obj.Delete(c, key); e != nil {
			deleteErr = fmt.Errorf("delete raw object: %w", e)
			return deleteErr
		}
		return nil
	})
	switch {
	case deleteErr != nil:
		return DeleteFailed, deleteErr
	case err != nil:
		return CountFailed, fmt.Errorf("release raw object: %w", err)
	case deleted:
		return Deleted, nil
	default:
		return StillReferenced, nil
	}
}
