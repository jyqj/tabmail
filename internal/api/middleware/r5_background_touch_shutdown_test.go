package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/api/lifecycle"
	"tabmail/internal/models"
)

type r5APITouchSample struct {
	id  uuid.UUID
	ip  string
	ctx context.Context
	at  time.Time
}
type r5APITouchStore struct {
	authStore
	tenant       *models.Tenant
	key          uuid.UUID
	owner        *uuid.UUID
	resolveEnter chan struct{}
	resolveGate  chan struct{}
	samples      chan r5APITouchSample
	touchGate    chan struct{}
	resolves     atomic.Int32
}

func (s *r5APITouchStore) ResolveAPIKey(context.Context, string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	s.resolves.Add(1)
	if s.resolveEnter != nil {
		close(s.resolveEnter)
		<-s.resolveGate
	}
	return s.tenant, &s.key, []string{"*"}, nil, s.owner, nil
}
func (s *r5APITouchStore) GetUser(context.Context, uuid.UUID) (*models.User, error) { return nil, nil }
func (s *r5APITouchStore) TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error {
	s.samples <- r5APITouchSample{id, ip, ctx, time.Now()}
	if s.touchGate != nil {
		<-s.touchGate
	}
	return nil
}
func r5APIMWStore() *r5APITouchStore {
	return &r5APITouchStore{tenant: &models.Tenant{ID: uuid.New()}, key: uuid.New(), samples: make(chan r5APITouchSample, 8)}
}
func r5APIMWGate(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return ch, release
}
func r5APIMWWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("middleware lifecycle barrier not reached")
	}
}
func r5APIMWSample(t *testing.T, s *r5APITouchStore) r5APITouchSample {
	t.Helper()
	select {
	case v := <-s.samples:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("touch did not start")
		return r5APITouchSample{}
	}
}
func r5APIMWRequest() *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "test-only-key")
	r.RemoteAddr = "192.0.2.17:4321"
	return r
}
func r5APIMWStoppedContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
func r5APIMWOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("middleware lost live owner")
	default:
	}
}

func TestR5APIBackgroundShutdownAPIKeyOutlivesResponse(t *testing.T) {
	st := r5APIMWStore()
	var release func()
	st.touchGate, release = r5APIMWGate(t)
	state := NewAuthState(nil)
	h := Auth(st, "test-only", st.tenant.ID.String(), state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r5APIMWRequest())
	if w.Code != 204 {
		t.Fatalf("status=%d", w.Code)
	}
	sample := r5APIMWSample(t, st)
	if sample.id != st.key || sample.ip != "192.0.2.17" {
		t.Fatalf("touch pair=%+v", sample)
	}
	if err := state.StopContext(r5APIMWStoppedContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	r5APIMWOpen(t, state.Done())
	release()
	r5APIMWWait(t, state.Done())
}

func TestR5APIBackgroundShutdownAPIKeyPendingResolve(t *testing.T) {
	st := r5APIMWStore()
	st.resolveEnter = make(chan struct{})
	var resolveRelease func()
	st.resolveGate, resolveRelease = r5APIMWGate(t)
	state := NewAuthState(nil)
	h := Auth(st, "test-only", st.tenant.ID.String(), state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	returned := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { defer close(returned); h.ServeHTTP(w, r5APIMWRequest()) }()
	r5APIMWWait(t, st.resolveEnter)
	state.CloseAdmission()
	r5APIMWOpen(t, state.Done())
	resolveRelease()
	r5APIMWWait(t, returned)
	r5APIMWSample(t, st)
	if w.Code != 204 {
		t.Fatalf("admitted request was dropped: %d", w.Code)
	}
	r5APIMWWait(t, state.Done())
	after := httptest.NewRecorder()
	h.ServeHTTP(after, r5APIMWRequest())
	if after.Code != 503 || st.resolves.Load() != 1 {
		t.Fatalf("new request status=%d resolves=%d", after.Code, st.resolves.Load())
	}
}

func TestR5APIBackgroundShutdownAPIKeyStillBeforeOwnerRejection(t *testing.T) {
	st := r5APIMWStore()
	id := uuid.New()
	st.owner = &id
	state := NewAuthState(nil)
	next := false
	h := Auth(st, "test-only", st.tenant.ID.String(), state)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { next = true }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r5APIMWRequest())
	r5APIMWSample(t, st)
	if w.Code != 401 || next {
		t.Fatalf("owner rejection drift status=%d next=%v", w.Code, next)
	}
	if err := state.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestR5APIBackgroundShutdownAPIKeyThrottleIsInstanceLocal(t *testing.T) {
	a, b := r5APIMWStore(), r5APIMWStore()
	b.key = a.key
	sa, sb := NewAuthState(nil), NewAuthState(nil)
	noop := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	ha := Auth(a, "test-only", a.tenant.ID.String(), sa)(noop)
	hb := Auth(b, "test-only", b.tenant.ID.String(), sb)(noop)
	ha.ServeHTTP(httptest.NewRecorder(), r5APIMWRequest())
	r5APIMWSample(t, a)
	ha.ServeHTTP(httptest.NewRecorder(), r5APIMWRequest())
	if err := sa.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.samples:
		t.Fatal("minute throttle lost")
	default:
	}
	hb.ServeHTTP(httptest.NewRecorder(), r5APIMWRequest())
	r5APIMWSample(t, b)
	if err := sb.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestR5APIBackgroundShutdownAPIKeyOriginalFiveSecondContext(t *testing.T) {
	st := r5APIMWStore()
	var release func()
	st.touchGate, release = r5APIMWGate(t)
	state := NewAuthState(nil)
	h := Auth(st, "test-only", st.tenant.ID.String(), state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	h.ServeHTTP(httptest.NewRecorder(), r5APIMWRequest().WithContext(requestCtx))
	v := r5APIMWSample(t, st)
	deadline, ok := v.ctx.Deadline()
	if !ok || deadline.Sub(v.at) > 5*time.Second || deadline.Sub(v.at) <= 0 || v.ctx.Err() != nil {
		t.Fatalf("touch inherited cancelled request or lost 5s cap: deadline=%v at=%v err=%v", deadline, v.at, v.ctx.Err())
	}
	release()
	if err := state.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestR5APIBackgroundShutdownAPIKeyInvalidLeaseNotSuccess(t *testing.T) {
	st := r5APIMWStore()
	state := NewAuthState(nil)
	ctx, release, err := state.owner.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, throttled := range []bool{false, true} {
		if throttled {
			state.lastTouch[st.key] = time.Now()
		}
		err := touchAPIKeyAsync(ctx, st, state, st.key, "192.0.2.17:4321")
		if !errors.Is(err, lifecycle.ErrLeaseUnavailable) {
			t.Fatalf("invalid lease hidden by throttle=%v: %v", throttled, err)
		}
	}
	if err := state.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-st.samples:
		t.Fatal("unowned PG task launched")
	default:
	}
}

func TestR5APIBackgroundShutdownStandaloneAuthHasJoinPort(t *testing.T) {
	st := r5APIMWStore()
	var release func()
	st.touchGate, release = r5APIMWGate(t)
	h := Auth(st, "test-only", st.tenant.ID.String())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	join, ok := h.(interface {
		StopContext(context.Context) error
		Done() <-chan struct{}
	})
	if !ok {
		t.Fatal("standalone Auth has no lifecycle port")
	}
	h.ServeHTTP(httptest.NewRecorder(), r5APIMWRequest())
	r5APIMWSample(t, st)
	if err := join.StopContext(r5APIMWStoppedContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	r5APIMWOpen(t, join.Done())
	release()
	r5APIMWWait(t, join.Done())
}

func TestR5APIBackgroundShutdownStreamRevalidationSharesOwnerAndThrottle(t *testing.T) {
	st := r5APIMWStore()
	state := NewAuthState(nil)
	h := Auth(st, "test-only", st.tenant.ID.String(), state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r5APIMWSample(t, st)
		state.CloseAdmission()
		// First revalidation must share the handshake's throttle even without an
		// explicit State argument (legacy callers use context inheritance).
		fresh, err := RevalidateRequest(r, st, "test-only", st.tenant.ID.String())
		if err != nil {
			t.Error(err)
			return
		}
		if err := state.owner.CheckRequest(fresh.Context()); err != nil {
			t.Errorf("returned temporary released lease: %v", err)
			return
		}
		state.mu.Lock()
		state.lastTouch[st.key] = time.Now().Add(-2 * time.Minute)
		state.mu.Unlock()
		// A later loop observation remains admitted at the original touch location.
		fresh, err = RevalidateRequest(fresh, st, "test-only", st.tenant.ID.String(), state)
		if err != nil {
			t.Error(err)
			return
		}
		r5APIMWSample(t, st)
		if err := state.StopContext(r5APIMWStoppedContext()); !errors.Is(err, context.Canceled) {
			t.Errorf("live stream stop=%v", err)
		}
		r5APIMWOpen(t, state.Done())
		w.WriteHeader(204)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r5APIMWRequest())
	if w.Code != 204 {
		t.Fatalf("stream status=%d", w.Code)
	}
	r5APIMWWait(t, state.Done())
	select {
	case <-st.samples:
		t.Fatal("revalidation used a different throttle")
	default:
	}
}
