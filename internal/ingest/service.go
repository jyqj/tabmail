package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/classify"
	"tabmail/internal/config"
	"tabmail/internal/configcache"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/workqueue"
)

type serviceStore interface {
	GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int, ensureObject func(context.Context) error) (bool, error)
	CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error)
	CreateIngestJob(ctx context.Context, job *models.IngestJob, ensureObject func(context.Context) error) error
	ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error)
	MarkIngestJobDone(ctx context.Context, id uuid.UUID) error
	MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
	ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error)
}

type Envelope struct {
	Source     string          `json:"source"`
	RemoteIP   string          `json:"remote_ip,omitempty"`
	MailFrom   string          `json:"mail_from,omitempty"`
	Recipients []string        `json:"recipients"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type AcceptResult struct {
	Queued    bool
	Delivered int
}

// AcceptOption customizes an Accept call without changing its positional
// signature, so existing callers Accept(ctx, env, raw) keep compiling.
type AcceptOption func(*acceptCfg)

// acceptCfg is the materialized set of Accept customizations. Only resolved is
// populated today (SMTP session reuse of RCPT-phase resolution); future per-call
// overrides extend this struct without touching Accept's signature.
type acceptCfg struct {
	// resolved carries RCPT-phase resolver.Results keyed by sanitizeAddr(rcpt).
	// deliver consults it for each recipient: a Reusable entry short-circuits
	// the second Resolve; non-reusable entries (auto-create, or absent) fall
	// back to a fresh Resolve so auto-create semantics are preserved exactly.
	resolved map[string]*resolver.Result
}

// WithResolved hands a previously-resolved Result for addr to Accept so deliver
// can skip a redundant Resolve. Only Mailbox-bearing, non-Created Results are
// reused; auto-create and stale entries are ignored (see Result.Reusable).
func WithResolved(addr string, r *resolver.Result) AcceptOption {
	return func(c *acceptCfg) {
		if c.resolved == nil {
			c.resolved = map[string]*resolver.Result{}
		}
		c.resolved[sanitizeAddr(addr)] = r
	}
}

func applyAcceptOptions(opts []AcceptOption) acceptCfg {
	var cfg acceptCfg
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// RecipientStatus is the disposition of one recipient in a delivered envelope.
type RecipientStatus string

const (
	// RecipientDelivered means the message was stored for this recipient.
	RecipientDelivered RecipientStatus = "delivered"
	// RecipientRejected means a routing, policy, or quota rule dropped the
	// recipient. This is an expected, terminal outcome — not retried.
	RecipientRejected RecipientStatus = "rejected"
	// RecipientError means a store/config failure prevented delivery; the
	// envelope as a whole may still be retried by the durable worker.
	RecipientError RecipientStatus = "error"
)

// RecipientOutcome is the disposition of a single recipient after delivery. It
// makes the accept decision a returned value rather than a side effect buried in
// logs: Reason is a stable machine code for rejected/error outcomes (empty for
// delivered), and MessageID is set only when the message was stored.
type RecipientOutcome struct {
	Address   string
	Status    RecipientStatus
	Reason    string
	MessageID string
}

func rejectedOutcome(addr, reason string) RecipientOutcome {
	return RecipientOutcome{Address: addr, Status: RecipientRejected, Reason: reason}
}

func erroredOutcome(addr, reason string) RecipientOutcome {
	return RecipientOutcome{Address: addr, Status: RecipientError, Reason: reason}
}

// deliveredCount reports how many outcomes resulted in a stored message.
func deliveredCount(outcomes []RecipientOutcome) int {
	n := 0
	for _, o := range outcomes {
		if o.Status == RecipientDelivered {
			n++
		}
	}
	return n
}

type Service struct {
	store              serviceStore
	obj                store.ObjectStore
	objects            *rawobject.Store
	resolver           *resolver.Resolver
	hub                *realtime.Hub
	dispatcher         *hooks.Dispatcher
	defaultPolicy      models.SMTPPolicy
	fallbackRetentionH int
	rdb                *redis.Client
	durable            bool
	pollInterval       time.Duration
	batchSize          int
	maxRetries         int
	logger             zerolog.Logger
	// policyCache holds a single-key SMTP-policy entry (the empty string). A
	// 2s TTL bounds staleness even if an admin update races the invalidator.
	policyCache *configcache.ConfigCache[string, *models.SMTPPolicy]
	// worker drives the durable claim loop. It is nil in non-durable mode or
	// before Run/ensureWorker is called.
	worker *workqueue.Worker[*ingestJob]
}

func NewService(
	st serviceStore,
	obj store.ObjectStore,
	res *resolver.Resolver,
	hub *realtime.Hub,
	dispatcher *hooks.Dispatcher,
	defaultPolicy models.SMTPPolicy,
	fallbackRetentionH int,
	rdb *redis.Client,
	cfg config.Ingest,
	logger zerolog.Logger,
) *Service {
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	s := &Service{
		store:              st,
		obj:                obj,
		objects:            rawobject.NewStore(obj, st),
		resolver:           res,
		hub:                hub,
		dispatcher:         dispatcher,
		defaultPolicy:      defaultPolicy,
		fallbackRetentionH: fallbackRetentionH,
		rdb:                rdb,
		durable:            cfg.Durable,
		pollInterval:       pollInterval,
		batchSize:          batchSize,
		maxRetries:         maxRetries,
		logger:             logger.With().Str("component", "ingest").Logger(),
	}
	s.policyCache = configcache.New(2*time.Second, func(ctx context.Context, _ string) (*models.SMTPPolicy, error) {
		pol, err := st.GetSMTPPolicy(ctx)
		if err != nil {
			return nil, err
		}
		if pol == nil {
			cp := s.defaultPolicy
			pol = &cp
		}
		cp := *pol
		return &cp, nil
	})
	return s
}

func (s *Service) Durable() bool { return s != nil && s.durable }

func (s *Service) Accept(ctx context.Context, env Envelope, raw []byte, opts ...AcceptOption) (AcceptResult, error) {
	if len(env.Recipients) == 0 {
		return AcceptResult{}, nil
	}
	if s.durable {
		objKey, err := s.objects.Put(ctx, raw)
		if err != nil {
			return AcceptResult{}, err
		}
		job := &models.IngestJob{
			ID:            uuid.New(),
			Source:        strings.TrimSpace(env.Source),
			RemoteIP:      strings.TrimSpace(env.RemoteIP),
			MailFrom:      strings.TrimSpace(env.MailFrom),
			Recipients:    append([]string(nil), env.Recipients...),
			RawObjectKey:  objKey,
			Metadata:      env.Metadata,
			State:         "pending",
			NextAttemptAt: time.Now().UTC(),
		}
		if err := s.objects.StoreIngestJob(ctx, job, raw); err != nil {
			s.deleteRawObjectIfOrphaned(ctx, objKey, "create_ingest_job_failed")
			return AcceptResult{}, err
		}
		return AcceptResult{Queued: true}, nil
	}
	outcomes, err := s.deliverResolved(ctx, env, raw, applyAcceptOptions(opts).resolved)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{Delivered: deliveredCount(outcomes)}, nil
}

func (s *Service) Run(ctx context.Context) {
	if s == nil || !s.durable {
		return
	}
	s.ensureWorker().Run(ctx)
}

// ProcessBatch runs a single claim+process cycle. It is the synchronous entry
// point used by tests; production code uses Run.
func (s *Service) ProcessBatch(ctx context.Context) {
	if s == nil || !s.durable {
		return
	}
	s.ensureWorker().ProcessBatch(ctx)
}

// ensureWorker lazily builds the workqueue.Worker backing Run/ProcessBatch. It
// is idempotent.
func (s *Service) ensureWorker() *workqueue.Worker[*ingestJob] {
	if s.worker != nil {
		return s.worker
	}
	storeAdapter := newIngestStore(s.store)
	hooks := &ingestHooks{svc: s}
	policy := workqueue.ExponentialBackoff[*ingestJob]{
		Base:   time.Second,
		CapExp: 8,
		Jitter: func() time.Duration { return time.Duration(rand.IntN(1000)) * time.Millisecond },
		Max:    s.maxRetries,
	}
	s.worker = workqueue.NewWorker[*ingestJob](
		storeAdapter,
		s.processOne,
		policy,
		hooks,
		5*time.Minute,
		s.pollInterval,
		s.batchSize,
		s.logger,
	)
	return s.worker
}

// processOne is the workqueue.Handler for ingest jobs. It loads the raw
// object, runs deliver, and records the delivery count on the payload so
// ingestHooks.OnDone can release the raw object when nobody was delivered.
// A nil error marks the job done.
func (s *Service) processOne(ctx context.Context, job *workqueue.Job[*ingestJob]) error {
	if job == nil || job.Payload == nil || job.Payload.IngestJob == nil {
		return nil
	}
	m := job.Payload.IngestJob
	rc, err := s.obj.Get(ctx, m.RawObjectKey)
	if err != nil {
		return fmt.Errorf("get raw object: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read raw object: %w", err)
	}
	outcomes, err := s.deliverResolved(ctx, Envelope{
		Source:     m.Source,
		RemoteIP:   m.RemoteIP,
		MailFrom:   m.MailFrom,
		Recipients: append([]string(nil), m.Recipients...),
		Metadata:   m.Metadata,
	}, raw, nil)
	if err != nil {
		return err
	}
	job.Payload.delivered = deliveredCount(outcomes)
	if job.Payload.delivered == 0 {
		s.logger.Warn().Str("job_id", m.ID.String()).Msg("ingest job processed with zero deliveries")
	}
	return nil
}

// deliver is the zero-option entry kept for direct unit-test calls. It always
// re-resolves every recipient (no RCPT-phase reuse), matching pre-P5 behavior.
func (s *Service) deliver(ctx context.Context, env Envelope, raw []byte) ([]RecipientOutcome, error) {
	return s.deliverResolved(ctx, env, raw, nil)
}

// deliverResolved attempts to store the envelope for every recipient and returns
// one RecipientOutcome per recipient. The per-recipient drop reasons are part of
// the return value (not just logs), so the accept decision can be asserted
// through this interface. A non-nil error signals an envelope-level failure
// (policy load or raw persistence) that should be retried, distinct from
// per-recipient drops.
//
// resolved (may be nil) carries RCPT-phase Results keyed by sanitizeAddr(rcpt).
// For each recipient deliverResolved prefers the cached entry when it is
// Reusable (Mailbox present and not just Created), short-circuiting a redundant
// Resolve; otherwise it falls back to s.resolver.Resolve so auto-create and
// quota semantics are untouched.
func (s *Service) deliverResolved(ctx context.Context, env Envelope, raw []byte, resolved map[string]*resolver.Result) ([]RecipientOutcome, error) {
	if len(env.Recipients) == 0 {
		return nil, nil
	}
	envMime, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		s.logger.Warn().Err(err).Msg("parsing MIME envelope (storing raw only)")
	}

	now := time.Now()
	outcomes := make([]RecipientOutcome, 0, len(env.Recipients))
	tenantConfigs := map[uuid.UUID]*models.EffectiveConfig{}
	pol, err := s.currentPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("load smtp policy: %w", err)
	}

	subject := ""
	var headersJSON json.RawMessage
	if envMime != nil {
		subject = envMime.GetHeader("Subject")
		hm := make(map[string]string)
		for _, key := range []string{"From", "To", "Cc", "Date", "Message-Id", "Reply-To", "Content-Type"} {
			if v := envMime.GetHeader(key); v != "" {
				hm[key] = v
			}
		}
		headersJSON, _ = json.Marshal(hm)
	}

	objKey, err := s.objects.Put(ctx, raw)
	if err != nil {
		return nil, err
	}

	for _, rcpt := range env.Recipients {
		addr := sanitizeAddr(rcpt)
		// Reuse a RCPT-phase Result when it is safe (Mailbox present, not just
		// Created): this is the SMTP-session-reuse fast path. Auto-create
		// results (Mailbox nil) and freshly-Created results always fall through
		// to a fresh Resolve so the limiter/quota gates and concurrent-retention
		// semantics stay identical to the pre-reuse behavior.
		var result *resolver.Result
		if cached, ok := resolved[addr]; ok && cached.Reusable() {
			result = cached
		} else {
			result, err = s.resolver.Resolve(ctx, addr)
			if err != nil {
				metrics.MailboxRecipientRejected(addr)
				s.logger.Warn().Err(err).Str("rcpt", addr).Msg("resolve failed")
				outcomes = append(outcomes, erroredOutcome(addr, "resolve_error"))
				continue
			}
		}
		if result == nil || result.Mailbox == nil || result.Zone == nil {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Debug().Str("rcpt", addr).Msg("no matching zone/route, rejecting recipient")
			outcomes = append(outcomes, rejectedOutcome(addr, "no_route"))
			continue
		}
		if !result.Zone.CanReceiveMessage() {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Warn().Str("rcpt", addr).Msg("recipient zone is not verified")
			outcomes = append(outcomes, rejectedOutcome(addr, "zone_unverified"))
			continue
		}
		if result.Created {
			s.logger.Info().Str("address", addr).Msg("auto-created mailbox")
		}

		mb := result.Mailbox
		if !policy.ShouldStoreDomain(mb.ResolvedDomain, pol.DefaultStore, pol.StoreDomains, pol.DiscardDomains) {
			s.logger.Info().Str("mailbox", mb.FullAddress).Msg("message accepted but discarded by store policy")
			outcomes = append(outcomes, rejectedOutcome(addr, "store_policy_discard"))
			continue
		}
		cfg, ok := tenantConfigs[mb.TenantID]
		if !ok {
			cfg, err = s.store.EffectiveConfig(ctx, mb.TenantID)
			if err != nil || cfg == nil {
				s.logger.Warn().Err(err).Str("mailbox", mb.FullAddress).Msg("load tenant config")
				outcomes = append(outcomes, erroredOutcome(addr, "tenant_config"))
				continue
			}
			tenantConfigs[mb.TenantID] = cfg
		}
		if cfg.MaxMessageBytes > 0 && len(raw) > cfg.MaxMessageBytes {
			s.logger.Warn().
				Str("mailbox", mb.FullAddress).
				Int("limit", cfg.MaxMessageBytes).
				Int("size", len(raw)).
				Msg("tenant max message bytes exceeded")
			outcomes = append(outcomes, rejectedOutcome(addr, "max_message_bytes"))
			continue
		}

		mbRetention, routeRetention, tenantRetention := retentionOf(result, cfg)
		retH := resolveRetention(mbRetention, routeRetention, tenantRetention, s.fallbackRetentionH)
		msg := &models.Message{
			TenantID:     mb.TenantID,
			MailboxID:    mb.ID,
			ZoneID:       mb.ZoneID,
			Sender:       env.MailFrom,
			Recipients:   []string{addr},
			Subject:      subject,
			Size:         int64(len(raw)),
			RawObjectKey: objKey,
			HeadersJSON:  headersJSON,
			ExpiresAt:    now.Add(time.Duration(retH) * time.Hour),
		}
		// OTP signal extraction. Reuses the already-parsed envelope (no extra
		// decode). OTPCode/OTPConfidence stay zero-value when nothing is found,
		// so the omitempty JSON path and existing deliver tests are unaffected.
		if envMime != nil {
			otp := classify.OTPFromMessage(classify.Env{
				Subject:  subject,
				TextBody: envMime.Text,
				HTMLBody: envMime.HTML,
				From:     env.MailFrom,
			})
			if otp.Found {
				msg.OTPCode = otp.Code
				msg.OTPConfidence = otp.Confidence
			}
		}
		if ok, err := s.reserveTenantDaily(ctx, mb.TenantID, cfg.DailyQuota); err != nil {
			s.logger.Warn().Err(err).Str("tenant", mb.TenantID.String()).Msg("reserve tenant daily quota")
			outcomes = append(outcomes, erroredOutcome(addr, "quota_error"))
			continue
		} else if !ok {
			s.logger.Warn().
				Str("tenant", mb.TenantID.String()).
				Int("limit", cfg.DailyQuota).
				Msg("tenant daily quota exceeded")
			outcomes = append(outcomes, rejectedOutcome(addr, "tenant_daily_quota"))
			continue
		}
		ok, err := s.objects.StoreMessage(ctx, msg, raw, cfg.MaxMessagesPerMailbox)
		if err != nil {
			_ = s.releaseTenantDaily(ctx, mb.TenantID)
			metrics.SMTPDeliveryFailed(mb.TenantID.String(), mb.FullAddress)
			s.logger.Err(err).Str("mailbox", mb.FullAddress).Msg("storing message metadata")
			outcomes = append(outcomes, erroredOutcome(addr, "store_failed"))
			continue
		}
		if !ok {
			_ = s.releaseTenantDaily(ctx, mb.TenantID)
			s.logger.Warn().
				Str("mailbox", mb.FullAddress).
				Int("limit", cfg.MaxMessagesPerMailbox).
				Msg("mailbox message quota exceeded")
			outcomes = append(outcomes, rejectedOutcome(addr, "mailbox_quota"))
			continue
		}
		metrics.SMTPDeliverySucceeded(mb.TenantID.String(), mb.FullAddress)
		if s.hub != nil {
			s.hub.Publish(realtime.Event{
				Type:      realtime.EventMessage,
				Mailbox:   mb.FullAddress,
				MessageID: msg.ID.String(),
				Sender:    env.MailFrom,
				Subject:   subject,
				Size:      int64(len(raw)),
			})
		}
		if s.dispatcher != nil {
			s.dispatcher.Publish(hooks.Event{
				Type:       "message.received",
				Mailbox:    mb.FullAddress,
				MessageID:  msg.ID.String(),
				TenantID:   mb.TenantID.String(),
				Sender:     env.MailFrom,
				Recipients: []string{addr},
				Subject:    subject,
			})
		}
		outcomes = append(outcomes, RecipientOutcome{
			Address:   addr,
			Status:    RecipientDelivered,
			MessageID: msg.ID.String(),
		})
		s.logger.Info().
			Str("from", env.MailFrom).
			Str("to", addr).
			Str("subject", subject).
			Int64("size", int64(len(raw))).
			Msg("message delivered")
	}
	return outcomes, nil
}

func (s *Service) currentPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	return s.policyCache.Get(ctx, "")
}

// InvalidatePolicy drops the cached SMTP policy so the next read observes the
// latest committed policy. Called by the admin write path after UpsertSMTPPolicy.
func (s *Service) InvalidatePolicy() {
	s.policyCache.InvalidateAll()
}

// InvalidateSMTPPolicy satisfies the admin service's policyInvalidator seam.
// It forwards to InvalidatePolicy for naming parity with the admin surface.
func (s *Service) InvalidateSMTPPolicy() {
	s.InvalidatePolicy()
}

func (s *Service) CurrentPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	return s.currentPolicy(ctx)
}

func (s *Service) deleteRawObjectIfOrphaned(ctx context.Context, key, reason string) {
	if s == nil {
		return
	}
	switch out, err := s.objects.Release(ctx, key); out {
	case rawobject.CountFailed, rawobject.DeleteFailed:
		s.logger.Warn().Err(err).Str("key", key).Str("reason", reason).Msg("release orphan raw object")
	case rawobject.StillReferenced:
		s.logger.Debug().Str("key", key).Str("reason", reason).Msg("raw object still referenced")
	case rawobject.Deleted:
		s.logger.Info().Str("key", key).Str("reason", reason).Msg("deleted orphan raw object")
	}
}

func (s *Service) reserveTenantDaily(ctx context.Context, tenantID uuid.UUID, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	if s.rdb == nil {
		count, err := s.store.CountTenantMessagesSince(ctx, tenantID, time.Now().UTC().Truncate(24*time.Hour))
		if err != nil {
			return false, err
		}
		return count < limit, nil
	}
	key := fmt.Sprintf("smtp:quota:tenant:%s:%s", tenantID, time.Now().UTC().Format("20060102"))
	res, err := s.rdb.Eval(ctx, `
local current = redis.call("GET", KEYS[1])
if current and tonumber(current) >= tonumber(ARGV[1]) then
  return 0
end
local next = redis.call("INCR", KEYS[1])
if next == 1 then
  redis.call("EXPIRE", KEYS[1], tonumber(ARGV[2]))
end
if next > tonumber(ARGV[1]) then
  redis.call("DECR", KEYS[1])
  return 0
end
return 1
`, []string{key}, limit, int((25 * time.Hour).Seconds())).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (s *Service) releaseTenantDaily(ctx context.Context, tenantID uuid.UUID) error {
	if s.rdb == nil {
		return nil
	}
	key := fmt.Sprintf("smtp:quota:tenant:%s:%s", tenantID, time.Now().UTC().Format("20060102"))
	_, err := s.rdb.Eval(ctx, `
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
if tonumber(current) <= 1 then
  redis.call("DEL", KEYS[1])
  return 0
end
return redis.call("DECR", KEYS[1])
`, []string{key}).Result()
	return err
}

// resolveRetention picks the retention window for a message from mailbox →
// route → tenant → fallback, mirroring the EffectiveConfig precedence without
// re-querying the store. Each level is a *int: nil means "unset" (skip to the
// next level); a non-nil value is returned as-is. This matches the legacy
// behavior where a non-nil EffectiveConfig.RetentionHours was returned even
// when 0 (immediate expiry), so the tenant level deliberately uses != nil
// rather than > 0. The caller passes the tenant's cached RetentionHours (the
// same value held in tenantConfigs from deliver's per-tenant EffectiveConfig
// lookup), so a single delivery no longer triggers a second EffectiveConfig
// round-trip per recipient.
func resolveRetention(mailboxOverride, routeOverride, tenantRetention *int, fallback int) int {
	if mailboxOverride != nil {
		return *mailboxOverride
	}
	if routeOverride != nil {
		return *routeOverride
	}
	if tenantRetention != nil {
		return *tenantRetention
	}
	if fallback > 0 {
		return fallback
	}
	return 24
}

// retentionOf extracts the override/route/tenant retention for a recipient from
// the resolved Result and the cached tenant config, returning nil for unset
// levels so resolveRetention's != nil precedence matches the legacy
// EffectiveConfig behavior (a non-nil cfg returns RetentionHours even when 0).
func retentionOf(res *resolver.Result, cfg *models.EffectiveConfig) (mailbox, route, tenant *int) {
	if res.Mailbox != nil && res.Mailbox.RetentionHoursOverride != nil {
		v := *res.Mailbox.RetentionHoursOverride
		mailbox = &v
	}
	if res.Route != nil && res.Route.RetentionHoursOverride != nil {
		v := *res.Route.RetentionHoursOverride
		route = &v
	}
	if cfg != nil {
		v := cfg.RetentionHours
		tenant = &v
	}
	return mailbox, route, tenant
}

func sanitizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	return strings.ToLower(addr)
}
