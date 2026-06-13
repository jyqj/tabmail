package store

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

var (
	ErrOutboundDailyQuotaExceeded = errors.New("outbound daily quota exceeded")
	ErrSendAsDailyQuotaExceeded   = errors.New("send-as daily quota exceeded")
)

// OutboundQuotaReservation describes quota limits that must be checked in the
// same critical section as outbound job creation.
type OutboundQuotaReservation struct {
	UserDaily   *OutboundUserDailyQuota
	SendAsDaily *OutboundSendAsDailyQuota
}

func (q OutboundQuotaReservation) HasLimits() bool {
	return (q.UserDaily != nil && q.UserDaily.Limit > 0) ||
		(q.SendAsDaily != nil && q.SendAsDaily.Limit > 0)
}

type OutboundUserDailyQuota struct {
	UserID *uuid.UUID
	Since  time.Time
	Limit  int
}

type OutboundSendAsDailyQuota struct {
	PrincipalType string
	PrincipalID   uuid.UUID
	IdentityID    uuid.UUID
	Since         time.Time
	Limit         int
}

// UserStore persists users, refresh tokens, and admin invitations.
type UserStore interface {
	// --- Users -----------------------------------------------------------
	CreateUser(ctx context.Context, u *models.User) error
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	ListUsers(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.User, int, error)
	UpdateUser(ctx context.Context, u *models.User) error
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
	TouchUserLogin(ctx context.Context, id uuid.UUID) error

	// --- Refresh tokens --------------------------------------------------
	CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error
	RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	DeleteExpiredRefreshTokens(ctx context.Context) error

	// --- Admin invitations -----------------------------------------------
	CreateAdminInvitation(ctx context.Context, inv *models.AdminInvitation) error
	GetAdminInvitationByCode(ctx context.Context, code string) (*models.AdminInvitation, error)
	MarkInvitationAccepted(ctx context.Context, id uuid.UUID) error
}

// PlanStore persists subscription plans.
type PlanStore interface {
	CreatePlan(ctx context.Context, p *models.Plan) error
	GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error)
	ListPlans(ctx context.Context) ([]*models.Plan, error)
	UpdatePlan(ctx context.Context, p *models.Plan) error
	DeletePlan(ctx context.Context, id uuid.UUID) error
}

// TenantStore persists tenants, tenant overrides, and tenant API keys.
type TenantStore interface {
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
	GetAPIKey(ctx context.Context, id uuid.UUID) (*models.TenantAPIKey, error)
	ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]*models.TenantAPIKey, error)
	ListAPIKeysByOwner(ctx context.Context, tenantID uuid.UUID, ownerUserID uuid.UUID) ([]*models.TenantAPIKey, error)
	DeleteAPIKey(ctx context.Context, id uuid.UUID) error
	// Looks up tenant by raw API key (hashes internally).
	// Returns the tenant, the API key UUID, scopes, allowed zone IDs, owner user ID, and any error.
	ResolveAPIKey(ctx context.Context, rawKey string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error)
	TouchAPIKey(ctx context.Context, id uuid.UUID, ip string) error
}

// ZoneStore persists domain zones and domain routes.
type ZoneStore interface {
	// --- Domain zones ----------------------------------------------------
	CreateZone(ctx context.Context, z *models.DomainZone) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	ListAllZones(ctx context.Context) ([]*models.DomainZone, error)
	ListPublicZones(ctx context.Context) ([]*models.DomainZone, error)
	ListZonesByVisibilities(ctx context.Context, visibilities []models.ResourceVisibility) ([]*models.DomainZone, error)
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
	// If tenantID is non-nil, only routes belonging to that tenant are returned.
	FindMatchingRoutes(ctx context.Context, domain string, tenantID *uuid.UUID) ([]*models.DomainRoute, error)
}

// MailboxStore persists mailboxes.
type MailboxStore interface {
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMailboxes(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	ListMailboxesByZone(ctx context.Context, zoneID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	ListMailboxesByZones(ctx context.Context, tenantID uuid.UUID, zoneIDs []uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CountAllMailboxes(ctx context.Context) (int, error)
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	ListZoneObjectKeys(ctx context.Context, zoneID uuid.UUID) ([]string, error)
}

// MessageStore persists messages and message retention cleanup.
type MessageStore interface {
	CreateMessage(ctx context.Context, m *models.Message) error
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
	ListMessages(ctx context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error)
	MarkSeen(ctx context.Context, id uuid.UUID) error
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	PurgeMailbox(ctx context.Context, mailboxID uuid.UUID) error
	CountMessages(ctx context.Context, mailboxID uuid.UUID) (int, error)
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
	CountRawObjectReferences(ctx context.Context, objectKey string) (int, error)
	CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error)
	CountAllMessages(ctx context.Context) (int, error)

	// Batch-delete expired messages, returns the number deleted.
	DeleteExpiredMessages(ctx context.Context, before time.Time, limit int) (int, error)
	// Returns raw_object_key values for messages deleted by retention.
	ListExpiredObjectKeys(ctx context.Context, before time.Time, limit int) ([]string, error)
	// Atomically deletes expired messages and returns affected object keys.
	DeleteExpiredMessagesReturningKeys(ctx context.Context, before time.Time, limit int) (int, []string, error)
}

// TenantScoped is a read view bound to a single tenant. Every lookup is
// filtered to that tenant; a row belonging to another tenant is reported
// as not found. Obtain via Store.ForTenant.
type TenantScoped interface {
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	GetMessage(ctx context.Context, id uuid.UUID) (*models.Message, error)
}

// ScopedStore hands out tenant-scoped read views. Prefer ForTenant over the
// unscoped point-lookups whenever tenant context is available; the unscoped
// MailboxStore/MessageStore getters are for genuinely tenant-less paths
// (SMTP-time resolution, super-admin global access).
type ScopedStore interface {
	ForTenant(tenantID uuid.UUID) TenantScoped
}

// PolicyStore persists the SMTP policy.
type PolicyStore interface {
	GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error)
	UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error
}

// AuditStore persists audit entries and monitor events.
type AuditStore interface {
	// --- Audit -----------------------------------------------------------
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
	ListAuditEntries(ctx context.Context, limit int) ([]*models.AuditEntry, error)
	ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error)

	// --- Monitor Events --------------------------------------------------
	CreateMonitorEvent(ctx context.Context, e *models.MonitorEvent) error
	ListMonitorEvents(ctx context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error)
}

// OutboxStore persists outbox events, webhook deliveries, and webhook endpoints.
type OutboxStore interface {
	// --- Outbox / Webhook deliveries ------------------------------------
	CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error
	ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error)
	MarkOutboxEventDone(ctx context.Context, id uuid.UUID) error
	MarkOutboxEventRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error
	CreateWebhookDeliveries(ctx context.Context, event *models.OutboxEvent, urls []string) error
	ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error)
	MarkWebhookDeliveryDone(ctx context.Context, id uuid.UUID) error
	MarkWebhookDeliveryRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
	ListDeadWebhookDeliveries(ctx context.Context, limit int) ([]models.DeadLetter, error)
	CountDeadWebhookDeliveries(ctx context.Context) (int, error)
	ListWebhookDeliveries(ctx context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error)
	CountWebhookDeliveriesByState(ctx context.Context, states ...string) (int, error)

	// --- Webhook endpoints (tenant-level) ---------------------------------
	CreateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	ListWebhookEndpoints(ctx context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error)
	GetWebhookEndpoint(ctx context.Context, id uuid.UUID) (*models.WebhookEndpoint, error)
	UpdateWebhookEndpoint(ctx context.Context, ep *models.WebhookEndpoint) error
	DeleteWebhookEndpoint(ctx context.Context, id uuid.UUID) error
}

// SuppressionStore persists the suppression list.
type SuppressionStore interface {
	AddSuppression(ctx context.Context, e *models.SuppressionEntry) error
	IsSuppressed(ctx context.Context, tenantID uuid.UUID, address string) (bool, error)
	ListSuppressions(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.SuppressionEntry, int, error)
	DeleteSuppression(ctx context.Context, tenantID uuid.UUID, id uuid.UUID) error
}

// IngestStore persists ingest jobs.
type IngestStore interface {
	CreateIngestJob(ctx context.Context, job *models.IngestJob) error
	ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error)
	MarkIngestJobDone(ctx context.Context, id uuid.UUID) error
	MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
	ListIngestJobs(ctx context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error)
	CountIngestJobsByState(ctx context.Context, states ...string) (int, error)

	// Purge completed/dead ingest jobs older than the given time.
	// Returns count of purged jobs and their object keys for orphan cleanup checks.
	PurgeOldIngestJobs(ctx context.Context, before time.Time, limit int) (int, []string, error)
}

// PermissionStore persists permission profiles and user permission overrides.
type PermissionStore interface {
	// --- Permission profiles -----------------------------------------------
	CreatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error)
	GetPermissionProfileByName(ctx context.Context, name string) (*models.PermissionProfile, error)
	ListPermissionProfiles(ctx context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error)
	UpdatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	DeletePermissionProfile(ctx context.Context, id uuid.UUID, tenantID *uuid.UUID) error

	// --- User permission overrides -----------------------------------------
	UpsertUserPermissionOverride(ctx context.Context, o *models.UserPermissionOverride) error
	DeleteUserPermissionOverride(ctx context.Context, userID uuid.UUID) error
	EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error)
}

// OutboundStore persists outbound jobs, outbound attempts, and send identities.
type OutboundStore interface {
	// --- Outbound jobs -----------------------------------------------------
	CreateOutboundJob(ctx context.Context, job *models.OutboundJob) error
	CreateOutboundJobWithQuota(ctx context.Context, job *models.OutboundJob, quota OutboundQuotaReservation) error
	GetOutboundJob(ctx context.Context, id uuid.UUID) (*models.OutboundJob, error)
	ListOutboundJobs(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error)
	ClaimOutboundJobs(ctx context.Context, now time.Time, limit int) ([]*models.OutboundJob, error)
	MarkOutboundJobSent(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, smtpCode int, smtpResponse, messageID string) error
	MarkOutboundJobRetry(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, nextAttemptAt time.Time) error
	MarkOutboundJobFailed(ctx context.Context, id uuid.UUID, deliveryToken *uuid.UUID, lastError string, dead bool) error
	CountOutboundSince(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, since time.Time) (int, error)
	CountOutboundByIdentitySince(ctx context.Context, tenantID uuid.UUID, principalType string, principalID uuid.UUID, identityID uuid.UUID, since time.Time) (int, error)
	RequeueOutboundJob(ctx context.Context, id uuid.UUID) error

	// --- Outbound jobs (user-scoped) ----------------------------------------
	ListOutboundJobsByUser(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error)
	ListOutboundJobsByAPIKey(ctx context.Context, tenantID uuid.UUID, apiKeyID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error)

	// --- Outbound attempts ------------------------------------------------
	CreateOutboundAttempt(ctx context.Context, a *models.OutboundAttempt) error
	ListOutboundAttempts(ctx context.Context, jobID uuid.UUID) ([]*models.OutboundAttempt, error)

	// --- Send identities -------------------------------------------------------
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	GetSendIdentity(ctx context.Context, id uuid.UUID) (*models.SendIdentity, error)
	ListSendIdentities(ctx context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error)
	ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error)
	FindSendIdentityForAddress(ctx context.Context, tenantID uuid.UUID, address string) (*models.SendIdentity, error)
	UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error
	DeleteSendIdentity(ctx context.Context, id uuid.UUID) error
}

// SettingsStore persists system settings.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (*models.SystemSetting, error)
	UpsertSetting(ctx context.Context, key, value, description string) error
	ListSettings(ctx context.Context) ([]*models.SystemSetting, error)
}

// LifecycleStore manages store lifecycle.
type LifecycleStore interface {
	Close() error
}

// Store is the primary persistence interface for TabMail.
type Store interface {
	UserStore
	PlanStore
	TenantStore
	ZoneStore
	MailboxStore
	MessageStore
	ScopedStore
	PolicyStore
	AuditStore
	OutboxStore
	SuppressionStore
	IngestStore
	PermissionStore
	OutboundStore
	SettingsStore
	LifecycleStore
}

// ObjectStore handles raw .eml blob storage.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}
