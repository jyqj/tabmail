package authz

import (
	"github.com/google/uuid"
	"tabmail/internal/models"
	"testing"
)

func TestOutboundContentKeyMetadataNeverWidensIdentity(t *testing.T) {
	tenant, user, id, zone := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, name := range []string{"read", "write", "normalized", "wrong-principal", "wrong-key", "wrong-tenant", "wrong-job-tenant", "wrong-owner", "ownerless-key", "ownerless-actor", "wrong-zone", "no-send-scope", "empty-scope", "nil-key", "nil-job"} {
		t.Run(name, func(t *testing.T) {
			a := Actor{Type: PrincipalAPIKey, ID: id, TenantID: tenant, OwnerUserID: &user}
			k := &models.TenantAPIKey{ID: id, TenantID: tenant, OwnerUserID: &user, Scopes: []string{"send:read"}}
			j := &models.OutboundJob{TenantID: tenant, ZoneID: zone}
			allowed := false
			switch name {
			case "read":
				allowed = true
			case "write":
				k.Scopes = []string{"send:write"}
				allowed = true
			case "normalized":
				k.Scopes = []string{" SEND:READ "}
				allowed = true
			case "wrong-principal":
				a.Type = PrincipalUser
			case "wrong-key":
				k.ID = uuid.New()
			case "wrong-tenant":
				k.TenantID = uuid.New()
			case "wrong-job-tenant":
				j.TenantID = uuid.New()
			case "wrong-owner":
				other := uuid.New()
				k.OwnerUserID = &other
			case "ownerless-key":
				k.OwnerUserID = nil
			case "ownerless-actor":
				a.OwnerUserID = nil
			case "wrong-zone":
				k.AllowedZoneIDs = []uuid.UUID{uuid.New()}
			case "no-send-scope":
				k.Scopes = []string{"messages:read"}
			case "empty-scope":
				k.Scopes = nil
			case "nil-key":
				k = nil
			case "nil-job":
				j = nil
			}
			if got := OutboundContentKeyMatches(a, k, j); got != allowed {
				t.Fatalf("got %v want %v", got, allowed)
			}
		})
	}
}
