package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
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
	policyMu           sync.RWMutex
	policyCache        *models.SMTPPolicy
	policyExpiresAt    time.Time
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
	return &Service{
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
}

func (s *Service) Durable() bool { return s != nil && s.durable }

func (s *Service) Accept(ctx context.Context, env Envelope, raw []byte) (AcceptResult, error) {
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
	outcomes, err := s.deliver(ctx, env, raw)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{Delivered: deliveredCount(outcomes)}, nil
}

func (s *Service) Run(ctx context.Context) {
	if s == nil || !s.durable {
		return
	}
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		if err := s.processBatch(ctx); err != nil {
			s.logger.Warn().Err(err).Msg("process ingest batch")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processBatch(ctx context.Context) error {
	jobs, err := s.store.ClaimIngestJobs(ctx, time.Now().UTC(), s.batchSize)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		result, err := s.processJob(ctx, job)
		if err != nil {
			dead := job.Attempts >= s.maxRetries
			backoff := retryBackoff(job.Attempts)
			nextAttempt := time.Now().UTC().Add(backoff)
			if markErr := s.store.MarkIngestJobRetry(ctx, job.ID, err.Error(), nextAttempt, dead); markErr != nil {
				return markErr
			}
			if dead {
				metrics.IngestJobDead()
				metrics.ObserveIngestJobLatency(time.Since(job.CreatedAt))
				s.deleteRawObjectIfOrphaned(ctx, job.RawObjectKey, "dead_ingest_job")
			} else {
				metrics.IngestJobRetried()
			}
			continue
		}
		if err := s.store.MarkIngestJobDone(ctx, job.ID); err != nil {
			return err
		}
		if result.delivered == 0 {
			s.deleteRawObjectIfOrphaned(ctx, result.rawObjectKey, "zero_delivery_ingest_job")
		}
		metrics.IngestJobProcessed()
		metrics.ObserveIngestJobLatency(time.Since(job.CreatedAt))
	}
	return nil
}

type processJobResult struct {
	rawObjectKey string
	delivered    int
}

func retryBackoff(attempts int) time.Duration {
	exp := max(attempts-1, 0)
	exp = min(exp, 8)
	return time.Duration(1<<exp)*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
}

func (s *Service) processJob(ctx context.Context, job *models.IngestJob) (processJobResult, error) {
	if job == nil {
		return processJobResult{}, nil
	}
	result := processJobResult{rawObjectKey: job.RawObjectKey}
	rc, err := s.obj.Get(ctx, job.RawObjectKey)
	if err != nil {
		return result, fmt.Errorf("get raw object: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return result, fmt.Errorf("read raw object: %w", err)
	}
	outcomes, err := s.deliver(ctx, Envelope{
		Source:     job.Source,
		RemoteIP:   job.RemoteIP,
		MailFrom:   job.MailFrom,
		Recipients: append([]string(nil), job.Recipients...),
		Metadata:   job.Metadata,
	}, raw)
	if err != nil {
		return result, err
	}
	result.delivered = deliveredCount(outcomes)
	if result.delivered == 0 {
		s.logger.Warn().Str("job_id", job.ID.String()).Msg("ingest job processed with zero deliveries")
	}
	return result, nil
}

// deliver attempts to store the envelope for every recipient and returns one
// RecipientOutcome per recipient. The per-recipient drop reasons are part of the
// return value (not just logs), so the accept decision can be asserted through
// this interface. A non-nil error signals an envelope-level failure (policy load
// or raw persistence) that should be retried, distinct from per-recipient drops.
func (s *Service) deliver(ctx context.Context, env Envelope, raw []byte) ([]RecipientOutcome, error) {
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
		// Resolution (and any auto-create) is owned solely by the resolver,
		// whose short-TTL cache already absorbs repeated zone/route lookups
		// across the SMTP session; there is no caller-supplied result to reuse.
		var result *resolver.Result
		result, err = s.resolver.Resolve(ctx, addr)
		if err != nil {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Warn().Err(err).Str("rcpt", addr).Msg("resolve failed")
			outcomes = append(outcomes, erroredOutcome(addr, "resolve_error"))
			continue
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

		retH := resolveRetention(s.store, ctx, result, s.fallbackRetentionH)
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
	s.policyMu.RLock()
	if s.policyCache != nil && time.Now().Before(s.policyExpiresAt) {
		cp := *s.policyCache
		s.policyMu.RUnlock()
		return &cp, nil
	}
	s.policyMu.RUnlock()

	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if s.policyCache != nil && time.Now().Before(s.policyExpiresAt) {
		cp := *s.policyCache
		return &cp, nil
	}

	pol, err := s.store.GetSMTPPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if pol == nil {
		cp := s.defaultPolicy
		pol = &cp
	}
	cp := *pol
	s.policyCache = &cp
	s.policyExpiresAt = time.Now().Add(2 * time.Second)
	return &cp, nil
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

func resolveRetention(st interface {
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
}, ctx context.Context, res *resolver.Result, fallback int) int {
	if res.Mailbox.RetentionHoursOverride != nil {
		return *res.Mailbox.RetentionHoursOverride
	}
	if res.Route != nil && res.Route.RetentionHoursOverride != nil {
		return *res.Route.RetentionHoursOverride
	}
	cfg, err := st.EffectiveConfig(ctx, res.Mailbox.TenantID)
	if err == nil && cfg != nil {
		return cfg.RetentionHours
	}
	if fallback > 0 {
		return fallback
	}
	return 24
}

func sanitizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	return strings.ToLower(addr)
}
