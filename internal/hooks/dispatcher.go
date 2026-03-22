package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	URLs       string
	Secret     string
	Timeout    time.Duration
	MaxRetries int
	RetryDelay time.Duration
	DeadLimit  int
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

type job struct {
	id        string
	url       string
	payload   []byte
	eventType string
	attempts  int
	created   time.Time
}

type Dispatcher struct {
	urls       []string
	secret     string
	client     *http.Client
	logger     zerolog.Logger
	enabled    bool
	maxRetries int
	retryDelay time.Duration
	deadLimit  int

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
	metrics.WebhooksConfigured(len(urls))
	return &Dispatcher{
		urls:       urls,
		secret:     cfg.Secret,
		client:     &http.Client{Timeout: timeout},
		logger:     logger.With().Str("component", "hooks").Logger(),
		enabled:    len(urls) > 0,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
		deadLimit:  deadLimit,
	}
}

func (d *Dispatcher) Enabled() bool { return d != nil && d.enabled }

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
	for _, url := range d.urls {
		metrics.WebhookQueued()
		go d.dispatch(job{
			id:        uuid.NewString(),
			url:       url,
			payload:   body,
			eventType: event.Type,
			created:   time.Now().UTC(),
		})
	}
}

func (d *Dispatcher) DeadLetters(limit int) []models.DeadLetter {
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
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.deadLetters)
}

func (d *Dispatcher) dispatch(j job) {
	var lastErr string
	for attempt := 1; attempt <= d.maxRetries; attempt++ {
		j.attempts = attempt
		if attempt > 1 {
			metrics.WebhookRetried()
			time.Sleep(d.retryDelay * time.Duration(attempt-1))
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, j.url, bytes.NewReader(j.payload))
		if err != nil {
			lastErr = err.Error()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TabMail-Event", j.eventType)
		req.Header.Set("X-TabMail-Attempt", strconv.Itoa(attempt))
		if d.secret != "" {
			req.Header.Set("X-TabMail-Signature", sign(d.secret, j.payload))
		}
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err.Error()
			d.logger.Warn().Err(err).Str("url", j.url).Int("attempt", attempt).Msg("webhook request failed")
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			metrics.WebhookDelivered()
			return
		}
		lastErr = "status " + strconv.Itoa(resp.StatusCode)
		d.logger.Warn().Str("url", j.url).Int("status", resp.StatusCode).Int("attempt", attempt).Msg("webhook non-2xx response")
	}
	metrics.WebhookFailed()
	d.pushDeadLetter(models.DeadLetter{
		ID:          j.id,
		URL:         j.url,
		EventType:   j.eventType,
		Payload:     append([]byte(nil), j.payload...),
		Attempts:    j.attempts,
		LastError:   lastErr,
		CreatedAt:   j.created,
		LastTriedAt: time.Now().UTC(),
	})
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
