package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
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
			urls = append(urls, u)
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

func (d *Dispatcher) Enabled() bool { return d != nil && d.enabled }

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
	for _, url := range d.urls {
		metrics.WebhookQueued()
		go d.dispatchDirect(&models.WebhookDelivery{
			ID:        uuid.New(),
			URL:       url,
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
		if err := d.store.CreateWebhookDeliveries(ctx, event, d.urls); err != nil {
			metrics.WebhookRetried()
			_ = d.store.MarkOutboxEventRetry(ctx, event.ID, err.Error(), now.Add(d.retryDelay))
			continue
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
	var lastErr string
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TabMail-Event", delivery.EventType)
	req.Header.Set("X-TabMail-Attempt", strconv.Itoa(delivery.Attempts))
	if d.secret != "" {
		req.Header.Set("X-TabMail-Signature", sign(d.secret, delivery.Payload))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		d.logger.Warn().Err(err).Str("url", delivery.URL).Int("attempt", delivery.Attempts).Msg("webhook request failed")
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	lastErr = "status " + strconv.Itoa(resp.StatusCode)
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
