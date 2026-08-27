// Package metrics owns the process-wide instrumentation. Every scalar is a
// prometheus collector on a private registry that /metrics serves through
// promhttp, and the JSON dashboard snapshot reads those same collectors, so a
// number never has two sources that can drift apart.
//
// The per-tenant and per-mailbox delivery counters stay hand-rolled: their keys
// are tenant ids and recipient addresses, which is exactly the unbounded label
// cardinality a Prometheus series must never carry. They are reported only in
// the dashboard's top-N lists.
package metrics

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"tabmail/internal/models"
)

var startedAt = time.Now().UTC()

var registry = prometheus.NewRegistry()

var (
	smtpSessionsOpened      = newCounter("tabmail_smtp_sessions_opened_total", "SMTP sessions opened since start.")
	smtpSessionsActive      = newGauge("tabmail_smtp_sessions_active", "SMTP sessions currently open.")
	smtpRecipientsAccepted  = newCounter("tabmail_smtp_recipients_accepted_total", "RCPT TO commands accepted.")
	smtpRecipientsRejected  = newCounter("tabmail_smtp_recipients_rejected_total", "RCPT TO commands rejected.")
	smtpMessagesAccepted    = newCounter("tabmail_smtp_messages_accepted_total", "Messages accepted by the SMTP server.")
	smtpMessagesRejected    = newCounter("tabmail_smtp_messages_rejected_total", "Messages rejected by the SMTP server.")
	smtpDeliveriesSucceeded = newCounter("tabmail_smtp_deliveries_succeeded_total", "Accepted messages delivered to a mailbox.")
	smtpDeliveriesFailed    = newCounter("tabmail_smtp_deliveries_failed_total", "Accepted messages that could not be delivered.")
	smtpBytesReceived       = newCounter("tabmail_smtp_bytes_received_total", "Message bytes read off SMTP connections.")

	webhooksConfigured   = newGauge("tabmail_webhooks_configured", "Webhook endpoints currently configured.")
	webhooksQueued       = newCounter("tabmail_webhooks_queued_total", "Webhook deliveries enqueued.")
	webhooksDelivered    = newCounter("tabmail_webhooks_delivered_total", "Webhook deliveries that reached their endpoint.")
	webhooksFailed       = newCounter("tabmail_webhooks_failed_total", "Webhook deliveries that failed.")
	webhooksRetried      = newCounter("tabmail_webhooks_retried_total", "Webhook deliveries scheduled for another attempt.")
	webhooksDeadLetters  = newGauge("tabmail_webhooks_dead_letter_size", "Webhook deliveries parked in the dead letter state.")
	webhooksBacklog      = newGauge("tabmail_webhooks_backlog", "Webhook deliveries waiting to be attempted.")
	ingestBacklog        = newGauge("tabmail_ingest_backlog", "Ingest jobs waiting or in flight.")
	ingestQueueDepth     = newGauge("tabmail_ingest_queue_depth", "Ingest jobs waiting or in flight.")
	ingestQueueReady     = newGauge("tabmail_ingest_queue_ready_depth", "Ingest jobs ready to be claimed.")
	ingestQueueInflight  = newGauge("tabmail_ingest_queue_inflight", "Ingest jobs currently being processed.")
	realtimeSubscribers  = newGauge("tabmail_realtime_subscribers_current", "Clients subscribed to the realtime hub.")
	realtimePublished    = newCounter("tabmail_realtime_events_published_total", "Events published to the realtime hub.")
	retentionMsgsDeleted = newCounter("tabmail_retention_messages_deleted_total", "Messages removed by the retention sweeper.")
	retentionObjsDeleted = newCounter("tabmail_retention_objects_deleted_total", "Stored objects removed by the retention sweeper.")
	retentionObjsFailed  = newCounter("tabmail_retention_objects_failed_total", "Stored objects the retention sweeper could not remove.")
	ingestJobsProcessed  = newCounter("tabmail_ingest_jobs_processed_total", "Ingest jobs processed successfully.")
	ingestJobsRetried    = newCounter("tabmail_ingest_jobs_retried_total", "Ingest jobs rescheduled after a failure.")
	ingestJobsDead       = newCounter("tabmail_ingest_jobs_dead_total", "Ingest jobs moved to the dead letter state.")

	mailboxCountersEvicted = newCounter("tabmail_mailbox_counters_evicted_total", "Per-mailbox delivery counters dropped to keep the map bounded.")

	ingestLatency  = newHistogram("tabmail_ingest_job_latency_seconds", "Time from enqueue to completion of an ingest job.", []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 300, 600, 1800, 3600})
	retentionSweep = newHistogram("tabmail_retention_sweep_duration_seconds", "Wall time of one retention sweep.", []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60})
)

func init() {
	registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "tabmail_uptime_seconds",
			Help: "Seconds since the process started.",
		}, func() float64 { return time.Since(startedAt).Seconds() }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "tabmail_mailbox_counters_tracked",
			Help: "Per-mailbox delivery counters currently held in memory.",
		}, func() float64 { return float64(trackedMailboxCount()) }),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			recordPoint()
		}
	}()
}

func newCounter(name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
	registry.MustRegister(c)
	return c
}

func newGauge(name, help string) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
	registry.MustRegister(g)
	return g
}

func newHistogram(name, help string, buckets []float64) prometheus.Histogram {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets})
	registry.MustRegister(h)
	return h
}

// Handler exposes the registry in the Prometheus text format.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// Backlogs are the queue depths only a database read can answer. The /metrics
// route refreshes them from its cached counts just before a scrape is served.
type Backlogs struct {
	WebhookDeadLetters int
	WebhooksPending    int
	IngestReady        int
	IngestProcessing   int
}

func SetBacklogs(b Backlogs) {
	webhooksDeadLetters.Set(float64(b.WebhookDeadLetters))
	webhooksBacklog.Set(float64(b.WebhooksPending))
	ingestBacklog.Set(float64(b.IngestReady + b.IngestProcessing))
	ingestQueueDepth.Set(float64(b.IngestReady + b.IngestProcessing))
	ingestQueueReady.Set(float64(b.IngestReady))
	ingestQueueInflight.Set(float64(b.IngestProcessing))
}

func SMTPSessionOpened() {
	smtpSessionsOpened.Inc()
	smtpSessionsActive.Inc()
}

func SMTPSessionClosed()         { smtpSessionsActive.Dec() }
func SMTPRecipientAccepted()     { smtpRecipientsAccepted.Inc() }
func SMTPRecipientRejected()     { smtpRecipientsRejected.Inc() }
func SMTPMessageAccepted()       { smtpMessagesAccepted.Inc() }
func SMTPMessageRejected()       { smtpMessagesRejected.Inc() }
func SMTPBytesReceived(n int64)  { smtpBytesReceived.Add(float64(n)) }
func WebhooksConfigured(n int)   { webhooksConfigured.Set(float64(n)) }
func WebhookQueued()             { webhooksQueued.Inc() }
func WebhookDelivered()          { webhooksDelivered.Inc() }
func WebhookFailed()             { webhooksFailed.Inc() }
func WebhookRetried()            { webhooksRetried.Inc() }
func RealtimeSubscriberAdded()   { realtimeSubscribers.Inc() }
func RealtimeSubscriberRemoved() { realtimeSubscribers.Dec() }
func RealtimeEventPublished()    { realtimePublished.Inc() }

func RetentionMessagesDeleted(n int) { retentionMsgsDeleted.Add(float64(n)) }
func RetentionObjectDeleted()        { retentionObjsDeleted.Inc() }
func RetentionObjectFailed()         { retentionObjsFailed.Inc() }
func IngestJobProcessed()            { ingestJobsProcessed.Inc() }
func IngestJobRetried()              { ingestJobsRetried.Inc() }
func IngestJobDead()                 { ingestJobsDead.Inc() }

func ObserveIngestJobLatency(d time.Duration) {
	ingestLatency.Observe(d.Seconds())
}

func ObserveRetentionSweepDuration(d time.Duration) {
	retentionSweep.Observe(d.Seconds())
}

func SMTPDeliverySucceeded(tenantID, mailbox string) {
	smtpDeliveriesSucceeded.Inc()
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.deliveriesOK++ })
	withMailboxCounter(mailbox, func(dc *deliveryCounter) { dc.deliveriesOK++ })
}

func SMTPDeliveryFailed(tenantID, mailbox string) {
	smtpDeliveriesFailed.Inc()
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.deliveriesFailed++ })
	withMailboxCounter(mailbox, func(dc *deliveryCounter) { dc.deliveriesFailed++ })
}

func TenantRecipientAccepted(tenantID string) {
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.accepted++ })
}

func TenantRecipientRejected(tenantID string) {
	withCounter(c.tenants, tenantID, func(dc *deliveryCounter) { dc.rejected++ })
}

func MailboxRecipientAccepted(mailbox string) {
	withMailboxCounter(mailbox, func(dc *deliveryCounter) { dc.accepted++ })
}

func MailboxRecipientRejected(mailbox string) {
	withMailboxCounter(mailbox, func(dc *deliveryCounter) { dc.rejected++ })
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
			SessionsOpened:      intValue(smtpSessionsOpened),
			SessionsActive:      intValue(smtpSessionsActive),
			RecipientsAccepted:  intValue(smtpRecipientsAccepted),
			RecipientsRejected:  intValue(smtpRecipientsRejected),
			MessagesAccepted:    intValue(smtpMessagesAccepted),
			MessagesRejected:    intValue(smtpMessagesRejected),
			DeliveriesSucceeded: intValue(smtpDeliveriesSucceeded),
			DeliveriesFailed:    intValue(smtpDeliveriesFailed),
			BytesReceived:       intValue(smtpBytesReceived),
		},
		Webhooks: models.WebhookMetrics{
			Enabled:        webhooksEnabled,
			Configured:     int(intValue(webhooksConfigured)),
			Queued:         intValue(webhooksQueued),
			Delivered:      intValue(webhooksDelivered),
			Failed:         intValue(webhooksFailed),
			Retried:        intValue(webhooksRetried),
			DeadLetterSize: deadLetterSize,
		},
		Realtime: models.RealtimeMetrics{
			SubscribersCurrent: intValue(realtimeSubscribers),
			EventsPublished:    intValue(realtimePublished),
		},
		TimeSeries: series,
	}
}

// intValue reads a counter or gauge back out of its collector. client_golang
// has no getter, so the value is taken from the same protobuf the exposition
// format is rendered from.
func intValue(m prometheus.Metric) int64 {
	var pb dto.Metric
	if err := m.Write(&pb); err != nil {
		return 0
	}
	switch {
	case pb.Counter != nil:
		return int64(pb.Counter.GetValue())
	case pb.Gauge != nil:
		return int64(pb.Gauge.GetValue())
	}
	return 0
}

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

func TopTenantDelivery(limit int) []models.DeliveryStats {
	return topDeliveryStats(c.tenants, limit)
}

func TopMailboxDelivery(limit int) []models.DeliveryStats {
	return topDeliveryStats(c.mailboxes, limit)
}

func trackedMailboxCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.mailboxes)
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

// The tenant map is bounded by the number of tenants, but mailbox keys are
// recipient addresses taken straight off the wire: any sender can grow the map
// without limit by addressing random local parts. Cap it. Only the busiest
// entries are ever reported, so discarding the quietest ones loses nothing an
// operator would look at, and the eviction count is exported so a truncated
// top-N is never silently misleading.
const (
	maxTrackedMailboxes = 2048
	// Evicting down to a fraction of the cap keeps the O(n log n) sweep
	// amortized rather than running it on every new key once full.
	mailboxesRetainedOnEvict = maxTrackedMailboxes * 3 / 4
)

func withMailboxCounter(mailbox string, fn func(*deliveryCounter)) {
	if mailbox == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dc := c.mailboxes[mailbox]
	if dc == nil {
		if len(c.mailboxes) >= maxTrackedMailboxes {
			evictQuietestMailboxes()
		}
		dc = &deliveryCounter{}
		c.mailboxes[mailbox] = dc
	}
	fn(dc)
}

// evictQuietestMailboxes drops the least active counters until the map is back
// to mailboxesRetainedOnEvict entries. Callers must hold c.mu.
func evictQuietestMailboxes() {
	type ranked struct {
		key   string
		total int64
	}
	entries := make([]ranked, 0, len(c.mailboxes))
	for key, dc := range c.mailboxes {
		entries = append(entries, ranked{key, dc.accepted + dc.rejected + dc.deliveriesOK + dc.deliveriesFailed})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].total < entries[j].total })

	drop := len(entries) - mailboxesRetainedOnEvict
	if drop <= 0 {
		return
	}
	for _, entry := range entries[:drop] {
		delete(c.mailboxes, entry.key)
	}
	mailboxCountersEvicted.Add(float64(drop))
}

func recordPoint() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC().Truncate(time.Minute)
	point := models.MetricPoint{
		At:                now,
		SMTPAccepted:      intValue(smtpMessagesAccepted),
		SMTPRejected:      intValue(smtpMessagesRejected),
		DeliveriesOK:      intValue(smtpDeliveriesSucceeded),
		DeliveriesFailed:  intValue(smtpDeliveriesFailed),
		WebhooksDelivered: intValue(webhooksDelivered),
		WebhooksFailed:    intValue(webhooksFailed),
		RealtimePublished: intValue(realtimePublished),
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
