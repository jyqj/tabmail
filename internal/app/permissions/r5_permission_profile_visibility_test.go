package permissions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type r5VisibilityPort struct {
	Store
	actor    authz.Actor
	selected *uuid.UUID
	called   bool
	items    []*models.PermissionProfile
	err      error
}

func (s *r5VisibilityPort) ListVisiblePermissionProfiles(_ context.Context, actor authz.Actor, selected *uuid.UUID) ([]*models.PermissionProfile, error) {
	s.called, s.actor, s.selected = true, actor, selected
	return s.items, s.err
}

func TestR5PermissionProfileVisibilityServicePort(t *testing.T) {
	tenant := uuid.New()
	v := int64(0)
	// Stale flags cannot choose a filter in the service. Current authority and
	// selected scope are passed to the fenced adapter, including promotions.
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), SessionVersion: &v}
	items := []*models.PermissionProfile{{ID: uuid.New(), TenantID: &tenant}}
	port := &r5VisibilityPort{Store: newStub(), items: items}
	out, err := New(port).ListProfiles(context.Background(), a, &tenant)
	if err != nil || !port.called || port.actor.ID != a.ID || port.actor.TenantID != a.TenantID || port.actor.SessionVersion != &v || port.selected != &tenant || len(out) != 1 || out[0] != items[0] {
		t.Fatalf("formal visibility port/actor/selection was bypassed: %v", err)
	}
	for _, err := range []error{authz.ErrForbidden("fresh authority denied"), app.Conflict("changing"), errors.New("read failed")} {
		port.err = err
		out, got := New(port).ListProfiles(context.Background(), a, &tenant)
		if got == nil || out != nil {
			t.Fatal("failed visibility read returned profiles")
		}
		want := app.KindInternal
		if authz.IsAuthzError(err) {
			want = app.KindForbidden
		}
		if e, ok := app.As(err); ok {
			want = e.Kind
		}
		assertKind(t, got, want, "")
	}
}

func TestR5PermissionProfileVisibilityServiceFailClosed(t *testing.T) {
	tenant := uuid.New()
	for _, a := range []authz.Actor{
		{},
		{Type: authz.PrincipalUser, IsSuperAdmin: true},
		{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, IsSuperAdmin: true},
		{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, OwnerUserID: ptr(uuid.New()), IsAdmin: true},
	} {
		port := &r5VisibilityPort{Store: newStub()}
		_, err := New(port).ListProfiles(context.Background(), a, &tenant)
		assertKind(t, err, app.KindForbidden, "")
		if port.called {
			t.Fatal("noninteractive principal reached visibility port")
		}
	}
	// Embedding only the legacy Store deliberately hides optional capabilities.
	legacy := struct{ Store }{newStub()}
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant, IsSuperAdmin: true}
	_, err := New(legacy).ListProfiles(context.Background(), a, &tenant)
	assertKind(t, err, app.KindInternal, "")
}
