package middleware

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/api/lifecycle"
)

const apiKeyTouchInterval = time.Minute

type authStateKey struct{}

// AuthState is shared by one Router's handshake and stream revalidation only.
// In particular, another router/store/run cannot suppress this one's key touch.
type AuthState struct {
	owner     *lifecycle.Owner
	mu        sync.Mutex
	lastTouch map[uuid.UUID]time.Time
}

func NewAuthState(owner *lifecycle.Owner) *AuthState {
	if owner == nil {
		owner = lifecycle.New()
	}
	return &AuthState{owner: owner, lastTouch: make(map[uuid.UUID]time.Time)}
}
func (s *AuthState) CloseAdmission()                       { s.owner.CloseAdmission() }
func (s *AuthState) StopContext(ctx context.Context) error { return s.owner.StopContext(ctx) }
func (s *AuthState) Done() <-chan struct{}                 { return s.owner.Done() }

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func touchAPIKeyAsync(requestCtx context.Context, st authStore, state *AuthState, keyID uuid.UUID, remoteAddr string) error {
	if err := state.owner.CheckRequest(requestCtx); err != nil {
		return err
	}
	now := time.Now()
	state.mu.Lock()
	defer state.mu.Unlock()
	if last, ok := state.lastTouch[keyID]; ok && now.Sub(last) < apiKeyTouchInterval {
		return nil
	}
	ip := clientIP(remoteAddr)
	if err := state.owner.Go(requestCtx, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.TouchAPIKey(ctx, keyID, ip)
	}); err != nil {
		return err
	}
	// Never record a rejected ownership registration as a successful observation.
	state.lastTouch[keyID] = now
	return nil
}
