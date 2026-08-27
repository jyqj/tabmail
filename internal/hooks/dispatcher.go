package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/netpolicy"
)

type Config struct {
	URLs         string
	Secret       string
	Timeout      time.Duration
	MaxRetries   int
	RetryDelay   time.Duration
	DeadLimit    int
	PollInterval time.Duration
	BatchSize    int
}

type Event struct {
	Type       string    `json:"type"`
	Mailbox    string    `json:"mailbox"`
	MessageID  string    `json:"message_id,omitempty"`
	TenantID   string    `json:"tenant_id,omitempty"`
	Sender     string    `json:"sender,omitempty"`
	Recipients []string  `json:"recipients,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Metadata   any       `json:"metadata,omitempty"`
}

type dispatcherStore interface {
	CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error
	ClaimOutboxEvents(ctx context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error)
	MarkOutboxEventDone(ctx context.Context, id uuid.UUID) error
	MarkOutboxEventRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error
	CreateWebhookDeliveries(ctx context.Context, event *models.OutboxEvent, urls []string) error
	ListWebhookEndpoints(ctx context.Context, tenantID uuid.UUID) ([]*models.WebhookEndpoint, error)
	ClaimWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error)
	MarkWebhookDeliveryDone(ctx context.Context, id uuid.UUID) error
	MarkWebhookDeliveryRetry(ctx context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error
	ListDeadWebhookDeliveries(ctx context.Context, limit int) ([]models.DeadLetter, error)
	CountDeadWebhookDeliveries(ctx context.Context) (int, error)
}

type Dispatcher struct {
	urls         []string
	secret       string
	client       *http.Client
	logger       zerolog.Logger
	enabled      bool
	maxRetries   int
	retryDelay   time.Duration
	deadLimit    int
	pollInterval time.Duration
	batchSize    int
	store        dispatcherStore

	mu          sync.Mutex
	deadLetters []models.DeadLetter
}

func New(cfg Config, logger zerolog.Logger) *Dispatcher {
	var urls []string
	for _, u := range strings.Split(cfg.URLs, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			urls = appendUniqueURL(urls, u)
		}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryDelay := cfg.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Second
	}
	deadLimit := cfg.DeadLimit
	if deadLimit <= 0 {
		deadLimit = 100
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	metrics.WebhooksConfigured(len(urls))
	return &Dispatcher{
		urls:         urls,
		secret:       cfg.Secret,
		client:       &http.Client{Timeout: timeout},
		logger:       logger.With().Str("component", "hooks").Logger(),
		enabled:      len(urls) > 0,
		maxRetries:   maxRetries,
		retryDelay:   retryDelay,
		deadLimit:    deadLimit,
		pollInterval: pollInterval,
		batchSize:    batchSize,
	}
}

// Enabled is true for either legacy static targets or a bound store. A store
// may contain tenant-managed webhook endpoints even when no global URL exists.
func (d *Dispatcher) Enabled() bool {
	return d != nil && (d.enabled || d.store != nil)
}

func (d *Dispatcher) BindStore(st dispatcherStore) *Dispatcher {
	if d != nil {
		d.store = st
	}
	return d
}

func (d *Dispatcher) Publish(event Event) {
	if !d.Enabled() {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(event)
	if err != nil {
		metrics.WebhookFailed()
		return
	}
	if d.store != nil {
		metrics.WebhookQueued()
		if err := d.store.CreateOutboxEvent(context.Background(), &models.OutboxEvent{
			ID:         uuid.New(),
			EventType:  event.Type,
			Payload:    body,
			OccurredAt: event.OccurredAt,
			State:      "pending",
		}); err != nil {
			metrics.WebhookFailed()
			d.logger.Warn().Err(err).Str("event_type", event.Type).Msg("persist webhook outbox event")
		}
		return
	}
	for _, targetURL := range d.urls {
		metrics.WebhookQueued()
		go d.dispatchDirect(&models.WebhookDelivery{
			ID:        uuid.New(),
			URL:       targetURL,
			EventType: event.Type,
			Payload:   body,
			CreatedAt: time.Now().UTC(),
		})
	}
}

func (d *Dispatcher) DeadLetters(limit int) []models.DeadLetter {
	if d.store != nil {
		out, err := d.store.ListDeadWebhookDeliveries(context.Background(), limit)
		if err == nil {
			return out
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if limit <= 0 || limit > len(d.deadLetters) {
		limit = len(d.deadLetters)
	}
	out := make([]models.DeadLetter, 0, limit)
	for i := len(d.deadLetters) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, d.deadLetters[i])
	}
	return out
}

func (d *Dispatcher) DeadLetterSize() int {
	if d.store != nil {
		n, err := d.store.CountDeadWebhookDeliveries(context.Background())
		if err == nil {
			return n
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.deadLetters)
}

func (d *Dispatcher) Run(ctx context.Context) {
	if !d.Enabled() || d.store == nil {
		return
	}
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()
	for {
		if err := d.processBatch(ctx); err != nil {
			d.logger.Warn().Err(err).Msg("process webhook batch")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) processBatch(ctx context.Context) error {
	now := time.Now().UTC()
	outboxEvents, err := d.store.ClaimOutboxEvents(ctx, now, d.batchSize)
	if err != nil {
		return err
	}
	for _, event := range outboxEvents {
		urls, err := d.targetsForEvent(ctx, event)
		if err != nil {
			metrics.WebhookRetried()
			_ = d.store.MarkOutboxEventRetry(ctx, event.ID, err.Error(), now.Add(d.retryDelay))
			continue
		}
		if len(urls) > 0 {
			if err := d.store.CreateWebhookDeliveries(ctx, event, urls); err != nil {
				metrics.WebhookRetried()
				_ = d.store.MarkOutboxEventRetry(ctx, event.ID, err.Error(), now.Add(d.retryDelay))
				continue
			}
		}
		if err := d.store.MarkOutboxEventDone(ctx, event.ID); err != nil {
			return err
		}
	}

	deliveries, err := d.store.ClaimWebhookDeliveries(ctx, now, d.batchSize)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if err := d.dispatch(ctx, delivery); err != nil {
			dead := delivery.Attempts >= d.maxRetries
			if dead {
				metrics.WebhookFailed()
			} else {
				metrics.WebhookRetried()
			}
			_ = d.store.MarkWebhookDeliveryRetry(ctx, delivery.ID, err.Error(), now.Add(d.retryDelay*time.Duration(delivery.Attempts)), dead)
			if dead {
				d.pushDeadLetter(models.DeadLetter{
					ID:          delivery.ID.String(),
					URL:         delivery.URL,
					EventType:   delivery.EventType,
					Payload:     append([]byte(nil), delivery.Payload...),
					Attempts:    delivery.Attempts,
					LastError:   err.Error(),
					CreatedAt:   delivery.CreatedAt,
					LastTriedAt: now,
				})
			}
			continue
		}
		metrics.WebhookDelivered()
		if err := d.store.MarkWebhookDeliveryDone(ctx, delivery.ID); err != nil {
			return err
		}
	}
	return nil
}

// targetsForEvent merges deployment-level targets with active tenant targets.
// Empty event_types means all events; otherwise an endpoint receives only an
// exact event-type match. URLs are deduplicated before delivery rows are made.
func (d *Dispatcher) targetsForEvent(ctx context.Context, event *models.OutboxEvent) ([]string, error) {
	urls := append([]string(nil), d.urls...)
	if event == nil || d.store == nil {
		return urls, nil
	}
	tenantID, ok := tenantIDFromPayload(event.Payload)
	if !ok {
		return urls, nil
	}
	endpoints, err := d.store.ListWebhookEndpoints(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, ep := range endpoints {
		if ep == nil || !ep.IsActive || !endpointAcceptsEvent(ep, event.EventType) {
			continue
		}
		urls = appendUniqueURL(urls, ep.URL)
	}
	return urls, nil
}

type deliveryTarget struct {
	secret        string
	tenantManaged bool
}

// resolveDeliveryTarget resolves the current tenant endpoint configuration at
// delivery time. This lets secret rotation take effect immediately and fails
// closed if a queued tenant endpoint has since been removed or disabled.
func (d *Dispatcher) resolveDeliveryTarget(ctx context.Context, delivery *models.WebhookDelivery) (deliveryTarget, error) {
	if delivery == nil {
		return deliveryTarget{}, nil
	}
	if tenantID, ok := tenantIDFromPayload(delivery.Payload); ok && d.store != nil {
		endpoints, err := d.store.ListWebhookEndpoints(ctx, tenantID)
		if err != nil {
			return deliveryTarget{}, err
		}
		for _, ep := range endpoints {
			if ep == nil || strings.TrimSpace(ep.URL) != strings.TrimSpace(delivery.URL) {
				continue
			}
			if !ep.IsActive {
				return deliveryTarget{}, errors.New("webhook endpoint is disabled")
			}
			if !endpointAcceptsEvent(ep, delivery.EventType) {
				return deliveryTarget{}, errors.New("webhook endpoint no longer accepts this event")
			}
			secret := ""
			if ep.Secret != nil {
				secret = *ep.Secret
			}
			return deliveryTarget{secret: secret, tenantManaged: true}, nil
		}
	}
	if d.isStaticURL(delivery.URL) {
		return deliveryTarget{secret: d.secret}, nil
	}
	return deliveryTarget{}, errors.New("webhook endpoint is no longer configured")
}

func (d *Dispatcher) isStaticURL(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	for _, configured := range d.urls {
		if strings.TrimSpace(configured) == candidate {
			return true
		}
	}
	return false
}

func tenantIDFromPayload(payload json.RawMessage) (uuid.UUID, bool) {
	var partial struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(payload, &partial); err != nil || strings.TrimSpace(partial.TenantID) == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(partial.TenantID)
	return id, err == nil
}

func endpointAcceptsEvent(ep *models.WebhookEndpoint, eventType string) bool {
	if ep == nil || len(ep.EventTypes) == 0 {
		return true
	}
	for _, configured := range ep.EventTypes {
		if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(eventType)) {
			return true
		}
	}
	return false
}

func appendUniqueURL(urls []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return urls
	}
	for _, existing := range urls {
		if strings.TrimSpace(existing) == candidate {
			return urls
		}
	}
	return append(urls, candidate)
}

func (d *Dispatcher) dispatchDirect(delivery *models.WebhookDelivery) {
	if delivery == nil {
		return
	}
	var lastErr string
	for attempt := 1; attempt <= d.maxRetries; attempt++ {
		delivery.Attempts = attempt
		if attempt > 1 {
			metrics.WebhookRetried()
			time.Sleep(d.retryDelay * time.Duration(attempt-1))
		}
		if err := d.dispatch(context.Background(), delivery); err != nil {
			lastErr = err.Error()
			continue
		}
		metrics.WebhookDelivered()
		return
	}
	metrics.WebhookFailed()
	d.pushDeadLetter(models.DeadLetter{
		ID:          delivery.ID.String(),
		URL:         delivery.URL,
		EventType:   delivery.EventType,
		Payload:     append([]byte(nil), delivery.Payload...),
		Attempts:    delivery.Attempts,
		LastError:   lastErr,
		CreatedAt:   delivery.CreatedAt,
		LastTriedAt: time.Now().UTC(),
	})
}

func (d *Dispatcher) dispatch(ctx context.Context, delivery *models.WebhookDelivery) error {
	if delivery == nil {
		return nil
	}
	target, err := d.resolveDeliveryTarget(ctx, delivery)
	if err != nil {
		return err
	}
	client := d.client
	if target.tenantManaged {
		client, err = netpolicy.NewPinnedWebhookClient(ctx, delivery.URL, d.client.Timeout)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TabMail-Event", delivery.EventType)
	req.Header.Set("X-TabMail-Attempt", strconv.Itoa(delivery.Attempts))
	if target.secret != "" {
		req.Header.Set("X-TabMail-Signature", sign(target.secret, delivery.Payload))
	}
	resp, err := client.Do(req)
	if err != nil {
		d.logger.Warn().Err(err).Str("url", delivery.URL).Int("attempt", delivery.Attempts).Msg("webhook request failed")
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	lastErr := "status " + strconv.Itoa(resp.StatusCode)
	d.logger.Warn().Str("url", delivery.URL).Int("status", resp.StatusCode).Int("attempt", delivery.Attempts).Msg("webhook non-2xx response")
	return errors.New(lastErr)
}

func (d *Dispatcher) pushDeadLetter(dl models.DeadLetter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadLetters = append(d.deadLetters, dl)
	if len(d.deadLetters) > d.deadLimit {
		d.deadLetters = d.deadLetters[len(d.deadLetters)-d.deadLimit:]
	}
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
