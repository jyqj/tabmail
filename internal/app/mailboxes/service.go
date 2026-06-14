package mailboxapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/store"
)

type storeRepo interface {
	app.AuditStore
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListZonesScoped(ctx context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	ListMailboxesScoped(ctx context.Context, scope authz.ZoneListFilter, pg models.Page) ([]*models.Mailbox, int, error)
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ForTenant(tenantID uuid.UUID) store.TenantScoped
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
}

type Service struct {
	store       storeRepo
	az          *authz.Authorizer
	obj         store.ObjectStore
	objects     *rawobject.Store
	dispatcher  *hooks.Dispatcher
	namingMode  policy.NamingMode
	stripPlus   bool
	tokenSecret string
	logger      zerolog.Logger
}

type CreateRequest struct {
	Address                string
	Password               string
	AccessMode             models.AccessMode
	RetentionHoursOverride *int
	ExpiresAt              *string
}

type TokenIssueResult struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

func NewService(s storeRepo, obj store.ObjectStore, objects *rawobject.Store, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, logger zerolog.Logger) *Service {
	return &Service{store: s, az: authz.New(s), obj: obj, objects: objects, dispatcher: dispatcher, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret, logger: logger.With().Str("service", "mailboxes").Logger()}
}

func (s *Service) List(ctx context.Context, actor authz.Actor, tenant *models.Tenant, pg models.Page) ([]*models.Mailbox, int, error) {
	isAdmin := actor.IsTenantAdmin()
	if err := app.EnsureTenantScope(tenant, isAdmin); err != nil {
		return nil, 0, err
	}
	// Tenant isolation, the zone allowlist, and (for regular users / user-owned
	// API keys) zone ownership are all enforced in SQL via the ZoneListFilter.
	// This replaces the previous accessibleZoneIDs ∩ ZoneAllowed in-memory
	// computation; the store's owner_user_id subquery reproduces it exactly.
	scope := authz.ZoneListScope(actor, tenant.ID)
	items, total, err := s.store.ListMailboxesScoped(ctx, scope, pg)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	return items, total, nil
}

func (s *Service) Create(ctx context.Context, actor authz.Actor, tenant *models.Tenant, req CreateRequest) (*models.Mailbox, error) {
	isAdmin := actor.IsTenantAdmin()
	if err := app.EnsureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	addr := strings.ToLower(strings.TrimSpace(req.Address))
	local, domain, err := policy.NormalizeAddressParts(addr, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid email address")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, s.namingMode, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid mailbox address")
	}
	zone, err := s.store.GetZoneByDomain(ctx, domain)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.BadRequest(fmt.Sprintf("domain %s is not registered", domain))
	}
	// Zone allowlist and zone ownership are enforced through the authz seam,
	// replacing the previous canManageZone + IsZoneAllowed pair.
	if err := s.authorize(ctx, actor, authz.ActionMailboxCreate, authz.ZoneResource(zone)); err != nil {
		return nil, err
	}
	// Per-user mailbox quota for non-admin principals.
	if actor.Permission != nil && !isAdmin && !models.IsUnlimited(actor.Permission.MaxMailboxes) {
		total, err := s.countUserMailboxes(ctx, actor, tenant)
		if err != nil {
			return nil, err
		}
		if total >= actor.Permission.MaxMailboxes {
			return nil, app.Forbidden("mailbox limit reached")
		}
	}
	cfg, err := s.store.EffectiveConfig(ctx, tenant.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	count, err := s.store.CountMailboxes(ctx, zone.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if !tenant.IsSuper && count >= cfg.MaxMailboxesPerDomain {
		return nil, app.Forbidden(fmt.Sprintf("mailbox limit reached (%d)", cfg.MaxMailboxesPerDomain))
	}
	am := req.AccessMode
	if am == "" {
		am = models.AccessPublic
	}
	if !am.Valid() {
		return nil, app.BadRequest("invalid access_mode")
	}
	if am == models.AccessToken && strings.TrimSpace(req.Password) == "" {
		return nil, app.BadRequest("password is required when access_mode=token")
	}
	if req.RetentionHoursOverride != nil && *req.RetentionHoursOverride <= 0 {
		return nil, app.BadRequest("retention_hours_override must be greater than 0")
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			return nil, app.BadRequest("invalid expires_at")
		}
		if !parsed.After(time.Now()) {
			return nil, app.BadRequest("expires_at must be in the future")
		}
		expiresAt = &parsed
	}
	mb := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: local, ResolvedDomain: domain, FullAddress: mailboxKey, AccessMode: am, RetentionHoursOverride: req.RetentionHoursOverride, ExpiresAt: expiresAt}
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, app.Internal(err)
		}
		s := string(hash)
		mb.PasswordHash = &s
	}
	if err := s.store.CreateMailbox(ctx, mb); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, app.Conflict("address already exists")
		}
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(tenant.ID), Actor: actor.AuditLabel(), Action: "mailbox.create", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "access_mode": mb.AccessMode, "retention_hours_override": mb.RetentionHoursOverride, "expires_at": mb.ExpiresAt})})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "mailbox.created", TenantID: tenant.ID.String(), Mailbox: mb.FullAddress, OccurredAt: time.Now().UTC(), Metadata: map[string]any{"mailbox_id": mb.ID.String(), "access_mode": mb.AccessMode}})
	}
	return mb, nil
}

func (s *Service) Delete(ctx context.Context, actor authz.Actor, tenant *models.Tenant, id uuid.UUID) error {
	var mb *models.Mailbox
	var err error
	if tenant != nil {
		mb, err = s.store.ForTenant(tenant.ID).GetMailbox(ctx, id)
	} else if actor.IsSuperAdmin {
		// Without a tenant context only super admins may use the global
		// lookup, matching the previous handler-derived isAdmin value.
		mb, err = s.store.GetMailbox(ctx, id)
	} else {
		return app.Forbidden("no tenant context")
	}
	if err != nil {
		return app.Internal(err)
	}
	if mb == nil {
		return app.NotFound("mailbox not found")
	}
	zone, err := s.store.GetZone(ctx, mb.ZoneID)
	if err != nil {
		return app.Internal(err)
	}
	if zone == nil {
		return app.Forbidden("not your mailbox")
	}
	// Zone allowlist and zone ownership are enforced through the authz seam;
	// ownership is compared against the zone owner, as before.
	if err := s.authorize(ctx, actor, authz.ActionMailboxDelete, authz.ZoneResource(zone)); err != nil {
		return err
	}
	keys, err := s.store.ListMailboxObjectKeys(ctx, mb.ID)
	if err != nil {
		return app.Internal(err)
	}
	if err := s.store.DeleteMailbox(ctx, id); err != nil {
		return app.Internal(err)
	}
	for _, key := range uniqueStrings(keys) {
		if _, err := s.objects.Release(ctx, key); err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("release raw object during mailbox delete")
		}
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor.AuditLabel(), Action: "mailbox.delete", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)})})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "mailbox.deleted", TenantID: mb.TenantID.String(), Mailbox: mb.FullAddress, OccurredAt: time.Now().UTC(), Metadata: map[string]any{"mailbox_id": mb.ID.String(), "deleted_objects": len(keys)}})
	}
	return nil
}

// countUserMailboxes counts the regular user's mailboxes for quota enforcement.
// It applies the same ZoneListFilter as List (owned zones ∩ allowlist) via the
// scoped store method, so quota accounting cannot diverge from list visibility.
func (s *Service) countUserMailboxes(ctx context.Context, actor authz.Actor, tenant *models.Tenant) (int, error) {
	scope := authz.ZoneListScope(actor, tenant.ID)
	_, total, err := s.store.ListMailboxesScoped(ctx, scope, models.Page{Page: 1, PerPage: 1})
	if err != nil {
		return 0, app.Internal(err)
	}
	return total, nil
}

// authorize runs the authz seam and converts AuthzError into an app-level
// Forbidden error so HTTP status and message stay stable.
func (s *Service) authorize(ctx context.Context, actor authz.Actor, action authz.Action, res authz.Resource) error {
	return app.FromAuthz(s.az.Authorize(ctx, actor, action, res))
}

func (s *Service) IssueToken(ctx context.Context, address, password, actor string) (*TokenIssueResult, error) {
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" || strings.TrimSpace(password) == "" {
		return nil, app.BadRequest("address and password are required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, s.namingMode, s.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}
	mb, err := s.store.GetMailboxByAddress(ctx, mailboxKey)
	if err != nil {
		return nil, app.Internal(err)
	}
	const tokenFailMsg = "invalid address or password"
	if mb == nil {
		return nil, app.Forbidden(tokenFailMsg)
	}
	if mb.AccessMode != models.AccessToken || mb.PasswordHash == nil {
		return nil, app.Forbidden(tokenFailMsg)
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, app.Forbidden(tokenFailMsg)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*mb.PasswordHash), []byte(password)); err != nil {
		return nil, app.Forbidden(tokenFailMsg)
	}
	ttl := 24 * time.Hour
	if mb.ExpiresAt != nil {
		remaining := time.Until(*mb.ExpiresAt)
		if remaining <= 0 {
			return nil, app.Forbidden("mailbox expired")
		}
		if remaining < ttl {
			ttl = remaining
		}
	}
	token, err := mailtoken.Issue(s.tokenSecret, mb.ID.String(), mb.FullAddress, ttl)
	if err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "mailbox.issue_token", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "ttl_seconds": int(ttl.Seconds())})})
	return &TokenIssueResult{Token: token, ExpiresIn: int(ttl.Seconds())}, nil
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
