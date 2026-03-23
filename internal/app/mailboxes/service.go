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
	"tabmail/internal/hooks"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

type storeRepo interface {
	app.AuditStore
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	ListMailboxes(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.Mailbox, int, error)
	GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error)
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListMailboxObjectKeys(ctx context.Context, mailboxID uuid.UUID) ([]string, error)
	DeleteMailbox(ctx context.Context, id uuid.UUID) error
	CountMessagesByObjectKey(ctx context.Context, objectKey string) (int, error)
}

type Service struct {
	store       storeRepo
	obj         store.ObjectStore
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

func NewService(s storeRepo, obj store.ObjectStore, dispatcher *hooks.Dispatcher, namingMode policy.NamingMode, stripPlus bool, tokenSecret string, logger zerolog.Logger) *Service {
	return &Service{store: s, obj: obj, dispatcher: dispatcher, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret, logger: logger.With().Str("service", "mailboxes").Logger()}
}

func (s *Service) List(ctx context.Context, tenant *models.Tenant, isAdmin bool, pg models.Page) ([]*models.Mailbox, int, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, 0, err
	}
	items, total, err := s.store.ListMailboxes(ctx, tenant.ID, pg)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	return items, total, nil
}

func (s *Service) Create(ctx context.Context, tenant *models.Tenant, isAdmin bool, req CreateRequest, actor string) (*models.Mailbox, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
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
	if !isAdmin && zone.TenantID != tenant.ID {
		return nil, app.Forbidden("domain belongs to another tenant")
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
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(tenant.ID), Actor: actor, Action: "mailbox.create", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "access_mode": mb.AccessMode, "retention_hours_override": mb.RetentionHoursOverride, "expires_at": mb.ExpiresAt})})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "mailbox.created", TenantID: tenant.ID.String(), Mailbox: mb.FullAddress, OccurredAt: time.Now().UTC(), Metadata: map[string]any{"mailbox_id": mb.ID.String(), "access_mode": mb.AccessMode}})
	}
	return mb, nil
}

func (s *Service) Delete(ctx context.Context, tenant *models.Tenant, isAdmin bool, id uuid.UUID, actor string) error {
	mb, err := s.store.GetMailbox(ctx, id)
	if err != nil {
		return app.Internal(err)
	}
	if mb == nil {
		return app.NotFound("mailbox not found")
	}
	if !isAdmin && (tenant == nil || mb.TenantID != tenant.ID) {
		return app.Forbidden("not your mailbox")
	}
	keys, err := s.store.ListMailboxObjectKeys(ctx, mb.ID)
	if err != nil {
		return app.Internal(err)
	}
	if err := s.store.DeleteMailbox(ctx, id); err != nil {
		return app.Internal(err)
	}
	for _, key := range uniqueStrings(keys) {
		refs, err := s.store.CountMessagesByObjectKey(ctx, key)
		if err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("count object references during mailbox delete")
			continue
		}
		if refs == 0 {
			if err := s.obj.Delete(ctx, key); err != nil {
				s.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during mailbox delete")
			}
		}
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{TenantID: app.UUIDPtr(mb.TenantID), Actor: actor, Action: "mailbox.delete", ResourceType: "mailbox", ResourceID: app.UUIDPtr(mb.ID), Details: app.MustJSON(map[string]any{"address": mb.FullAddress, "deleted_objects": len(keys)})})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "mailbox.deleted", TenantID: mb.TenantID.String(), Mailbox: mb.FullAddress, OccurredAt: time.Now().UTC(), Metadata: map[string]any{"mailbox_id": mb.ID.String(), "deleted_objects": len(keys)}})
	}
	return nil
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
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if mb.AccessMode != models.AccessToken || mb.PasswordHash == nil {
		return nil, app.Forbidden("mailbox does not support token authentication")
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, app.Forbidden("mailbox expired")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*mb.PasswordHash), []byte(password)); err != nil {
		return nil, app.Forbidden("invalid credentials")
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

func ensureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	if tenant == nil {
		return app.Forbidden("no tenant context")
	}
	if isAdmin && tenant.ID == uuid.Nil {
		return app.BadRequest("admin requests to tenant-scoped endpoints must include X-Tenant-ID")
	}
	return nil
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
