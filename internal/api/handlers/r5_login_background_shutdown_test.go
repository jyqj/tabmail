package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/lifecycle"
	"tabmail/internal/models"
)

type r5APILoginStore struct {
	authStore
	user                    *models.User
	lookupEnter, lookupGate chan struct{}
	tokenEnter, tokenGate   chan struct{}
	touchGate               chan struct{}
	touches                 chan context.Context
	lookups, tokens         atomic.Int32
}

func (s *r5APILoginStore) GetUserByEmail(context.Context, string) (*models.User, error) {
	s.lookups.Add(1)
	if s.lookupEnter != nil {
		close(s.lookupEnter)
		<-s.lookupGate
	}
	return s.user, nil
}
func (s *r5APILoginStore) CreateRefreshToken(context.Context, *models.RefreshToken) error {
	s.tokens.Add(1)
	if s.tokenEnter != nil {
		close(s.tokenEnter)
		<-s.tokenGate
	}
	return nil
}
func (s *r5APILoginStore) TouchUserLogin(ctx context.Context, id uuid.UUID) error {
	s.touches <- ctx
	if s.touchGate != nil {
		<-s.touchGate
	}
	return nil
}
func r5APILoginFixture(t *testing.T) *r5APILoginStore {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &r5APILoginStore{user: &models.User{ID: uuid.New(), TenantID: uuid.New(), Email: "login@example.test", PasswordHash: string(hash), Role: models.RoleUser, IsActive: true}, touches: make(chan context.Context, 4)}
}
func r5APILoginRequest() *http.Request {
	return httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"login@example.test","password":"test-password-123"}`))
}
func r5APILoginGate(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return ch, release
}
func r5APILoginWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("login lifecycle barrier not reached")
	}
}
func r5APILoginTouch(t *testing.T, st *r5APILoginStore) context.Context {
	t.Helper()
	select {
	case ctx := <-st.touches:
		return ctx
	case <-time.After(3 * time.Second):
		t.Fatal("login touch did not start")
		return nil
	}
}
func r5APILoginStopped() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
func r5APILoginOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("login owner falsely drained")
	default:
	}
}

// The first Header access is the original setRefreshCookie site. Blocking it
// proves Touch is launched there, not delayed until after handler completion.
type r5APILoginCookieGate struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (w *r5APILoginCookieGate) Header() http.Header {
	w.once.Do(func() { close(w.entered); <-w.gate })
	return w.ResponseRecorder.Header()
}

func TestR5APIBackgroundShutdownLoginOriginalAsyncPositionAndBackground(t *testing.T) {
	st := r5APILoginFixture(t)
	var touchRelease func()
	st.touchGate, touchRelease = r5APILoginGate(t)
	h := NewAuthHandler(st, "test-only", uuid.New(), false, nil, false, zerolog.Nop())
	cookieGate, cookieRelease := r5APILoginGate(t)
	w := &r5APILoginCookieGate{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), gate: cookieGate}
	returned := make(chan struct{})
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() { defer close(returned); h.Login(w, r5APILoginRequest().WithContext(requestCtx)) }()
	r5APILoginWait(t, w.entered)
	touchCtx := r5APILoginTouch(t, st)
	if _, ok := touchCtx.Deadline(); ok || touchCtx.Err() != nil {
		t.Fatalf("login no longer uses original Background: %v", touchCtx.Err())
	}
	cookieRelease()
	r5APILoginWait(t, returned)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := h.StopContext(r5APILoginStopped()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	r5APILoginOpen(t, h.Done())
	touchRelease()
	r5APILoginWait(t, h.Done())
}

func TestR5APIBackgroundShutdownPendingLoginNotDropped(t *testing.T) {
	for _, stage := range []string{"lookup", "token"} {
		t.Run(stage, func(t *testing.T) {
			st := r5APILoginFixture(t)
			entered := make(chan struct{})
			gate, release := r5APILoginGate(t)
			if stage == "lookup" {
				st.lookupEnter, st.lookupGate = entered, gate
			} else {
				st.tokenEnter, st.tokenGate = entered, gate
			}
			h := NewAuthHandler(st, "test-only", uuid.New(), false, nil, false, zerolog.Nop())
			w := httptest.NewRecorder()
			returned := make(chan struct{})
			go func() { defer close(returned); h.Login(w, r5APILoginRequest()) }()
			r5APILoginWait(t, entered)
			h.CloseAdmission()
			r5APILoginOpen(t, h.Done())
			release()
			r5APILoginWait(t, returned)
			r5APILoginTouch(t, st)
			if w.Code != 200 || st.tokens.Load() != 1 {
				t.Fatalf("pending login dropped status=%d tokens=%d", w.Code, st.tokens.Load())
			}
			r5APILoginWait(t, h.Done())
		})
	}
}

func TestR5APIBackgroundShutdownNewLoginRejectedBeforeTokenEffects(t *testing.T) {
	st := r5APILoginFixture(t)
	h := NewAuthHandler(st, "test-only", uuid.New(), false, nil, false, zerolog.Nop())
	h.CloseAdmission()
	w := httptest.NewRecorder()
	h.Login(w, r5APILoginRequest())
	if w.Code != 503 || st.lookups.Load() != 0 || st.tokens.Load() != 0 {
		t.Fatalf("closed admission status=%d lookups=%d tokens=%d", w.Code, st.lookups.Load(), st.tokens.Load())
	}
	select {
	case <-st.touches:
		t.Fatal("closed login launched touch")
	default:
	}
	r5APILoginWait(t, h.Done())
}

func TestR5APIBackgroundShutdownLoginReleasedLeaseIsNotSuccess(t *testing.T) {
	st := r5APILoginFixture(t)
	o := lifecycle.New()
	ctx, release, err := o.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	h := NewAuthHandler(st, "test-only", uuid.New(), false, nil, false, zerolog.Nop(), o)
	w := httptest.NewRecorder()
	h.Login(w, r5APILoginRequest().WithContext(ctx))
	if w.Code != 503 || st.lookups.Load() != 0 || st.tokens.Load() != 0 {
		t.Fatalf("released lease accepted status=%d lookups=%d tokens=%d", w.Code, st.lookups.Load(), st.tokens.Load())
	}
	if err := h.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
