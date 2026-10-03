package api_test

// These are pure shipping-Router ServeHTTP tests with fake storage, not TCP/PG
// evidence and not proof of the main caller's process shutdown wiring.
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
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type r5APIBackgroundRouterStore struct {
	*testutil.FakeStore
	tenant   *models.Tenant
	key      uuid.UUID
	touches  chan string
	gate     chan struct{}
	resolves atomic.Int32
}

func (s *r5APIBackgroundRouterStore) ResolveAPIKey(context.Context, string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	s.resolves.Add(1)
	return s.tenant, &s.key, []string{"*"}, nil, nil, nil
}
func (s *r5APIBackgroundRouterStore) TouchAPIKey(context.Context, uuid.UUID, string) error {
	s.touches <- "key"
	<-s.gate
	return nil
}
func (s *r5APIBackgroundRouterStore) TouchUserLogin(context.Context, uuid.UUID) error {
	s.touches <- "login"
	<-s.gate
	return nil
}
func r5APIBackgroundRouterFixture(t *testing.T) (*api.Router, *r5APIBackgroundRouterStore, func()) {
	t.Helper()
	st := &r5APIBackgroundRouterStore{FakeStore: testutil.NewFakeStore(), tenant: &models.Tenant{ID: uuid.New(), Name: "owner-test"}, key: uuid.New(), touches: make(chan string, 4), gate: make(chan struct{})}
	st.SeedTenant(st.tenant)
	var once sync.Once
	release := func() { once.Do(func() { close(st.gate) }) }
	t.Cleanup(release)
	router := api.NewRouter(api.RouterConfig{Store: st, PublicTenantID: st.tenant.ID.String(), JWTSecret: "test-only", RateLimiter: middleware.NewRateLimiter(nil, st, 0, nil), Logger: zerolog.Nop()})
	return router, st, release
}
func r5APIBackgroundRouterWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("router lifecycle barrier not reached")
	}
}
func r5APIBackgroundRouterTouch(t *testing.T, st *r5APIBackgroundRouterStore, want string) {
	t.Helper()
	select {
	case got := <-st.touches:
		if got != want {
			t.Fatalf("touch=%s want=%s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("router touch did not start")
	}
}
func r5APIBackgroundRouterIncomplete(t *testing.T, r *api.Router) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.StopContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	select {
	case <-r.Done():
		t.Fatal("router dropped live task")
	default:
	}
}

func TestR5APIBackgroundShutdownRouterAPIKeyRealWiring(t *testing.T) {
	r, st, release := r5APIBackgroundRouterFixture(t)
	var _ http.Handler = r
	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req.Header.Set("X-API-Key", "test-only")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("key request status=%d body=%s", w.Code, w.Body.String())
	}
	r5APIBackgroundRouterTouch(t, st, "key")
	r5APIBackgroundRouterIncomplete(t, r)
	release()
	r5APIBackgroundRouterWait(t, r.Done())
}

func TestR5APIBackgroundShutdownRouterLoginRealWiring(t *testing.T) {
	r, st, release := r5APIBackgroundRouterFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{ID: uuid.New(), TenantID: st.tenant.ID, Email: "login@example.test", PasswordHash: string(hash), Role: models.RoleUser, IsActive: true}
	if err := st.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"login@example.test","password":"test-password-123"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	r5APIBackgroundRouterTouch(t, st, "login")
	r5APIBackgroundRouterIncomplete(t, r)
	release()
	r5APIBackgroundRouterWait(t, r.Done())
}

func TestR5APIBackgroundShutdownRouterAdmissionCoversHealthAndAuth(t *testing.T) {
	r, st, _ := r5APIBackgroundRouterFixture(t)
	r.CloseAdmission()
	for _, path := range []string{"/health", "/ready", "/api/v1/auth/me", "/api/v1/auth/login"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-API-Key", "test-only")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 503 {
			t.Fatalf("path=%s status=%d", path, w.Code)
		}
	}
	if st.resolves.Load() != 0 {
		t.Fatal("closed request reached auth store")
	}
	r5APIBackgroundRouterWait(t, r.Done())
}

func TestR5APIBackgroundShutdownRouterInstancesDoNotShareThrottle(t *testing.T) {
	a, sa, releaseA := r5APIBackgroundRouterFixture(t)
	b, sb, releaseB := r5APIBackgroundRouterFixture(t)
	sb.key = sa.key
	for _, r := range []*api.Router{a, b} {
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		req.Header.Set("X-API-Key", "test-only")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	r5APIBackgroundRouterTouch(t, sa, "key")
	r5APIBackgroundRouterTouch(t, sb, "key")
	r5APIBackgroundRouterIncomplete(t, a)
	r5APIBackgroundRouterIncomplete(t, b)
	releaseA()
	r5APIBackgroundRouterWait(t, a.Done())
	select {
	case <-b.Done():
		t.Fatal("one router released another router's owner")
	default:
	}
	releaseB()
	r5APIBackgroundRouterWait(t, b.Done())
}

// Flush is the shipping SSE handler's real active-body barrier. No polling
// interval or scheduler sleep is used to simulate a long-lived stream.
type r5APIBackgroundStreamWriter struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (w *r5APIBackgroundStreamWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.once.Do(func() { close(w.flushed); <-w.gate })
}

func TestR5APIBackgroundShutdownRouterOwnsActualSSEBody(t *testing.T) {
	r, st, _ := r5APIBackgroundRouterFixture(t)
	user := &models.User{ID: uuid.New(), TenantID: st.tenant.ID, Email: "stream@example.test", Role: models.RoleSuperAdmin, IsActive: true}
	if err := st.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	token, err := authn.IssueAccessToken("test-only", user)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req := httptest.NewRequest("GET", "/api/v1/admin/monitor/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	w := &r5APIBackgroundStreamWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{}), gate: gate}
	returned := make(chan struct{})
	go func() { defer close(returned); r.ServeHTTP(w, req) }()
	r5APIBackgroundRouterWait(t, w.flushed)
	r5APIBackgroundRouterIncomplete(t, r)
	select {
	case <-returned:
		t.Fatal("SSE body returned before flush gate")
	default:
	}
	cancel()
	release()
	r5APIBackgroundRouterWait(t, returned)
	r5APIBackgroundRouterWait(t, r.Done())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "resync") {
		t.Fatalf("shipping SSE failed status=%d body=%s", w.Code, w.Body.String())
	}
}
