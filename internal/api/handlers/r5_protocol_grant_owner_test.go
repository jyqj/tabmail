//go:build r5protocol

package handlers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
)

// r5UIBarrierOwner is the batch fixture's lifecycle contract. Register close
// after fixture creation: testing cleanup is LIFO, so join precedes pool/server
// teardown. Result is stable and readable only after Done closes.
type r5UIBarrierOwner interface {
	ownerClose() error
	ownerDone() <-chan struct{}
	ownerResult() error
}

type r5UIGrantOwner struct {
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	release     func() error
	releaseOnce sync.Once
	releaseErr  error
	closeOnce   sync.Once
	result      error
	// Only the coordinator starts/joins this child; close joins via done.
	revoked    chan struct{}
	revokerErr error
}

func r5UINewGrantOwner(ctx context.Context, cancel context.CancelFunc, release func() error, run func(*r5UIGrantOwner) error) *r5UIGrantOwner {
	o := &r5UIGrantOwner{ctx: ctx, cancel: cancel, done: make(chan struct{}), release: release}
	go func() {
		err := run(o)
		// Release locks before joining a revoker that may be waiting for them.
		o.cancel()
		err = errors.Join(err, o.releaseHeld())
		if o.revoked != nil {
			<-o.revoked
			err = errors.Join(err, o.revokerErr)
		}
		o.result = err
		close(o.done)
	}()
	return o
}
func (o *r5UIGrantOwner) releaseHeld() error {
	o.releaseOnce.Do(func() { o.releaseErr = o.release() })
	return o.releaseErr
}
func (o *r5UIGrantOwner) startRevoker(run func(context.Context) error) {
	o.revoked = make(chan struct{})
	go func() { defer close(o.revoked); o.revokerErr = run(o.ctx) }()
}
func (o *r5UIGrantOwner) ownerDone() <-chan struct{} { return o.done }
func (o *r5UIGrantOwner) ownerResult() error         { <-o.done; return o.result }
func (o *r5UIGrantOwner) ownerClose() error {
	o.closeOnce.Do(func() { o.cancel(); o.releaseHeld() })
	return o.ownerResult()
}

var _ r5UIBarrierOwner = (*r5UIGrantOwner)(nil)

func TestR5UIGrantOwnerJoin(t *testing.T) {
	for _, phase := range []string{"no_spawn", "cancel_before_spawn", "blocked_child", "finished_child", "query_failure", "release_failure"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				coordinatorGate, childGate := make(chan struct{}), make(chan struct{})
				coordinatorExited, childExited := false, false
				releaseCalls := 0
				queryErr, childErr, releaseErr := errors.New("query failure"), errors.New("revoker failure"), errors.New("rollback failure")
				owner := r5UINewGrantOwner(ctx, cancel, func() error {
					releaseCalls++
					if phase == "release_failure" {
						return releaseErr
					}
					return nil
				}, func(o *r5UIGrantOwner) error {
					defer func() { coordinatorExited = true }()
					if phase == "cancel_before_spawn" {
						<-ctx.Done()
					}
					if phase != "no_spawn" && phase != "release_failure" {
						o.startRevoker(func(ctx context.Context) error {
							defer func() { childExited = true }()
							if phase != "finished_child" {
								<-childGate
							}
							return childErr
						})
					}
					<-coordinatorGate
					if phase == "query_failure" {
						return queryErr
					}
					return nil
				})
				var first, second error
				closed1, closed2 := false, false
				go func() { first = owner.ownerClose(); closed1 = true }()
				go func() { second = owner.ownerClose(); closed2 = true }()
				synctest.Wait()
				if closed1 || closed2 {
					t.Fatal("close acknowledged before coordinator exit")
				}
				select {
				case <-owner.ownerDone():
					t.Fatal("done published before coordinator exit")
				default:
				}
				close(coordinatorGate)
				synctest.Wait()
				needsChild := phase != "no_spawn" && phase != "release_failure" && phase != "finished_child"
				if needsChild {
					if closed1 || closed2 {
						t.Fatal("close acknowledged while revoker still blocked")
					}
					select {
					case <-owner.ownerDone():
						t.Fatal("done published while revoker still blocked")
					default:
					}
				}
				close(childGate)
				synctest.Wait()
				if !closed1 || !closed2 || !coordinatorExited || (phase != "no_spawn" && phase != "release_failure" && !childExited) {
					t.Fatal("close failed to join all owned work")
				}
				if releaseCalls != 1 {
					t.Fatalf("release calls=%d", releaseCalls)
				}
				for _, result := range []error{first, second, owner.ownerClose(), owner.ownerResult()} {
					if phase != "no_spawn" && phase != "release_failure" && !errors.Is(result, childErr) {
						t.Fatal("revoker error lost")
					}
					if phase == "query_failure" && !errors.Is(result, queryErr) {
						t.Fatal("query error lost")
					}
					if phase == "release_failure" && !errors.Is(result, releaseErr) {
						t.Fatal("release error lost")
					}
					if phase == "no_spawn" && result != nil {
						t.Fatal("unexpected error")
					}
				}
			})
		})
	}
}

func TestR5UIGrantOwnerReleaseBeforeJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		released := make(chan struct{})
		childExited := false
		owner := r5UINewGrantOwner(ctx, cancel, func() error { close(released); return nil }, func(o *r5UIGrantOwner) error {
			o.startRevoker(func(context.Context) error { <-released; childExited = true; return nil })
			<-ctx.Done()
			return nil
		})
		if err := owner.ownerClose(); err != nil {
			t.Fatal(err)
		}
		if !childExited {
			t.Fatal("release failed to unblock/join child")
		}
	})
}

func TestR5UIGrantOwnerFailureJoinsRevoker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		childGate := make(chan struct{})
		queryErr, childErr := errors.New("coordinator query failed"), errors.New("revoker failed")
		released, exited := false, false
		owner := r5UINewGrantOwner(ctx, cancel, func() error { released = true; return nil }, func(o *r5UIGrantOwner) error {
			o.startRevoker(func(context.Context) error { <-childGate; exited = true; return childErr })
			return queryErr
		})
		synctest.Wait()
		if !released || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("failure did not release/cancel owned work")
		}
		select {
		case <-owner.ownerDone():
			t.Fatal("failed coordinator published done before revoker exit")
		default:
		}
		close(childGate)
		result := owner.ownerResult()
		if !exited || !errors.Is(result, queryErr) || !errors.Is(result, childErr) {
			t.Fatal("failure did not join/preserve both results")
		}
		if owner.ownerClose() != result {
			t.Fatal("close changed retained result")
		}
	})
}
