package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tabmail/internal/models"
)

var startedAt = time.Now().UTC()

type deliveryCounter struct {
	accepted         int64
	rejected         int64
	deliveriesOK     int64
	deliveriesFailed int64
}

type collector struct {
	mu         sync.Mutex
	timeSeries []models.MetricPoint
	tenants    map[string]*deliveryCounter
	mailboxes  map[string]*deliveryCounter
}

var c = &collector{
	timeSeries: make([]models.MetricPoint, 0, 60),
	tenants:    make(map[string]*deliveryCounter),
	mailboxes:  make(map[string]*deliveryCounter),
}

var (
	smtpSessionsOpened      atomic.Int64
	smtpSessionsActive      atomic.Int64
	smtpRecipientsAccepted  atomic.Int64
	smtpRecipientsRejected  atomic.Int64
	smtpMessagesAccepted    atomic.Int64
	smtpMessagesRejected    atomic.Int64
	smtpDeliveriesSucceeded atomic.Int64
	smtpDeliveriesFailed    atomic.Int64
	smtpBytesReceived       atomic.Int64
	webhooksConfigured      atomic.Int64
	webhooksQueued          atomic.Int64
	webhooksDelivered       atomic.Int64
	webhooksFailed          atomic.Int64
	webhooksRetried         atomic.Int64
	realtimeSubscribers     atomic.Int64
	realtimePublished       atomic.Int64
)

func init() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			recordPoint()
		}
	}()
}

func SMTPSessionOpened() {
	smtpSessionsOpened.Add(1)
	smtpSessionsActive.Add(1)
}

func SMTPSessionClosed()         { smtpSessionsActive.Add(-1) }
func SMTPRecipientAccepted()     { smtpRecipientsAccepted.Add(1) }
func SMTPRecipientRejected()     { smtpRecipientsRejected.Add(1) }
func SMTPMessageAccepted()       { smtpMessagesAccepted.Add(1) }
func SMTPMessageRejected()       { smtpMessagesRejected.Add(1) }
func SMTPBytesReceived(n int64)  { smtpBytesReceived.Add(n) }
func WebhooksConfigured(n int)   { webhooksConfigured.Store(int64(n)) }
func WebhookQueued()             { webhooksQueued.Add(1) }
func WebhookDelivered()          { webhooksDelivered.Add(1) }
func WebhookFailed()             { webhooksFailed.Add(1) }
func WebhookRetried()            { webhooksRetried.Add(1) }
func RealtimeSubscriberAdded()   { realtimeSubscribers.Add(1) }
func RealtimeSubscriberRemoved() { realtimeSubscribers.Add(-1) }
func RealtimeEventPublished()    { realtimePublished.Add(1) }

func SMTPDeliverySucceeded(tenantID, mailbox string) {
	smtpDeliveriesSucceeded.Add(1)
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.deliveriesOK++ })
	withCounter(c.mailboxes, mailbox, func(dc *deliveryCounter) { dc.deliveriesOK++ })
}

func SMTPDeliveryFailed(tenantID, mailbox string) {
	smtpDeliveriesFailed.Add(1)
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.deliveriesFailed++ })
	withCounter(c.mailboxes, mailbox, func(dc *deliveryCounter) { dc.deliveriesFailed++ })
}

func TenantRecipientAccepted(tenantID string) {
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.accepted++ })
}

func TenantRecipientRejected(tenantID string) {
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.rejected++ })
}

func MailboxRecipientAccepted(mailbox string) {
	withCounter(c.mailboxes, mailbox, func(dc *deliveryCounter) { dc.accepted++ })
}

func MailboxRecipientRejected(mailbox string) {
	withCounter(c.mailboxes, mailbox, func(dc *deliveryCounter) { dc.rejected++ })
}

func Snapshot(webhooksEnabled bool, deadLetterSize int) models.MetricsSnapshot {
	recordPoint()
	c.mu.Lock()
	defer c.mu.Unlock()
	series := append([]models.MetricPoint(nil), c.timeSeries...)
	return models.MetricsSnapshot{
		StartedAt:     startedAt,
		UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		SMTP: models.SMTPMetrics{
			SessionsOpened:      smtpSessionsOpened.Load(),
			SessionsActive:      smtpSessionsActive.Load(),
			RecipientsAccepted:  smtpRecipientsAccepted.Load(),
			RecipientsRejected:  smtpRecipientsRejected.Load(),
			MessagesAccepted:    smtpMessagesAccepted.Load(),
			MessagesRejected:    smtpMessagesRejected.Load(),
			DeliveriesSucceeded: smtpDeliveriesSucceeded.Load(),
			DeliveriesFailed:    smtpDeliveriesFailed.Load(),
			BytesReceived:       smtpBytesReceived.Load(),
		},
		Webhooks: models.WebhookMetrics{
			Enabled:        webhooksEnabled,
			Configured:     int(webhooksConfigured.Load()),
			Queued:         webhooksQueued.Load(),
			Delivered:      webhooksDelivered.Load(),
			Failed:         webhooksFailed.Load(),
			Retried:        webhooksRetried.Load(),
			DeadLetterSize: deadLetterSize,
		},
		Realtime: models.RealtimeMetrics{
			SubscribersCurrent: realtimeSubscribers.Load(),
			EventsPublished:    realtimePublished.Load(),
		},
		TimeSeries: series,
	}
}

func TopTenantDelivery(limit int) []models.DeliveryStats {
	return topDeliveryStats(c.tenants, limit)
}

func TopMailboxDelivery(limit int) []models.DeliveryStats {
	return topDeliveryStats(c.mailboxes, limit)
}

func withCounter(m map[string]*deliveryCounter, key string, fn func(*deliveryCounter)) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dc := m[key]
	if dc == nil {
		dc = &deliveryCounter{}
		m[key] = dc
	}
	fn(dc)
}

func recordPoint() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC().Truncate(time.Minute)
	point := models.MetricPoint{
		At:                now,
		SMTPAccepted:      smtpMessagesAccepted.Load(),
		SMTPRejected:      smtpMessagesRejected.Load(),
		DeliveriesOK:      smtpDeliveriesSucceeded.Load(),
		DeliveriesFailed:  smtpDeliveriesFailed.Load(),
		WebhooksDelivered: webhooksDelivered.Load(),
		WebhooksFailed:    webhooksFailed.Load(),
		RealtimePublished: realtimePublished.Load(),
	}
	if n := len(c.timeSeries); n > 0 && c.timeSeries[n-1].At.Equal(now) {
		c.timeSeries[n-1] = point
		return
	}
	c.timeSeries = append(c.timeSeries, point)
	if len(c.timeSeries) > 60 {
		c.timeSeries = c.timeSeries[len(c.timeSeries)-60:]
	}
}

func topDeliveryStats(m map[string]*deliveryCounter, limit int) []models.DeliveryStats {
	if limit <= 0 {
		limit = 10
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]models.DeliveryStats, 0, len(m))
	for key, dc := range m {
		out = append(out, models.DeliveryStats{
			Key:              key,
			Accepted:         dc.accepted,
			Rejected:         dc.rejected,
			DeliveriesOK:     dc.deliveriesOK,
			DeliveriesFailed: dc.deliveriesFailed,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return (out[i].DeliveriesOK + out[i].DeliveriesFailed) > (out[j].DeliveriesOK + out[j].DeliveriesFailed)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func RenderPrometheus(snapshot models.MetricsSnapshot, extras map[string]float64) string {
	var b strings.Builder

	writeGauge := func(name string, value any) {
		fmt.Fprintf(&b, "%s %v\n", name, value)
	}

	writeGauge("tabmail_uptime_seconds", snapshot.UptimeSeconds)
	writeGauge("tabmail_smtp_sessions_opened_total", snapshot.SMTP.SessionsOpened)
	writeGauge("tabmail_smtp_sessions_active", snapshot.SMTP.SessionsActive)
	writeGauge("tabmail_smtp_recipients_accepted_total", snapshot.SMTP.RecipientsAccepted)
	writeGauge("tabmail_smtp_recipients_rejected_total", snapshot.SMTP.RecipientsRejected)
	writeGauge("tabmail_smtp_messages_accepted_total", snapshot.SMTP.MessagesAccepted)
	writeGauge("tabmail_smtp_messages_rejected_total", snapshot.SMTP.MessagesRejected)
	writeGauge("tabmail_smtp_deliveries_succeeded_total", snapshot.SMTP.DeliveriesSucceeded)
	writeGauge("tabmail_smtp_deliveries_failed_total", snapshot.SMTP.DeliveriesFailed)
	writeGauge("tabmail_smtp_bytes_received_total", snapshot.SMTP.BytesReceived)
	writeGauge("tabmail_webhooks_configured", snapshot.Webhooks.Configured)
	writeGauge("tabmail_webhooks_queued_total", snapshot.Webhooks.Queued)
	writeGauge("tabmail_webhooks_delivered_total", snapshot.Webhooks.Delivered)
	writeGauge("tabmail_webhooks_failed_total", snapshot.Webhooks.Failed)
	writeGauge("tabmail_webhooks_retried_total", snapshot.Webhooks.Retried)
	writeGauge("tabmail_webhooks_dead_letter_size", snapshot.Webhooks.DeadLetterSize)
	writeGauge("tabmail_realtime_subscribers_current", snapshot.Realtime.SubscribersCurrent)
	writeGauge("tabmail_realtime_events_published_total", snapshot.Realtime.EventsPublished)

	keys := make([]string, 0, len(extras))
	for k := range extras {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeGauge(key, extras[key])
	}
	return b.String()
}
