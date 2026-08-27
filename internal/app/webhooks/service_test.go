package webhooksapp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type webhookTestStore struct {
	endpoints map[uuid.UUID]*models.WebhookEndpoint
	deleted   []uuid.UUID
	listErr   error
}

func newWebhookTestStore() *webhookTestStore {
	return &webhookTestStore{endpoints: map[uuid.UUID]*models.WebhookEndpoint{}}
}

func (s *webhookTestStore) CreateWebhookEndpoint(_ context.Context, ep *models.WebhookEndpoint) error {
	cp := *ep
	s.endpoints[cp.ID] = &cp
	return nil
}

func (s *webhookTestStore) ListWebhookEndpoints(_ context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []*models.WebhookEndpoint
	for _, ep := range s.endpoints {
		if ep.TenantID == tenantID {
			cp := *ep
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *webhookTestStore) GetWebhookEndpoint(_ context.Context, id uuid.UUID) (*models.WebhookEndpoint, error) {
	ep, found := s.endpoints[id]
	if !found {
		return nil, nil
	}
	cp := *ep
	return &cp, nil
}

func (s *webhookTestStore) UpdateWebhookEndpoint(_ context.Context, ep *models.WebhookEndpoint) error {
	if _, found := s.endpoints[ep.ID]; !found {
		return errors.New("endpoint not found")
	}
	cp := *ep
	s.endpoints[cp.ID] = &cp
	return nil
}

func (s *webhookTestStore) DeleteWebhookEndpoint(_ context.Context, id uuid.UUID) error {
	delete(s.endpoints, id)
	s.deleted = append(s.deleted, id)
	return nil
}

func newTestService(store *webhookTestStore) *Service {
	return NewService(store, zerolog.Nop())
}

func requireKind(t *testing.T, err error, want app.ErrorKind) {
	t.Helper()
	appErr, found := app.As(err)
	if !found {
		t.Fatalf("expected an application error, got %v", err)
	}
	if appErr.Kind != want {
		t.Fatalf("expected kind %q, got %q (%s)", want, appErr.Kind, appErr.Message)
	}
}

func adminCaller(tenantID uuid.UUID) Caller {
	return Caller{Actor: authz.Actor{Type: authz.PrincipalUser, IsAdmin: true, TenantID: tenantID}}
}

func keyCaller(tenantID uuid.UUID, scopes ...string) Caller {
	return Caller{
		Actor: authz.Actor{Type: authz.PrincipalAPIKey, TenantID: tenantID, TenantWide: true},
		HasScope: func(scope string) bool {
			for _, s := range scopes {
				if s == scope {
					return true
				}
			}
			return false
		},
	}
}

func seedEndpoint(store *webhookTestStore, tenantID uuid.UUID) *models.WebhookEndpoint {
	ep := &models.WebhookEndpoint{
		ID: uuid.New(), TenantID: tenantID,
		URL: "https://example.com/hook", IsActive: true,
	}
	store.endpoints[ep.ID] = ep
	return ep
}

func TestCreateNormalizesTheEndpoint(t *testing.T) {
	store := newWebhookTestStore()
	tenantID := uuid.New()
	svc := newTestService(store)

	ep, err := svc.Create(context.Background(), adminCaller(tenantID), CreateRequest{
		URL:        "  https://example.com/hook  ",
		Secret:     "  s3cret  ",
		EventTypes: []string{" message.received ", "MESSAGE.DELETED", "message.received"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ep.URL != "https://example.com/hook" {
		t.Fatalf("url not trimmed: %q", ep.URL)
	}
	if len(ep.EventTypes) != 2 || ep.EventTypes[0] != "message.received" || ep.EventTypes[1] != "message.deleted" {
		t.Fatalf("event types not sanitized/deduplicated: %v", ep.EventTypes)
	}
	if ep.Secret == nil || *ep.Secret != "s3cret" {
		t.Fatalf("secret not trimmed: %v", ep.Secret)
	}
	if ep.TenantID != tenantID || !ep.IsActive {
		t.Fatalf("unexpected endpoint: %#v", ep)
	}
}

// A tenant must not be able to aim TabMail at an address only the server can
// reach, which is the SSRF the URL rules exist to prevent.
func TestCreateRejectsUnreachableOrPrivateDestinations(t *testing.T) {
	tenantID := uuid.New()
	for _, rawURL := range []string{
		"",
		"http://example.com/hook",
		"https://user:pass@example.com/hook",
		"https://localhost/hook",
		"https://app.localhost/hook",
		"https://127.0.0.1/hook",
		"https://10.0.0.1/hook",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/hook",
		"https:///missing-host",
		"https://example.com/ho\u0000ok",
	} {
		t.Run(rawURL, func(t *testing.T) {
			store := newWebhookTestStore()
			svc := newTestService(store)

			_, err := svc.Create(context.Background(), adminCaller(tenantID), CreateRequest{URL: rawURL})
			requireKind(t, err, app.KindBadRequest)
			if len(store.endpoints) != 0 {
				t.Fatal("expected nothing to be stored for a rejected url")
			}
		})
	}
}

func TestCreateRejectsMalformedEventTypes(t *testing.T) {
	store := newWebhookTestStore()
	svc := newTestService(store)

	_, err := svc.Create(context.Background(), adminCaller(uuid.New()), CreateRequest{
		URL:        "https://example.com/hook",
		EventTypes: []string{"not a valid type"},
	})
	requireKind(t, err, app.KindBadRequest)
}

func TestPlainUsersCannotReachWebhookEndpoints(t *testing.T) {
	svc := newTestService(newWebhookTestStore())
	caller := Caller{Actor: authz.Actor{Type: authz.PrincipalUser, TenantID: uuid.New()}}

	_, err := svc.List(context.Background(), caller)
	requireKind(t, err, app.KindForbidden)
}

func TestAPIKeysNeedTheMatchingScope(t *testing.T) {
	store := newWebhookTestStore()
	tenantID := uuid.New()
	seedEndpoint(store, tenantID)
	svc := newTestService(store)

	if _, err := svc.List(context.Background(), keyCaller(tenantID, "webhooks:read")); err != nil {
		t.Fatalf("read scope should list: %v", err)
	}
	_, err := svc.Create(context.Background(), keyCaller(tenantID, "webhooks:read"), CreateRequest{
		URL: "https://example.com/hook",
	})
	requireKind(t, err, app.KindForbidden)
}

func TestCallsWithoutATenantAreRefused(t *testing.T) {
	svc := newTestService(newWebhookTestStore())

	_, err := svc.List(context.Background(), adminCaller(uuid.Nil))
	requireKind(t, err, app.KindForbidden)
}

func TestListReturnsAnEmptySliceRatherThanNil(t *testing.T) {
	svc := newTestService(newWebhookTestStore())

	items, err := svc.List(context.Background(), adminCaller(uuid.New()))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if items == nil {
		t.Fatal("expected an empty slice so the response renders as []")
	}
}

func TestListReportsStoreFailuresAsInternal(t *testing.T) {
	store := newWebhookTestStore()
	store.listErr = errors.New("database is down")
	svc := newTestService(store)

	_, err := svc.List(context.Background(), adminCaller(uuid.New()))
	requireKind(t, err, app.KindInternal)
}

func TestUpdateAppliesOnlyTheSuppliedFields(t *testing.T) {
	store := newWebhookTestStore()
	tenantID := uuid.New()
	ep := seedEndpoint(store, tenantID)
	ep.EventTypes = []string{"message.received"}
	svc := newTestService(store)

	inactive := false
	updated, err := svc.Update(context.Background(), adminCaller(tenantID), ep.ID, UpdateRequest{IsActive: &inactive})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.IsActive {
		t.Fatal("expected the endpoint to be disabled")
	}
	if updated.URL != "https://example.com/hook" || len(updated.EventTypes) != 1 {
		t.Fatalf("expected untouched fields to survive, got %#v", updated)
	}
}

func TestUpdateRejectsANewPrivateURL(t *testing.T) {
	store := newWebhookTestStore()
	tenantID := uuid.New()
	ep := seedEndpoint(store, tenantID)
	svc := newTestService(store)

	private := "https://10.0.0.1/hook"
	_, err := svc.Update(context.Background(), adminCaller(tenantID), ep.ID, UpdateRequest{URL: &private})
	requireKind(t, err, app.KindBadRequest)
	if store.endpoints[ep.ID].URL != "https://example.com/hook" {
		t.Fatal("expected the stored url to be left alone")
	}
}

// Another tenant's endpoint must look missing rather than forbidden, so the
// route never confirms that an id exists somewhere else.
func TestOtherTenantsEndpointsLookMissing(t *testing.T) {
	store := newWebhookTestStore()
	foreign := seedEndpoint(store, uuid.New())
	svc := newTestService(store)

	_, err := svc.Update(context.Background(), adminCaller(uuid.New()), foreign.ID, UpdateRequest{})
	requireKind(t, err, app.KindNotFound)

	err = svc.Delete(context.Background(), adminCaller(uuid.New()), foreign.ID)
	requireKind(t, err, app.KindNotFound)
	if len(store.deleted) != 0 {
		t.Fatalf("expected no delete to reach the store, got %v", store.deleted)
	}
}

func TestDeleteRemovesTheTenantsEndpoint(t *testing.T) {
	store := newWebhookTestStore()
	tenantID := uuid.New()
	ep := seedEndpoint(store, tenantID)
	svc := newTestService(store)

	if err := svc.Delete(context.Background(), adminCaller(tenantID), ep.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != ep.ID {
		t.Fatalf("expected the endpoint to be deleted, got %v", store.deleted)
	}
}
