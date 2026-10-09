package testutil

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// This adapter mirrors its existing in-memory permission defaults. PostgreSQL
// integration tests are authoritative for profile fences and usage-row rollback.
func (s *FakeStore) CreateAPIKeyAuthorized(ctx context.Context, issuer authz.APIKeyIssuer, key *models.TenantAPIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == nil {
		return authz.ErrForbidden("current interactive JWT issuer required")
	}
	if s.tenants[key.TenantID] == nil {
		return app.NotFound("tenant not found")
	}
	actor, valid := issuer.Refresh(s.users[issuer.Actor.ID], key.TenantID)
	if !valid {
		return authz.ErrForbidden("API key issuer no longer matches the authenticated session")
	}
	if !actor.IsTenantAdmin() {
		var err error
		actor.Permission, err = s.EffectivePermission(ctx, actor.ID)
		if err != nil {
			return err
		}
	}
	candidate := *key
	candidate.Scopes = append([]string(nil), key.Scopes...)
	candidate.AllowedZoneIDs = append([]uuid.UUID(nil), key.AllowedZoneIDs...)
	if err := authz.ConfigureIssuedAPIKey(actor, &candidate); err != nil {
		return err
	}
	for _, id := range candidate.AllowedZoneIDs {
		zone := s.zones[id]
		if zone == nil {
			return app.BadRequest("zone " + id.String() + " not found")
		}
		if zone.TenantID != key.TenantID {
			return authz.ErrForbidden("zone " + id.String() + " does not belong to tenant")
		}
	}
	if candidate.ID == uuid.Nil {
		candidate.ID = uuid.New()
	}
	candidate.CreatedAt = time.Now()
	details, err := json.Marshal(map[string]any{"label": candidate.Label, "key_prefix": candidate.KeyPrefix, "scopes": candidate.Scopes, "owner_user_id": candidate.OwnerUserID})
	if err != nil {
		return err
	}
	entry := &models.AuditEntry{ID: uuid.New(), TenantID: &candidate.TenantID, Actor: actor.AuditLabel(), Action: "api_key.create", ResourceType: "tenant_api_key", ResourceID: &candidate.ID, Details: details, CreatedAt: candidate.CreatedAt}
	s.apiKeys[candidate.ID] = &candidate
	s.audits = append(s.audits, entry)
	*key = candidate
	return nil
}
