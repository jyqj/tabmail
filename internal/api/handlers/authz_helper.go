package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// newAuthorizer creates an Authorizer from the store.
func newAuthorizer(st store.Store) *authz.Authorizer {
	return authz.New(st)
}

// authorizeRequest is a convenience that extracts the actor from context,
// runs Authorize, and writes a 403 if denied.
func authorizeRequest(w http.ResponseWriter, r *http.Request, az *authz.Authorizer, action authz.Action, res authz.Resource) bool {
	actor := authz.ActorFromContext(r.Context())
	if err := az.Authorize(r.Context(), actor, action, res); err != nil {
		if authz.IsAuthzError(err) {
			errForbidden(w, err.Error())
		} else {
			errInternal(w)
		}
		return false
	}
	return true
}

// domainActorParams extracts common authorization parameters for domain handlers.
func domainActorParams(r *http.Request) (tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) {
	actor := authz.ActorFromContext(r.Context())
	tenant = actorTenant(r)
	isAdmin = actor.IsPlatformAdmin || actor.IsTenantAdmin
	if actor.Type == authz.PrincipalUser {
		id := actor.ID
		ownerUserID = &id
	} else if actor.Type == authz.PrincipalAPIKey && actor.OwnerUserID != nil {
		// API key with an active owner: use the owner's user ID for resource
		// ownership (e.g. zone/route creation auto-grants).
		ownerUserID = actor.OwnerUserID
	}
	tenantWide = actor.TenantWide || actor.IsPlatformAdmin || actor.IsTenantAdmin
	return
}

// mailboxActorParams extracts common authorization parameters for mailbox handlers.
func mailboxActorParams(r *http.Request) (tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) {
	return domainActorParams(r)
}

// actorTenant returns the tenant from middleware context (unchanged helper).
func actorTenant(r *http.Request) *models.Tenant {
	return middleware.TenantFromCtx(r.Context())
}
