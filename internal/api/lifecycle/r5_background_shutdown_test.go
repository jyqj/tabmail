package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func r5APIWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("API lifecycle barrier not reached")
	}
}
func r5APIOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("owner falsely reported done")
	default:
	}
}
func r5APICancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestR5APIBackgroundShutdownRegistrationAndRealReturn(t *testing.T) {
	o := New()
	ctx, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(unblock)
	entered := make(chan struct{})
	if err := o.Go(ctx, func() { close(entered); <-gate }); err != nil {
		t.Fatal(err)
	}
	// This assertion does not depend on the goroutine having been scheduled.
	o.mu.Lock()
	tasks := o.tasks
	o.mu.Unlock()
	if tasks != 1 {
		t.Fatalf("registration before launch: tasks=%d", tasks)
	}
	release()
	if err := o.StopContext(r5APICancelled()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	r5APIOpen(t, o.Done())
	o.mu.Lock()
	tasks, requests := o.tasks, o.requests
	o.mu.Unlock()
	if tasks != 1 || requests != 0 {
		t.Fatalf("timeout lost owners: %d/%d", requests, tasks)
	}
	r5APIWait(t, entered)
	unblock()
	r5APIWait(t, o.Done())
	if err := o.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.Enter(context.Background()); !errors.Is(err, ErrStopping) {
		t.Fatalf("reopened: %v", err)
	}
}

func TestR5APIBackgroundShutdownPendingLeaseCanStillSpawn(t *testing.T) {
	o := New()
	ctx, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	o.CloseAdmission()
	if _, _, err := o.Enter(context.Background()); !errors.Is(err, ErrStopping) {
		t.Fatalf("admission=%v", err)
	}
	nested, releaseNested, err := o.Enter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release() // A nested revalidation body remains owned after its parent returns.
	called := make(chan struct{})
	if err := o.Go(nested, func() { close(called) }); err != nil {
		t.Fatal(err)
	}
	r5APIWait(t, called)
	r5APIOpen(t, o.Done())
	releaseNested()
	r5APIWait(t, o.Done())
}

func TestR5APIBackgroundShutdownInvalidLeaseRejected(t *testing.T) {
	o := New()
	ctx, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other := New()
	if err := other.Go(ctx, func() { t.Error("foreign task ran") }); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("foreign=%v", err)
	}
	if _, _, err := other.Enter(ctx); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("foreign enter=%v", err)
	}
	if err := o.Go(context.Background(), func() { t.Error("unowned task ran") }); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("missing=%v", err)
	}
	release()
	release()
	if err := o.Go(ctx, func() { t.Error("released task ran") }); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("released=%v", err)
	}
	if _, _, err := o.Enter(ctx); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("released enter=%v", err)
	}
	if err := o.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestR5APIBackgroundShutdownWrapRejectsBeforeEffects(t *testing.T) {
	o := New()
	calls := 0
	h := o.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	h.CloseAdmission()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusServiceUnavailable || calls != 0 {
		t.Fatalf("status=%d effects=%d", w.Code, calls)
	}
	r5APIWait(t, h.Done())
}

func TestR5APIBackgroundShutdownOriginalDeadlineNotReplaced(t *testing.T) {
	o := New()
	ctx, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stop, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := o.StopContext(stop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop=%v", err)
	}
	r5APIOpen(t, o.Done())
	if err := o.CheckRequest(ctx); err != nil {
		t.Fatalf("timeout invalidated active owner: %v", err)
	}
	release()
	r5APIWait(t, o.Done())
	if err := o.StopContext(stop); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late done became on-time success: %v", err)
	}
}

func TestR5APIBackgroundShutdownRestoreStreamLease(t *testing.T) {
	o := New()
	outer, outerRelease, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer outerRelease()
	inner, innerRelease, err := o.Enter(outer)
	if err != nil {
		t.Fatal(err)
	}
	innerRelease()
	o.CloseAdmission()
	if err := o.CheckRequest(inner); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("inner=%v", err)
	}
	restored := RestoreRequestLease(inner, outer)
	if err := o.CheckRequest(restored); err != nil {
		t.Fatal(err)
	}
	outerRelease()
	if err := o.CheckRequest(restored); !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("escaped stream=%v", err)
	}
}

func TestR5APIBackgroundShutdownConcurrentStopIsIdempotent(t *testing.T) {
	o := New()
	_, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const n = 16
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); o.CloseAdmission(); _ = o.StopContext(r5APICancelled()) }()
	}
	go func() { wg.Wait(); close(done) }()
	r5APIWait(t, done)
	r5APIOpen(t, o.Done())
	release()
	r5APIWait(t, o.Done())
}

func TestR5APIBackgroundShutdownRequestPanicKeepsDetachedOwner(t *testing.T) {
	o := New()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	h := o.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := o.Go(r.Context(), func() { <-gate }); err != nil {
			t.Fatal(err)
		}
		panic("test-only handler panic")
	}))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected handler panic")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if err := o.StopContext(r5APICancelled()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	o.mu.Lock()
	requests, tasks := o.requests, o.tasks
	o.mu.Unlock()
	if requests != 0 || tasks != 1 {
		t.Fatalf("panic lost ownership: %d/%d", requests, tasks)
	}
	r5APIOpen(t, o.Done())
	release()
	r5APIWait(t, o.Done())
}
