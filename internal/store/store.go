package store

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// Store is the primary persistence interface for TabMail.
type Store interface {
	// --- Plans -----------------------------------------------------------
	CreatePlan(ctx context.Context, p *models.Plan) error
	GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error)
	ListPlans(ctx context.Context) ([]*models.Plan, error)
	UpdatePlan(ctx context.Context, p *models.Plan) error
	DeletePlan(ctx context.Context, id uuid.UUID) error

	// --- Tenants ---------------------------------------------------------
	CreateTenant(ctx context.Context, t *models.Tenant) error
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	ListTenants(ctx context.Context) ([]*models.Tenant, error)
	DeleteTenant(ctx context.Context, id uuid.UUID) error

	// --- Tenant overrides ------------------------------------------------
	UpsertOverride(ctx context.Context, o *models.TenantOverride) error
	GetOverride(ctx context.Context, tenantID uuid.UUID) (*models.TenantOverride, error)

	// Merges plan + override into a flat config.
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)

	// --- Tenant API keys -------------------------------------------------
	CreateAPIKey(ctx context.Context, k *models.TenantAPIKey) error
	ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error)
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
	// Looks up tenant by raw API key (hashes internally).
	ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, []string, error)
	TouchAPIKey(ctx context.Context, id uuid.UUID) error

	// --- Domain zones ----------------------------------------------------
	CreateZone(ctx context.Context, z *models.DomainZone) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	UpdateZone(ctx context.Context, z *models.DomainZone) error
	DeleteZone(ctx context.Context, id uuid.UUID) error
	CountZones(ctx context.Context, tenantID uuid.UUID) (int, error)
	CountAllZones(ctx context.Context) (int, error)

	// --- Domain routes ---------------------------------------------------
	CreateRoute(ctx context.Context, r *models.DomainRoute) error
	GetRoute(ctx context.Context, id uuid.UUID) (*models.DomainRoute, error)
	ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error)
	DeleteRoute(ctx context.Context, id uuid.UUID) error
	// Returns all routes whose zone domain matches the given address domain.
	FindMatchingRoutes(ctx context.Context, domain string) ([]*models.DomainRoute, error)

	// --- SMTP Policy -----------------------------------------------------
	GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error)
	UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error

	// --- Mailboxes -------------------------------------------------------
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMailboxes(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	ListMailboxesByZone(ctx context.Context, zoneID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CountAllMailboxes(ctx context.Context) (int, error)
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)

	// --- Messages --------------------------------------------------------
	CreateMessage(ctx context.Context, m *models.Message) error
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	CountMessages(ctx context.Context, mailboxID uuid.UUID) (int, error)
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
	CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error)
	CountAllMessages(ctx context.Context) (int, error)

	// Batch-delete expired messages, returns the number deleted.
	DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error)
	// Returns raw_object_key values for messages deleted by retention.
	ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error)

	// --- Audit -----------------------------------------------------------
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
	ListAuditEntries(ctx context.Context, limit int) ([]*models.AuditEntry, error)
	ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error)

	// --- Monitor Events --------------------------------------------------
	CreateMonitorEvent(ctx context.Context, e *models.MonitorEvent) error
	ListMonitorEvents(ctx context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error)

	// --- Lifecycle -------------------------------------------------------
	Close() error
}

// ObjectStore handles raw .eml blob storage.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}
