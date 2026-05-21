package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"tabmail/internal/authn"
	"tabmail/internal/models"
)

type authStore interface {
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, []string, error)
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
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
)

const (
	AuthModePublic = "public"
	AuthModeAPIKey = "api_key"
	AuthModeAdmin  = "admin"
	AuthModeUser   = "user"
)

// TenantFromCtx returns the resolved tenant, or nil for unauthenticated requests.
func TenantFromCtx(ctx context.Context) *models.Tenant {
	if v, ok := ctx.Value(ctxTenant).(*models.Tenant); ok {
		return v
	}
	return nil
}

// IsAdmin returns true when the request was authenticated as an admin.
func IsAdmin(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxIsAdmin).(bool); ok {
		return v
	}
	return false
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
	if mode == AuthModeAdmin || mode == AuthModeUser {
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

// PermissionFromCtx returns the resolved effective permission, or nil.
func PermissionFromCtx(ctx context.Context) *models.EffectivePermission {
	if v, ok := ctx.Value(ctxPermission).(*models.EffectivePermission); ok {
		return v
	}
	return nil
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
			if mode != AuthModeAdmin && mode != AuthModeUser {
				next.ServeHTTP(w, r)
				return
			}

			// Admin gets unlimited
			if mode == AuthModeAdmin {
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
				// Fall back to defaults on error, don't block the request
				perm = &models.EffectivePermission{
					CanSend:           false,
					DailyReceiveQuota: 500,
					MaxMailboxes:      10,
					MaxDomains:        1,
					CanCreateAPIKeys:  true,
				}
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

					isAdmin := user.Role == models.RoleAdmin
					ctx = context.WithValue(ctx, ctxUser, user)
					ctx = context.WithValue(ctx, ctxTenant, tenant)
					ctx = context.WithValue(ctx, ctxIsAdmin, isAdmin)
					ctx = context.WithValue(ctx, ctxBypassLimits, isAdmin)
					ctx = context.WithValue(ctx, ctxScopes, []string{"*"})
					if isAdmin {
						// Admin can impersonate tenant via X-Tenant-ID header
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
				tenant, scopes, err := st.ResolveAPIKey(ctx, key)
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

// RequireAdmin rejects non-admin requests with 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsAdmin(r.Context()) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin access required")
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
		case AuthModeAdmin, AuthModeUser:
			next.ServeHTTP(w, r)
		case AuthModePublic:
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		default:
			writeError(w, http.StatusForbidden, "FORBIDDEN", "jwt user authentication required")
		}
	})
}

// RequireTenantKeyOrAdmin allows admin, user (JWT), or API key authenticated requests.
func RequireTenantKeyOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch AuthModeFromCtx(r.Context()) {
		case AuthModeAdmin, AuthModeAPIKey, AuthModeUser:
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
			if mode == AuthModeAdmin || mode == AuthModeUser {
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
