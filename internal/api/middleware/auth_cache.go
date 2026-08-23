package middleware

import (
	"context"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/configcache"
	"tabmail/internal/models"
)

const authCacheTTL = 10 * time.Second

// CachedAuthStore wraps a store with short-lived TTL caches for the hot
// authentication path (user/tenant/permission/config lookups).
type CachedAuthStore struct {
	inner authStore
	users *configcache.ConfigCache[uuid.UUID, *models.User]
	tenants *configcache.ConfigCache[uuid.UUID, *models.Tenant]
	perms   *configcache.ConfigCache[uuid.UUID, *models.EffectivePermission]
	configs *configcache.ConfigCache[uuid.UUID, *models.EffectiveConfig]
}

func NewCachedAuthStore(st authStore, configLoader func(context.Context, uuid.UUID) (*models.EffectiveConfig, error)) *CachedAuthStore {
	c := &CachedAuthStore{inner: st}
	c.users = configcache.New(authCacheTTL, st.GetUser, configcache.WithNilCache[uuid.UUID, *models.User](true))
	c.tenants = configcache.New(authCacheTTL, st.GetTenant, configcache.WithNilCache[uuid.UUID, *models.Tenant](true))
	c.perms = configcache.New(authCacheTTL, st.EffectivePermission, configcache.WithNilCache[uuid.UUID, *models.EffectivePermission](true))
	if configLoader != nil {
		c.configs = configcache.New(authCacheTTL, configLoader, configcache.WithNilCache[uuid.UUID, *models.EffectiveConfig](true))
	}
	return c
}

func (c *CachedAuthStore) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return c.users.Get(ctx, id)
}

func (c *CachedAuthStore) GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	return c.tenants.Get(ctx, id)
}

func (c *CachedAuthStore) EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	return c.perms.Get(ctx, userID)
}

func (c *CachedAuthStore) EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error) {
	if c.configs == nil {
		return nil, nil
	}
	return c.configs.Get(ctx, tenantID)
}

func (c *CachedAuthStore) ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error) {
	return c.inner.ResolveAPIKey(ctx, rawKey)
}

func (c *CachedAuthStore) TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error {
	return c.inner.TouchAPIKey(ctx, id, ip)
}
