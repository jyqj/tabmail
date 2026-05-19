// Package settings provides a cached reader for system_settings with
// env-var seeding on first start and admin-editable runtime updates.
package settings

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

type settingsStore interface {
	GetSetting(ctx context.Context, key string) (*models.SystemSetting, error)
	UpsertSetting(ctx context.Context, key, value, description string) error
	ListSettings(ctx context.Context) ([]*models.SystemSetting, error)
}

// Manager provides cached access to system_settings.
// All services should read config through Manager instead of static env values.
type Manager struct {
	store       settingsStore
	logger      zerolog.Logger
	mu          sync.RWMutex
	cache       map[string]string
	cacheExpiry time.Time
	cacheTTL    time.Duration
}

func NewManager(st settingsStore, logger zerolog.Logger) *Manager {
	return &Manager{
		store:    st,
		logger:   logger.With().Str("component", "settings").Logger(),
		cache:    make(map[string]string),
		cacheTTL: 5 * time.Second,
	}
}

// Seed writes default values for keys that don't yet exist in the DB.
// Called once at startup. Existing DB values are never overwritten.
func (m *Manager) Seed(ctx context.Context, defaults map[string]SeedValue) {
	for key, sv := range defaults {
		existing, err := m.store.GetSetting(ctx, key)
		if err != nil {
			m.logger.Warn().Err(err).Str("key", key).Msg("seed: check existing")
			continue
		}
		if existing != nil {
			continue // DB already has this key → don't overwrite
		}
		if err := m.store.UpsertSetting(ctx, key, sv.Value, sv.Description); err != nil {
			m.logger.Warn().Err(err).Str("key", key).Msg("seed: upsert")
		} else {
			m.logger.Info().Str("key", key).Str("value", sv.Value).Msg("seeded setting from env")
		}
	}
}

type SeedValue struct {
	Value       string
	Description string
}

// Get returns a setting value, falling back to defaultVal if not found.
func (m *Manager) Get(ctx context.Context, key, defaultVal string) string {
	m.mu.RLock()
	if time.Now().Before(m.cacheExpiry) {
		if v, ok := m.cache[key]; ok {
			m.mu.RUnlock()
			return v
		}
		m.mu.RUnlock()
		return defaultVal
	}
	m.mu.RUnlock()

	m.refresh(ctx)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.cache[key]; ok {
		return v
	}
	return defaultVal
}

// GetInt is a convenience wrapper that parses the value as int.
func (m *Manager) GetInt(ctx context.Context, key string, defaultVal int) int {
	s := m.Get(ctx, key, strconv.Itoa(defaultVal))
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetBool is a convenience wrapper that parses the value as bool.
func (m *Manager) GetBool(ctx context.Context, key string, defaultVal bool) bool {
	s := m.Get(ctx, key, strconv.FormatBool(defaultVal))
	v, err := strconv.ParseBool(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// Set updates a setting in DB and invalidates cache.
func (m *Manager) Set(ctx context.Context, key, value, description string) error {
	if err := m.store.UpsertSetting(ctx, key, value, description); err != nil {
		return err
	}
	m.mu.Lock()
	m.cache[key] = value
	m.mu.Unlock()
	return nil
}

// All returns all settings from DB (bypasses cache).
func (m *Manager) All(ctx context.Context) ([]*models.SystemSetting, error) {
	return m.store.ListSettings(ctx)
}

// Invalidate forces the next Get to reload from DB.
func (m *Manager) Invalidate() {
	m.mu.Lock()
	m.cacheExpiry = time.Time{}
	m.mu.Unlock()
}

func (m *Manager) refresh(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// double-check after acquiring write lock
	if time.Now().Before(m.cacheExpiry) {
		return
	}
	items, err := m.store.ListSettings(ctx)
	if err != nil {
		m.logger.Warn().Err(err).Msg("refresh settings cache")
		// Keep stale cache rather than returning defaults
		m.cacheExpiry = time.Now().Add(1 * time.Second)
		return
	}
	newCache := make(map[string]string, len(items))
	for _, item := range items {
		newCache[item.Key] = item.Value
	}
	m.cache = newCache
	m.cacheExpiry = time.Now().Add(m.cacheTTL)
}
