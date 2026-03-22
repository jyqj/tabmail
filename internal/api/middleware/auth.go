package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type ctxKey int

const (
	ctxTenant ctxKey = iota
	ctxIsAdmin
	ctxAuthMode
	ctxScopes
)

const (
	AuthModePublic = "public"
	AuthModeAPIKey = "api_key"
	AuthModeAdmin  = "admin"
)

// TenantFromCtx returns the resolved tenant, or nil for unauthenticated requests.
func TenantFromCtx(ctx context.Context) *models.Tenant {
	if v, ok := ctx.Value(ctxTenant).(*models.Tenant); ok {
		return v
	}
	return nil
}

// IsAdmin returns true when the request was authenticated via X-Admin-Key.
func IsAdmin(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxIsAdmin).(bool); ok {
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

// Auth resolves the caller identity using a 3-layer model:
//
//  1. X-Admin-Key → super admin (bypass all limits)
//  2. X-API-Key   → tenant API key
//  3. no key      → public tenant
func Auth(st store.Store, adminKey string, publicTenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if key := r.Header.Get("X-Admin-Key"); key != "" {
				if key == adminKey {
					tenant := &models.Tenant{Name: "admin", IsSuper: true}
					if tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID")); tenantID != "" {
						id, err := uuid.Parse(tenantID)
						if err != nil {
							writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid X-Tenant-ID")
							return
						}
						resolved, err := st.GetTenant(ctx, id)
						if err != nil {
							writeError(w, http.StatusInternalServerError, "INTERNAL", "tenant lookup failed")
							return
						}
						if resolved == nil {
							writeError(w, http.StatusNotFound, "NOT_FOUND", "tenant not found")
							return
						}
						tenant = resolved
						tenant.IsSuper = true
					}
					ctx = context.WithValue(ctx, ctxIsAdmin, true)
					ctx = context.WithValue(ctx, ctxAuthMode, AuthModeAdmin)
					ctx = context.WithValue(ctx, ctxScopes, []string{"*"})
					ctx = context.WithValue(ctx, ctxTenant, tenant)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid admin key")
				return
			}

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

			// Public / unauthenticated path.
			pubID, _ := uuid.Parse(publicTenantID)
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

func RequireTenantKeyOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch AuthModeFromCtx(r.Context()) {
		case AuthModeAdmin, AuthModeAPIKey:
			next.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusForbidden, "FORBIDDEN", "tenant api key or admin key required")
		}
	})
}

func RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mode := AuthModeFromCtx(r.Context())
			if mode == AuthModeAdmin || mode == AuthModePublic {
				next.ServeHTTP(w, r)
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

func hasAnyScope(scopes []string, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		seen[s] = struct{}{}
		if s == "*" {
			return true
		}
	}
	for _, req := range required {
		if _, ok := seen[req]; ok {
			return true
		}
	}
	return false
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
