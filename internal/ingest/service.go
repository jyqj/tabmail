package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
)

type serviceStore interface {
	GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int) (bool, error)
	CountTenantMessagesSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error)
	CreateIngestJob(ctx context.Context, job *models.IngestJob) error
	ClaimIngestJobs(ctx context.Context, now time.Time, limit int) ([]*models.IngestJob, error)
	MarkIngestJobDone(ctx context.Context, id uuid.UUID) error
	MarkIngestJobRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
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

type Service struct {
	store              serviceStore
	obj                store.ObjectStore
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

func (s *Service) Accept(ctx context.Context, env Envelope, raw []byte, rcptChecks map[string]*resolver.Result) (AcceptResult, error) {
	if len(env.Recipients) == 0 {
		return AcceptResult{}, nil
	}
	if s.durable {
		objKey, err := s.persistRaw(ctx, raw)
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
		if err := s.store.CreateIngestJob(ctx, job); err != nil {
			return AcceptResult{}, err
		}
		return AcceptResult{Queued: true}, nil
	}
	delivered, err := s.deliver(ctx, env, raw, rcptChecks)
	if err != nil {
		return AcceptResult{}, err
	}
	return AcceptResult{Delivered: delivered}, nil
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
		if err := s.processJob(ctx, job); err != nil {
			dead := job.Attempts >= s.maxRetries
			backoff := retryBackoff(job.Attempts)
			nextAttempt := time.Now().UTC().Add(backoff)
			if markErr := s.store.MarkIngestJobRetry(ctx, job.ID, err.Error(), nextAttempt, dead); markErr != nil {
				return markErr
			}
			if dead {
				metrics.IngestJobDead()
				metrics.ObserveIngestJobLatency(time.Since(job.CreatedAt))
			} else {
				metrics.IngestJobRetried()
			}
			continue
		}
		if err := s.store.MarkIngestJobDone(ctx, job.ID); err != nil {
			return err
		}
		metrics.IngestJobProcessed()
		metrics.ObserveIngestJobLatency(time.Since(job.CreatedAt))
	}
	return nil
}

func retryBackoff(attempts int) time.Duration {
	exp := max(attempts-1, 0)
	exp = min(exp, 8)
	return time.Duration(1<<exp)*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
}

func (s *Service) processJob(ctx context.Context, job *models.IngestJob) error {
	if job == nil {
		return nil
	}
	rc, err := s.obj.Get(ctx, job.RawObjectKey)
	if err != nil {
		return fmt.Errorf("get raw object: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read raw object: %w", err)
	}
	delivered, err := s.deliver(ctx, Envelope{
		Source:     job.Source,
		RemoteIP:   job.RemoteIP,
		MailFrom:   job.MailFrom,
		Recipients: append([]string(nil), job.Recipients...),
		Metadata:   job.Metadata,
	}, raw, nil)
	if err != nil {
		return err
	}
	if delivered == 0 {
		s.logger.Warn().Str("job_id", job.ID.String()).Msg("ingest job processed with zero deliveries")
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, env Envelope, raw []byte, rcptChecks map[string]*resolver.Result) (int, error) {
	if len(env.Recipients) == 0 {
		return 0, nil
	}
	envMime, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		s.logger.Warn().Err(err).Msg("parsing MIME envelope (storing raw only)")
	}

	now := time.Now()
	successes := 0
	tenantConfigs := map[uuid.UUID]*models.EffectiveConfig{}
	pol, err := s.currentPolicy(ctx)
	if err != nil {
		return 0, fmt.Errorf("load smtp policy: %w", err)
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

	objKey, err := s.persistRaw(ctx, raw)
	if err != nil {
		return 0, err
	}

	for _, rcpt := range env.Recipients {
		addr := sanitizeAddr(rcpt)
		var result *resolver.Result
		if checked, ok := rcptChecks[addr]; ok && checked != nil && checked.Mailbox != nil {
			result = checked
		} else {
			result, err = s.resolver.Resolve(ctx, addr)
			if err != nil {
				metrics.MailboxRecipientRejected(addr)
				s.logger.Warn().Err(err).Str("rcpt", addr).Msg("resolve failed")
				continue
			}
		}
		if result == nil || result.Mailbox == nil || result.Zone == nil {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Debug().Str("rcpt", addr).Msg("no matching zone/route, rejecting recipient")
			continue
		}
		if !result.Zone.IsVerified || !result.Zone.MXVerified {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Warn().Str("rcpt", addr).Msg("recipient zone is not verified")
			continue
		}
		if result.Created {
			s.logger.Info().Str("address", addr).Msg("auto-created mailbox")
		}

		mb := result.Mailbox
		if !policy.ShouldStoreDomain(mb.ResolvedDomain, pol.DefaultStore, pol.StoreDomains, pol.DiscardDomains) {
			s.logger.Info().Str("mailbox", mb.FullAddress).Msg("message accepted but discarded by store policy")
			continue
		}
		cfg, ok := tenantConfigs[mb.TenantID]
		if !ok {
			cfg, err = s.store.EffectiveConfig(ctx, mb.TenantID)
			if err != nil || cfg == nil {
				s.logger.Warn().Err(err).Str("mailbox", mb.FullAddress).Msg("load tenant config")
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
			continue
		} else if !ok {
			s.logger.Warn().
				Str("tenant", mb.TenantID.String()).
				Int("limit", cfg.DailyQuota).
				Msg("tenant daily quota exceeded")
			continue
		}
		ok, err := s.store.CreateMessageWithQuota(ctx, msg, cfg.MaxMessagesPerMailbox)
		if err != nil {
			_ = s.releaseTenantDaily(ctx, mb.TenantID)
			metrics.SMTPDeliveryFailed(mb.TenantID.String(), mb.FullAddress)
			s.logger.Err(err).Str("mailbox", mb.FullAddress).Msg("storing message metadata")
			continue
		}
		if !ok {
			_ = s.releaseTenantDaily(ctx, mb.TenantID)
			s.logger.Warn().
				Str("mailbox", mb.FullAddress).
				Int("limit", cfg.MaxMessagesPerMailbox).
				Msg("mailbox message quota exceeded")
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
		successes++
		s.logger.Info().
			Str("from", env.MailFrom).
			Str("to", addr).
			Str("subject", subject).
			Int64("size", int64(len(raw))).
			Msg("message delivered")
	}
	return successes, nil
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

func (s *Service) persistRaw(ctx context.Context, raw []byte) (string, error) {
	key := objectKeyForRaw(raw)
	exists, err := s.obj.Exists(ctx, key)
	if err != nil {
		return "", fmt.Errorf("checking raw object existence: %w", err)
	}
	if !exists {
		if err := s.obj.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))); err != nil {
			return "", fmt.Errorf("storing raw .eml: %w", err)
		}
	}
	return key, nil
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

func objectKeyForRaw(raw []byte) string {
	sum := sha256.Sum256(raw)
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("sha256/%s/%s.eml", hexSum[:2], hexSum)
}
