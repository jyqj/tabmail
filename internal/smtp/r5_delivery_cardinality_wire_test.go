package smtp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/ingest"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

func TestR5SMTPRejectedAddressMetricsCardinality(t *testing.T) {
	const flood = 8192
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()
	rv := resolver.New(st, policy.NamingFull, true)
	pol := models.SMTPPolicy{DefaultAccept: true, DefaultStore: true, RejectDomains: []string{"blocked.metrics.test"}}
	ingestSvc := ingest.NewService(st, obj, rv, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), pol, 24, nil, config.Ingest{}, zerolog.Nop())
	s := NewServer(config.SMTP{Domain: "mx.example.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: 1024}, ingestSvc, rv, zerolog.Nop())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
	started := r5Start(s, context.Background())
	r5Await(t, s.ready)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("SMTP join: %v", err)
		}
		if err := r5Result(t, started); err != nil {
			t.Errorf("SMTP serve: %v", err)
		}
	})
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	r5CloudWire(t, conn, r, "", "220")
	r5CloudWire(t, conn, r, "EHLO metrics.example.test", "250")
	r5CloudWire(t, conn, r, "MAIL FROM:<sender@example.test>", "250")
	before := metrics.Snapshot(false, 0).SMTP
	sumRejected := func(rows []models.DeliveryStats) int64 {
		var total int64
		for _, row := range rows {
			total += row.Rejected
		}
		return total
	}
	beforeRejected := sumRejected(metrics.TopMailboxDelivery(1 << 30))
	for i := 0; i < flood; i++ {
		// Every address is syntactically valid and rejected by the real RCPT
		// policy path. No mailbox provisioning, DATA, or mock metric call occurs.
		r5CloudWire(t, conn, r, fmt.Sprintf("RCPT TO:<flood-%05d@blocked.metrics.test>", i), "550")
	}
	r5CloudWire(t, conn, r, "QUIT", "221")
	after := metrics.Snapshot(false, 0).SMTP
	rows := metrics.TopMailboxDelivery(1 << 30)
	t.Logf("real TCP SMTP: %d distinct RCPT commands, %d observed 550 replies; detail rows=%d", flood, flood, len(rows))

	t.Run("rejections_keep_global_total_without_accepting_mail", func(t *testing.T) {
		if got := after.RecipientsRejected - before.RecipientsRejected; got != flood {
			t.Fatalf("global rejection delta %d, want %d", got, flood)
		}
		if after.RecipientsAccepted != before.RecipientsAccepted || after.MessagesAccepted != before.MessagesAccepted || after.DeliveriesSucceeded != before.DeliveriesSucceeded {
			t.Fatal("rejected addresses were counted as accepted or delivered")
		}
		mailboxes, err := st.CountAllMailboxes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		messages, err := st.CountAllMessages(context.Background())
		if err != nil || mailboxes != 0 || messages != 0 || obj.Count() != 0 {
			t.Fatalf("rejected flood provisioned data: mailboxes=%d messages=%d objects=%d err=%v", mailboxes, messages, obj.Count(), err)
		}
	})
	t.Run("unprovisioned_addresses_cannot_expand_details", func(t *testing.T) {
		if len(rows) > 4097 {
			t.Fatalf("arbitrary rejected addresses retained %d rows, maximum 4096 real keys plus one aggregate", len(rows))
		}
	})
	t.Run("bounded_projection_does_not_drop_rejections", func(t *testing.T) {
		if got := sumRejected(rows) - beforeRejected; got != flood {
			t.Fatalf("projected rejection delta %d, want %d", got, flood)
		}
	})
	t.Run("overflow_is_explicit_in_the_actual_top_api", func(t *testing.T) {
		encoded, err := json.Marshal(metrics.TopMailboxDelivery(10))
		if err != nil {
			t.Fatal(err)
		}
		var wire []struct {
			Key       string `json:"key"`
			Aggregate bool   `json:"aggregate"`
			Rejected  int64  `json:"rejected"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if len(wire) != 10 {
			t.Fatalf("Top(10) returned %d rows", len(wire))
		}
		last := wire[len(wire)-1]
		if !last.Aggregate || last.Key != "" || last.Rejected < flood-4096 {
			t.Fatalf("Top hides rejected-address overflow: %+v", last)
		}
	})
	t.Run("prometheus_never_exports_attacker_address_labels", func(t *testing.T) {
		body := metrics.RenderPrometheus(metrics.Snapshot(false, 0), nil)
		if strings.Contains(body, "blocked.metrics.test") || strings.Contains(body, "mailbox=") {
			t.Fatal("SMTP rejection flood created per-address Prometheus labels")
		}
		if !strings.Contains(body, fmt.Sprintf("tabmail_smtp_recipients_rejected_total %d", after.RecipientsRejected)) {
			t.Fatal("Prometheus lost the global rejection count")
		}
	})
}
