package middleware

import (
	"context"
	"errors"
	"net/http"
)

// RevalidateRequest reruns identity and effective-permission loading against the
// uncached store. Long-lived requests must not reuse their handshake's actor.
// The private response sink prevents a failed check from writing HTTP error JSON
// into an already-established SSE response.
func RevalidateRequest(r *http.Request, st authStore, jwtSecret, publicTenant string) (*http.Request, error) {
	var current *http.Request
	original := r
	r = r.WithContext(cleanIdentityContext{r.Context()})
	sink := &identitySink{header: make(http.Header)}
	Auth(st, jwtSecret, publicTenant)(PermissionLoader(st)(http.HandlerFunc(func(_ http.ResponseWriter, fresh *http.Request) {
		current = fresh
	}))).ServeHTTP(sink, r)
	if current == nil {
		return nil, errors.New("stream authentication expired or unavailable")
	}
	before, after := ActorFromContext(original.Context()), ActorFromContext(current.Context())
	if before.Type != after.Type || before.ID != after.ID || before.TenantID != after.TenantID {
		return nil, errors.New("stream identity changed")
	}
	return current, nil
}

type identitySink struct{ header http.Header }

func (s *identitySink) Header() http.Header       { return s.header }
func (*identitySink) WriteHeader(int)             {}
func (*identitySink) Write(b []byte) (int, error) { return len(b), nil }

// Keep cancellation/tracing but never inherit a previous authentication decision.
type cleanIdentityContext struct{ context.Context }

func (c cleanIdentityContext) Value(key any) any {
	if _, ok := key.(ctxKey); ok {
		return nil
	}
	return c.Context.Value(key)
}
