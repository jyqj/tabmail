package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// shutdownOwner remains owned until its body and stop call really return.
// For Start-style APIs bodyDone means startup returned, never worker drain;
// their stop callback must provide the actual worker join proof.
type shutdownOwner struct {
	name      string
	bodyDone  chan struct{}
	startOnly bool
	stop      func(context.Context) error
	stopDone  chan struct{}
	stopErr   error // published by closing stopDone
}

// shutdownCoordinator owns the one process shutdown origin and budget. Every
// process-level goroutine is registered before launch. Nested work remains the
// component stop/join contract's responsibility. Timeout does not remove an owner, close
// its completion channel, close dependencies, or authorize a replacement.
type shutdownCoordinator struct {
	mu          sync.Mutex
	runCtx      context.Context
	runCancel   context.CancelFunc
	budget      time.Duration
	stopCtx     context.Context
	stopCancel  context.CancelFunc
	origin      string
	owners      []*shutdownOwner
	stopStarted bool
	drained     bool
	finalized   bool
	result      error
	changed     chan struct{}
}

func newShutdownCoordinator(budget time.Duration) *shutdownCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	return &shutdownCoordinator{runCtx: ctx, runCancel: cancel, budget: budget, changed: make(chan struct{}, 1)}
}

func (s *shutdownCoordinator) context() context.Context { return s.runCtx }

func (s *shutdownCoordinator) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *shutdownCoordinator) register(name string, startOnly bool, stop func(context.Context) error) *shutdownOwner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCtx != nil {
		return nil
	}
	o := &shutdownOwner{name: name, bodyDone: make(chan struct{}), startOnly: startOnly, stop: stop, stopDone: make(chan struct{})}
	s.owners = append(s.owners, o)
	return o
}

func (s *shutdownCoordinator) finishBody(o *shutdownOwner) {
	close(o.bodyDone)
	s.notify()
}

// goRun tracks blocking Run/Serve methods, including a body not yet scheduled.
// A server stop callback returning nil is insufficient until Run also returns.
func (s *shutdownCoordinator) goRun(name string, run func(context.Context), stop func(context.Context) error) bool {
	o := s.register(name, false, stop)
	if o == nil {
		return false
	}
	go func() {
		defer s.finishBody(o)
		run(s.runCtx)
	}()
	return true
}

// start tracks a synchronous Start-style method and its separate real join.
// At shutdown the cancelled run context is already immutable; stop cannot take
// an idle worker snapshot until the pending lazy initialization/start returns.
func (s *shutdownCoordinator) start(name string, start func(context.Context), stop func(context.Context) error) bool {
	if stop == nil {
		panic("shutdown: Start owner requires a real stop/join callback")
	}
	o := s.register(name, true, stop)
	if o == nil {
		return false
	}
	defer s.finishBody(o)
	start(s.runCtx)
	return true
}

// requestStop is the sole origin for signals and server errors. The budget is
// captured here, not when drain eventually runs and not once per component.
func (s *shutdownCoordinator) requestStop(reason string) {
	s.mu.Lock()
	if s.stopCtx == nil {
		s.origin = reason
		s.stopCtx, s.stopCancel = context.WithDeadline(context.Background(), time.Now().Add(s.budget))
	}
	s.mu.Unlock()
	s.runCancel()
	s.notify()
}

func (s *shutdownCoordinator) stopOwners() (context.Context, []*shutdownOwner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := s.stopCtx
	owners := append([]*shutdownOwner(nil), s.owners...)
	if s.stopStarted {
		return ctx, owners
	}
	s.stopStarted = true
	for _, o := range owners {
		if o.stop == nil {
			close(o.stopDone)
			continue
		}
		// Each stop call has an owned completion channel. If it ignores ctx,
		// drain reports it by name; its goroutine is never called drained.
		go func(o *shutdownOwner) {
			defer func() { close(o.stopDone); s.notify() }()
			if o.startOnly {
				select {
				case <-o.bodyDone:
				case <-ctx.Done():
					o.stopErr = ctx.Err()
					return
				}
			}
			o.stopErr = o.stop(ctx)
		}(o)
	}
	return ctx, owners
}

func shutdownClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func shutdownOwnerErrors(ctx context.Context, owners []*shutdownOwner) (bool, error) {
	allReturned := true
	var failures []error
	for _, o := range owners {
		bodyDone, stopDone := shutdownClosed(o.bodyDone), shutdownClosed(o.stopDone)
		if !bodyDone || !stopDone {
			allReturned = false
			cause := ctx.Err()
			if cause == nil {
				cause = errors.New("still running")
			}
			failures = append(failures, fmt.Errorf("%s: owner has not exited: %w", o.name, cause))
		}
		if stopDone && o.stopErr != nil {
			failures = append(failures, fmt.Errorf("%s: stop/join incomplete: %w", o.name, o.stopErr))
		}
	}
	return allReturned, errors.Join(failures...)
}

// drain never resets the budget. nil proves both stop and body completion for
// every registered owner. Callers must terminate unsuccessfully on error and
// must not run dependency-closing defers in that path.
func (s *shutdownCoordinator) drain() error {
	s.mu.Lock()
	if s.finalized {
		err := s.result
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.requestStop("shutdown")
	ctx, owners := s.stopOwners()
	for {
		complete, err := shutdownOwnerErrors(ctx, owners)
		if complete || ctx.Err() != nil {
			if complete && err == nil {
				return s.finishDrain(nil)
			}
			return s.finishDrain(errors.Join(err, ctx.Err()))
		}
		select {
		case <-s.changed:
		case <-ctx.Done():
		}
	}
}

// A timed-out process shutdown is a terminal failure, not a fresh budget or a
// later opportunity to relabel that same shutdown as graceful success.
func (s *shutdownCoordinator) finishDrain(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finalized {
		if err == nil {
			// Successful joins cannot commit after the original process budget.
			// Check the absolute deadline too: its timer may not have run yet.
			cause := s.stopCtx.Err()
			if deadline, ok := s.stopCtx.Deadline(); cause == nil && ok && !time.Now().Before(deadline) {
				cause = context.DeadlineExceeded
			}
			if cause != nil {
				err = fmt.Errorf("shutdown %s: budget ended before drain commit: %w", s.origin, cause)
			}
		}
		s.finalized = true
		s.result = err
		s.drained = err == nil
	}
	return s.result
}

func (s *shutdownCoordinator) dependenciesMayClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drained
}

func (s *shutdownCoordinator) release() {
	s.runCancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCancel != nil {
		s.stopCancel()
	}
}

// apiShutdownOwner includes detached API work, not just net/http connections.
// An admitted request may register its final child while HTTP is draining.
type apiShutdownOwner interface {
	CloseAdmission()
	StopContext(context.Context) error
}

// stopHTTPAndAPI closes request admission before stopping the listener, then
// joins API-owned request/task bodies even when HTTP shutdown returns an error.
// Both waits use the original process context; neither renews its budget.
func stopHTTPAndAPI(ctx context.Context, stopHTTP func(context.Context) error, owner apiShutdownOwner) error {
	owner.CloseAdmission()
	httpErr := stopHTTP(ctx)
	ownerErr := owner.StopContext(ctx)
	return errors.Join(httpErr, ownerErr)
}
