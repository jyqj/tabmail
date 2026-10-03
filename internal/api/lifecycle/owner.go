// Package lifecycle owns API request bodies and the work they detach. Admission
// closes for new requests, not for children of a still-running admitted request.
package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

var (
	ErrStopping         = errors.New("API request admission is closed")
	ErrLeaseUnavailable = errors.New("API request lease is missing, foreign, or released")
)

type leaseKey struct{}

type requestLease struct {
	owner  *Owner
	active bool // protected by owner.mu
}

// Owner is per router/run, never global. Construct it with New; do not copy it.
// Neither cancellation nor StopContext returning releases a request/task owner.
type Owner struct {
	mu       sync.Mutex
	stopping bool
	requests int
	tasks    int
	done     chan struct{}
	drained  bool
}

func New() *Owner { return &Owner{done: make(chan struct{})} }

// Enter registers a request before it can use dependencies. Nested entry with a
// live lease is allowed during shutdown and receives its own counted lease. In
// particular, stream revalidation cannot become unowned if its parent returns.
func (o *Owner) Enter(ctx context.Context) (context.Context, func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if parent, ok := ctx.Value(leaseKey{}).(*requestLease); ok {
		if parent.owner != o || !parent.active {
			return ctx, nil, ErrLeaseUnavailable
		}
	} else if o.stopping {
		return ctx, nil, ErrStopping
	}
	lease := &requestLease{owner: o, active: true}
	o.requests++
	return context.WithValue(ctx, leaseKey{}, lease), func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if lease.active {
			lease.active = false
			o.requests--
			o.finishLocked()
		}
	}, nil
}

// RestoreRequestLease keeps freshly revalidated identity values while restoring
// the enclosing stream's lease. Auth's nested lease ends with revalidation; the
// returned request must not retain that already-released temporary lease.
func RestoreRequestLease(ctx, enclosing context.Context) context.Context {
	if lease, ok := enclosing.Value(leaseKey{}).(*requestLease); ok {
		return context.WithValue(ctx, leaseKey{}, lease)
	}
	return ctx
}

// CheckRequest prevents even a throttled observation from hiding an invalid
// lifecycle call. Go validates again atomically with registration.
func (o *Owner) CheckRequest(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	lease, ok := ctx.Value(leaseKey{}).(*requestLease)
	if !ok || lease.owner != o || !lease.active {
		return ErrLeaseUnavailable
	}
	return nil
}

// Go synchronously registers before launch. Only a live request lease can add
// work, including after admission closes. The task releases itself last, after
// fn has actually returned; a timeout does not substitute for that return.
func (o *Owner) Go(ctx context.Context, fn func()) error {
	o.mu.Lock()
	lease, ok := ctx.Value(leaseKey{}).(*requestLease)
	if !ok || lease.owner != o || !lease.active || fn == nil {
		o.mu.Unlock()
		return ErrLeaseUnavailable
	}
	o.tasks++
	o.mu.Unlock()
	go func() {
		defer func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.tasks--
			o.finishLocked()
		}()
		fn()
	}()
	return nil
}

func (o *Owner) finishLocked() {
	if o.stopping && o.requests == 0 && o.tasks == 0 && !o.drained {
		o.drained = true
		close(o.done)
	}
}

func (o *Owner) CloseAdmission() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopping = true
	o.finishLocked()
}

func (o *Owner) Done() <-chan struct{} { return o.done }

// StopContext uses exactly the supplied deadline, not a fresh stop budget. A
// caller may observe a later real Done after timeout, but must not relabel its
// already-failed process shutdown as successful.
func (o *Owner) StopContext(ctx context.Context) error {
	o.CloseAdmission()
	select {
	case <-o.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Handler exposes an actual join port even for standalone middleware callers.
type Handler struct {
	next  http.Handler
	owner *Owner
}

func (o *Owner) Wrap(next http.Handler) *Handler         { return &Handler{next: next, owner: o} }
func (h *Handler) CloseAdmission()                       { h.owner.CloseAdmission() }
func (h *Handler) StopContext(ctx context.Context) error { return h.owner.StopContext(ctx) }
func (h *Handler) Done() <-chan struct{}                 { return h.owner.Done() }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, release, err := h.owner.Enter(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAVAILABLE","message":"API request admission unavailable"}}`))
		return
	}
	defer release()
	h.next.ServeHTTP(w, r.WithContext(ctx))
}
