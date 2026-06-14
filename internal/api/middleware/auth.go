package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type authStore interface {
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error)
	TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error)
}

type ctxKey int

const (
	ctxTenant ctxKey = iota
	ctxIsAdmin
	ctxBypassLimits
	ctxAuthMode
	ctxScopes
	ctxUser
	ctxPermission
	ctxAPIKeyID
	ctxOwnerUserID
)

const (
	AuthModePublic     = "public"
	AuthModeAPIKey     = "api_key"
	AuthModeSuperAdmin = "super_admin"
	AuthModeAdmin      = "admin"
	AuthModeUser       = "user"
)

// TenantFromCtx returns the resolved tenant, or nil for unauthenticated requests.
func TenantFromCtx(ctx context.Context) *models.Tenant {
	if v, ok := ctx.Value(ctxTenant).(*models.Tenant); ok {
		return v
	}
	return nil
}

// IsSuperAdmin returns true when the request was authenticated as a super_admin.
func IsSuperAdmin(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxIsAdmin).(bool); ok {
		return v
	}
	return false
}

// IsAdmin returns true when the request was authenticated as admin or super_admin.
func IsAdmin(ctx context.Context) bool {
	mode := AuthModeFromCtx(ctx)
	return mode == AuthModeSuperAdmin || mode == AuthModeAdmin
}

// BypassLimits returns true when the request should bypass tenant/public limits.
func BypassLimits(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxBypassLimits).(bool); ok {
		return v
	}
	return false
}

func AuthModeFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxAuthMode).(string); ok {
		return v
	}
	return ""
}

func APIScopesFromCtx(ctx context.Context) []string {
	if v, ok := ctx.Value(ctxScopes).([]string); ok {
		return v
	}
	return nil
}

func HasScope(ctx context.Context, required ...string) bool {
	mode := AuthModeFromCtx(ctx)
	if mode == AuthModeSuperAdmin || mode == AuthModeAdmin || mode == AuthModeUser {
		return true
	}
	return hasAnyScope(APIScopesFromCtx(ctx), required...)
}

// UserFromCtx returns the authenticated user, or nil.
func UserFromCtx(ctx context.Context) *models.User {
	if v, ok := ctx.Value(ctxUser).(*models.User); ok {
		return v
	}
	return nil
}

// APIKeyIDFromCtx returns the authenticated API key's UUID, or nil.
func APIKeyIDFromCtx(ctx context.Context) *uuid.UUID {
	if v, ok := ctx.Value(ctxAPIKeyID).(*uuid.UUID); ok {
		return v
	}
	return nil
}

// PermissionFromCtx returns the resolved effective permission, or nil.
func PermissionFromCtx(ctx context.Context) *models.EffectivePermission {
	if v, ok := ctx.Value(ctxPermission).(*models.EffectivePermission); ok {
		return v
	}
	return nil
}

// OwnerUserIDFromCtx returns the API key owner's user ID, or nil.
// This is set only for API key authentication when the key has an active owner.
func OwnerUserIDFromCtx(ctx context.Context) *uuid.UUID {
	if v, ok := ctx.Value(ctxOwnerUserID).(*uuid.UUID); ok {
		return v
	}
	return nil
}

// ActorFromContext builds an authz.Actor from the request context. It lives in
// middleware (rather than authz) so the authz package has no dependency on this
// package — that keeps testutil.FakeStore, which implements store.Store (and
// thus references authz scope types), free of an import cycle through authz.
//
// API key identity is checked first so that an API key with an owner is
// correctly identified as PrincipalAPIKey (not PrincipalUser). The owner's
// user ID is stored in OwnerUserID for fallback grant checks.
func ActorFromContext(ctx context.Context) authz.Actor {
	actor := authz.Actor{}

	keyID := APIKeyIDFromCtx(ctx)
	user := UserFromCtx(ctx)

	if keyID != nil {
		// API key is the primary identity — even when the key has an owner.
		actor.Type = authz.PrincipalAPIKey
		actor.ID = *keyID
		if ownerID := OwnerUserIDFromCtx(ctx); ownerID != nil {
			actor.OwnerUserID = ownerID
		} else {
			// Only ownerless integration keys are tenant-wide. User-owned API keys
			// keep the API-key principal for audit/grants, but inherit ownership via
			// OwnerUserID rather than becoming broad tenant credentials.
			actor.TenantWide = true
		}
	} else if user != nil {
		actor.Type = authz.PrincipalUser
		actor.ID = user.ID
		actor.TenantID = user.TenantID
		actor.Role = user.Role
	}

	if tenant := TenantFromCtx(ctx); tenant != nil {
		actor.TenantID = tenant.ID
	}

	actor.IsSuperAdmin = IsSuperAdmin(ctx)
	actor.IsAdmin = IsAdmin(ctx)
	actor.Permission = PermissionFromCtx(ctx)

	return actor
}

type permStore interface {
	EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error)
}

// PermissionLoader loads effective permissions for JWT users and injects into context.
// For admin users, it grants unlimited permissions.
// For API key and public access, permissions are not loaded (nil in context).
func PermissionLoader(st permStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			mode := AuthModeFromCtx(ctx)

			// Only load permissions for JWT users
			if mode != AuthModeSuperAdmin && mode != AuthModeAdmin && mode != AuthModeUser {
				next.ServeHTTP(w, r)
				return
			}

			// super_admin / admin gets unlimited
			if mode == AuthModeSuperAdmin || mode == AuthModeAdmin {
				ctx = context.WithValue(ctx, ctxPermission, &models.EffectivePermission{
					CanSend:           true,
					DailySendQuota:    0, // unlimited
					DailyReceiveQuota: 0,
					MaxMailboxes:      0,
					MaxDomains:        0,
					AllowedZoneIDs:    nil, // all
					CanCreateDomains:  true,
					CanCreateRoutes:   true,
					CanCreateAPIKeys:  true,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			user := UserFromCtx(ctx)
			if user == nil {
				next.ServeHTTP(w, r)
				return
			}

			perm, err := st.EffectivePermission(ctx, user.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to load permissions")
				return
			}
			ctx = context.WithValue(ctx, ctxPermission, perm)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Auth resolves the caller identity using a 3-layer model:
//
//  1. Authorization: Bearer <JWT>  → logged-in user (admin or regular user)
//  2. X-API-Key                    → tenant API key
//  3. no credentials               → public tenant
func Auth(st authStore, jwtSecret string, publicTenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Layer 1: JWT Bearer token (logged-in user)
			if bearer := extractBearer(r); bearer != "" {
				// Try JWT access token first
				claims, err := authn.VerifyAccessToken(jwtSecret, bearer)
				if err == nil {
					user, err := st.GetUser(ctx, claims.UserID)
					if err != nil {
						writeError(w, http.StatusInternalServerError, "INTERNAL", "user lookup failed")
						return
					}
					if user == nil || !user.IsActive {
						writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found or inactive")
						return
					}
					tenant, err := st.GetTenant(ctx, user.TenantID)
					if err != nil || tenant == nil {
						writeError(w, http.StatusInternalServerError, "INTERNAL", "tenant lookup failed")
						return
					}

					isSuperAdmin := user.Role == models.RoleSuperAdmin
					isAdmin := user.Role == models.RoleAdmin
					ctx = context.WithValue(ctx, ctxUser, user)
					ctx = context.WithValue(ctx, ctxTenant, tenant)
					ctx = context.WithValue(ctx, ctxIsAdmin, isSuperAdmin) // ctxIsAdmin only true for super_admin
					ctx = context.WithValue(ctx, ctxBypassLimits, isSuperAdmin)
					ctx = context.WithValue(ctx, ctxScopes, []string{"*"})
					if isSuperAdmin {
						// Super admin can impersonate tenant via X-Tenant-ID header
						if tenantIDStr := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tenantIDStr != "" {
							tid, parseErr := uuid.Parse(tenantIDStr)
							if parseErr != nil {
								writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid X-Tenant-ID")
								return
							}
							resolved, lookupErr := st.GetTenant(ctx, tid)
							if lookupErr != nil {
								writeError(w, http.StatusInternalServerError, "INTERNAL", "tenant lookup failed")
								return
							}
							if resolved == nil {
								writeError(w, http.StatusNotFound, "NOT_FOUND", "tenant not found")
								return
							}
							ctx = context.WithValue(ctx, ctxTenant, resolved)
							ctx = context.WithValue(ctx, ctxBypassLimits, false)
						}
						ctx = context.WithValue(ctx, ctxAuthMode, AuthModeSuperAdmin)
					} else if isAdmin {
						ctx = context.WithValue(ctx, ctxAuthMode, AuthModeAdmin)
					} else {
						ctx = context.WithValue(ctx, ctxAuthMode, AuthModeUser)
					}
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Not a valid JWT — fall through (could be a mailbox bearer token,
				// which is handled at the handler/service layer, not middleware).
			}

			// Layer 2: X-API-Key → tenant API key
			if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
				tenant, keyID, scopes, allowedZoneIDs, ownerUserID, err := st.ResolveAPIKey(ctx, key)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "INTERNAL", "key lookup failed")
					return
				}
				if tenant == nil {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
					return
				}
				ctx = context.WithValue(ctx, ctxAuthMode, AuthModeAPIKey)
				ctx = context.WithValue(ctx, ctxScopes, scopes)
				ctx = context.WithValue(ctx, ctxTenant, tenant)
				if keyID != nil {
					ctx = context.WithValue(ctx, ctxAPIKeyID, keyID)
					go func() { _ = st.TouchAPIKey(context.Background(), *keyID, r.RemoteAddr) }()
				}

				// If the API key has an owner, verify the owner is still active
				// and load their effective permission for quota enforcement.
				// We store the owner user ID separately (ctxOwnerUserID) instead of
				// injecting a synthetic User into ctxUser, so that
				// ActorFromContext correctly identifies the caller as PrincipalAPIKey.
				if ownerUserID != nil {
					owner, ownerErr := st.GetUser(ctx, *ownerUserID)
					if ownerErr != nil {
						writeError(w, http.StatusInternalServerError, "INTERNAL", "api key owner lookup failed")
						return
					}
					if owner == nil || !owner.IsActive {
						writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "api key owner not found or inactive")
						return
					}
					ownerPerm, permErr := st.EffectivePermission(ctx, *ownerUserID)
					if permErr != nil || ownerPerm == nil {
						writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to load api key owner permissions")
						return
					}
					// Merge zone allowlists fail-closed: the key may only narrow the owner's
					// current permission, never expand it if the owner's profile changes later.
					if len(allowedZoneIDs) > 0 {
						ownerPerm.AllowedZoneIDs = intersectAllowedZones(ownerPerm.AllowedZoneIDs, allowedZoneIDs)
					}
					ctx = context.WithValue(ctx, ctxPermission, ownerPerm)
					ctx = context.WithValue(ctx, ctxOwnerUserID, ownerUserID)
				}
				// Build a restricted EffectivePermission for API keys with zone limits
				// but no owner user.
				if ownerUserID == nil && len(allowedZoneIDs) > 0 {
					ctx = context.WithValue(ctx, ctxPermission, &models.EffectivePermission{
						CanSend:          true,
						AllowedZoneIDs:   allowedZoneIDs,
						CanCreateDomains: true,
						CanCreateRoutes:  true,
					})
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Layer 3: Public / unauthenticated path.
			pubID, err := uuid.Parse(publicTenantID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "invalid public tenant id")
				return
			}
			pub, err := st.GetTenant(ctx, pubID)
			if err != nil || pub == nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "public tenant missing")
				return
			}
			ctx = context.WithValue(ctx, ctxAuthMode, AuthModePublic)
			ctx = context.WithValue(ctx, ctxTenant, pub)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func intersectAllowedZones(ownerZones, keyZones []uuid.UUID) []uuid.UUID {
	if len(keyZones) == 0 {
		return ownerZones
	}
	if len(ownerZones) == 0 {
		return append([]uuid.UUID(nil), keyZones...)
	}
	ownerSet := make(map[uuid.UUID]struct{}, len(ownerZones))
	for _, id := range ownerZones {
		ownerSet[id] = struct{}{}
	}
	intersection := make([]uuid.UUID, 0, len(keyZones))
	for _, id := range keyZones {
		if _, ok := ownerSet[id]; ok {
			intersection = append(intersection, id)
		}
	}
	if len(intersection) == 0 {
		return []uuid.UUID{uuid.Nil}
	}
	return intersection
}

// RequireAdmin accepts super_admin and admin. Rejects others with 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := AuthModeFromCtx(r.Context())
		if mode != AuthModeSuperAdmin && mode != AuthModeAdmin {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSuperAdmin accepts only super_admin. Rejects others with 403.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsSuperAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "super admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth allows only JWT-authenticated user/admin requests. Tenant API
// keys are integration credentials, not interactive user sessions.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch AuthModeFromCtx(r.Context()) {
		case AuthModeSuperAdmin, AuthModeAdmin, AuthModeUser:
			next.ServeHTTP(w, r)
		case AuthModePublic:
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		default:
			writeError(w, http.StatusForbidden, "FORBIDDEN", "jwt user authentication required")
		}
	})
}

// RequireTenantKeyOrAdmin allows super_admin, admin, user (JWT), or API key authenticated requests.
func RequireTenantKeyOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch AuthModeFromCtx(r.Context()) {
		case AuthModeSuperAdmin, AuthModeAdmin, AuthModeAPIKey, AuthModeUser:
			next.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusForbidden, "FORBIDDEN", "authentication required")
		}
	})
}

func RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mode := AuthModeFromCtx(r.Context())
			if mode == AuthModeSuperAdmin || mode == AuthModeAdmin || mode == AuthModeUser {
				next.ServeHTTP(w, r)
				return
			}
			if mode == AuthModePublic {
				if allReadScopes(required) {
					next.ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusForbidden, "FORBIDDEN", "authentication required for write operations")
				return
			}
			if hasAnyScope(APIScopesFromCtx(r.Context()), required...) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient api key scope")
		})
	}
}

func allReadScopes(scopes []string) bool {
	for _, s := range scopes {
		if !strings.HasSuffix(s, ":read") {
			return false
		}
	}
	return true
}

func hasAnyScope(scopes []string, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		scope := strings.ToLower(strings.TrimSpace(s))
		if scope == "" {
			continue
		}
		seen[scope] = struct{}{}
	}
	for _, req := range required {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(req))]; ok {
			return true
		}
	}
	return false
}

func extractBearer(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": msg,
		},
	})
}
