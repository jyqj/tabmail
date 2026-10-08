package metrics

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"tabmail/internal/models"
)

// Decode the public JSON shape so this fixed regression also compiles before
// the optional aggregate field exists in the production model.
type r5DeliveryWire struct {
	Key              string `json:"key"`
	Aggregate        bool   `json:"aggregate"`
	Accepted         int64  `json:"accepted"`
	Rejected         int64  `json:"rejected"`
	DeliveriesOK     int64  `json:"deliveries_ok"`
	DeliveriesFailed int64  `json:"deliveries_failed"`
}

func r5DeliveryRows(t *testing.T, rows []models.DeliveryStats) []r5DeliveryWire {
	t.Helper()
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []r5DeliveryWire
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func r5DeliverySum(rows []r5DeliveryWire, aggregateOnly bool) [4]int64 {
	var sum [4]int64
	for _, row := range rows {
		if aggregateOnly && !row.Aggregate {
			continue
		}
		sum[0] += row.Accepted
		sum[1] += row.Rejected
		sum[2] += row.DeliveriesOK
		sum[3] += row.DeliveriesFailed
	}
	return sum
}

func r5DeliveryDelta(after, before [4]int64) [4]int64 {
	for i := range after {
		after[i] -= before[i]
	}
	return after
}

func TestR5DeliveryMetricsCardinality(t *testing.T) {
	const capacity, flood = 4096, 8192
	const stableTenant, stableMailbox = "__overflow__", "__overflow__@metrics.example.test"

	// Fill the tenant dimension first: it must not consume mailbox capacity.
	mailboxesBefore := r5DeliveryRows(t, TopMailboxDelivery(1<<30))
	TenantRecipientAccepted(stableTenant)
	for i := 1; i < capacity; i++ {
		TenantRecipientAccepted(fmt.Sprintf("r5-tracked-tenant-%04d", i))
	}
	t.Run("tenant_capacity_is_independent_of_mailboxes", func(t *testing.T) {
		if got := r5DeliveryRows(t, TopMailboxDelivery(1<<30)); len(got) != len(mailboxesBefore) || r5DeliverySum(got, false) != r5DeliverySum(mailboxesBefore, false) {
			t.Fatal("tenant admissions changed mailbox counters")
		}
	})
	MailboxRecipientAccepted(stableMailbox)
	for i := 1; i < capacity; i++ {
		MailboxRecipientAccepted(fmt.Sprintf("r5-tracked-%04d@metrics.example.test", i))
	}
	beforeTenant := r5DeliveryRows(t, TopTenantDelivery(1<<30))
	beforeMailbox := r5DeliveryRows(t, TopMailboxDelivery(1<<30))
	beforeGlobal := Snapshot(false, 0).SMTP

	// These are the same public accounting calls used by SMTP and ingest. Every
	// key beyond the admitted set must share fixed storage, also under contention.
	var wg sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < flood; i += 32 {
				tenant := fmt.Sprintf("r5-excess-tenant-%05d", i)
				mailbox := fmt.Sprintf("r5-excess-%05d@metrics.example.test", i)
				SMTPRecipientAccepted()
				TenantRecipientAccepted(tenant)
				MailboxRecipientAccepted(mailbox)
				SMTPRecipientRejected()
				TenantRecipientRejected(tenant)
				MailboxRecipientRejected(mailbox)
				SMTPDeliverySucceeded(tenant, mailbox)
				SMTPDeliveryFailed(tenant, mailbox)
			}
		}(worker)
	}
	wg.Wait()
	afterTenant := r5DeliveryRows(t, TopTenantDelivery(1<<30))
	afterMailbox := r5DeliveryRows(t, TopMailboxDelivery(1<<30))
	afterGlobal := Snapshot(false, 0).SMTP

	for _, dimension := range []struct {
		name   string
		before []r5DeliveryWire
		after  []r5DeliveryWire
		m      map[string]*deliveryCounter
	}{
		{"tenant", beforeTenant, afterTenant, c.tenants},
		{"mailbox", beforeMailbox, afterMailbox, c.mailboxes},
	} {
		t.Run(dimension.name+"_retained_keys_bounded", func(t *testing.T) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if got := len(dimension.m); got != capacity {
				t.Fatalf("retained %d real keys after concurrent flood, want %d", got, capacity)
			}
		})
		t.Run(dimension.name+"_overflow_accounts_every_event", func(t *testing.T) {
			aggregates := 0
			for _, row := range dimension.after {
				if row.Aggregate {
					aggregates++
					if row.Key != "" {
						t.Errorf("aggregate impersonates real key %q", row.Key)
					}
				}
			}
			if aggregates != 1 {
				t.Fatalf("got %d aggregate rows, want one separate overflow counter", aggregates)
			}
			got := r5DeliveryDelta(r5DeliverySum(dimension.after, true), r5DeliverySum(dimension.before, true))
			if want := [4]int64{flood, flood, flood, flood}; got != want {
				t.Fatalf("aggregate event delta %v, want %v", got, want)
			}
		})
	}
	t.Run("global_counters_remain_exact", func(t *testing.T) {
		got := [4]int64{
			afterGlobal.RecipientsAccepted - beforeGlobal.RecipientsAccepted,
			afterGlobal.RecipientsRejected - beforeGlobal.RecipientsRejected,
			afterGlobal.DeliveriesSucceeded - beforeGlobal.DeliveriesSucceeded,
			afterGlobal.DeliveriesFailed - beforeGlobal.DeliveriesFailed,
		}
		if want := [4]int64{flood, flood, flood, flood}; got != want {
			t.Fatalf("global event delta %v, want %v", got, want)
		}
	})
	t.Run("full_projection_conserves_totals", func(t *testing.T) {
		for _, got := range [][4]int64{
			r5DeliveryDelta(r5DeliverySum(afterTenant, false), r5DeliverySum(beforeTenant, false)),
			r5DeliveryDelta(r5DeliverySum(afterMailbox, false), r5DeliverySum(beforeMailbox, false)),
		} {
			if want := [4]int64{flood, flood, flood, flood}; got != want {
				t.Errorf("projected event delta %v, want %v", got, want)
			}
		}
	})

	// A legitimate key resembling an overflow sentinel stays an ordinary row.
	// Admission is stable: reaching capacity must never evict its past counters.
	find := func(rows []r5DeliveryWire, key string) r5DeliveryWire {
		for _, row := range rows {
			if !row.Aggregate && row.Key == key {
				return row
			}
		}
		return r5DeliveryWire{}
	}
	SMTPDeliverySucceeded(stableTenant, stableMailbox)
	SMTPDeliveryFailed(stableTenant, stableMailbox)
	TenantRecipientAccepted(stableTenant)
	TenantRecipientRejected(stableTenant)
	MailboxRecipientAccepted(stableMailbox)
	MailboxRecipientRejected(stableMailbox)
	for _, dimension := range []struct {
		name, key string
		before    []r5DeliveryWire
		after     []r5DeliveryWire
	}{
		{"tenant", stableTenant, afterTenant, r5DeliveryRows(t, TopTenantDelivery(1<<30))},
		{"mailbox", stableMailbox, afterMailbox, r5DeliveryRows(t, TopMailboxDelivery(1<<30))},
	} {
		t.Run(dimension.name+"_existing_real_key_survives_capacity", func(t *testing.T) {
			want := find(dimension.before, dimension.key)
			want.Accepted++
			want.Rejected++
			want.DeliveriesOK++
			want.DeliveriesFailed++
			if got := find(dimension.after, dimension.key); got != want || got.Key != dimension.key {
				t.Fatalf("real key counter %+v, want %+v", got, want)
			}
		})
	}
	for _, limit := range []int{1, 0, 10, 1 << 30} {
		t.Run(fmt.Sprintf("top_limit_%d_includes_aggregate", limit), func(t *testing.T) {
			want := limit
			if want <= 0 {
				want = 10
			}
			if want > capacity+1 {
				want = capacity + 1
			}
			for _, rows := range [][]r5DeliveryWire{
				r5DeliveryRows(t, TopTenantDelivery(limit)), r5DeliveryRows(t, TopMailboxDelivery(limit)),
			} {
				if len(rows) != want {
					t.Errorf("Top(%d) returned %d rows, want %d including overflow", limit, len(rows), want)
				}
				if len(rows) == 0 || !rows[len(rows)-1].Aggregate {
					t.Error("Top hid the overflow summary behind individual delivery rows")
				}
			}
		})
	}
	t.Run("empty_keys_keep_existing_ignored_semantics", func(t *testing.T) {
		beforeT := r5DeliverySum(r5DeliveryRows(t, TopTenantDelivery(1<<30)), false)
		beforeM := r5DeliverySum(r5DeliveryRows(t, TopMailboxDelivery(1<<30)), false)
		TenantRecipientAccepted("")
		TenantRecipientRejected("")
		MailboxRecipientAccepted("")
		MailboxRecipientRejected("")
		if got := r5DeliverySum(r5DeliveryRows(t, TopTenantDelivery(1<<30)), false); got != beforeT {
			t.Error("empty tenant key changed counters")
		}
		if got := r5DeliverySum(r5DeliveryRows(t, TopMailboxDelivery(1<<30)), false); got != beforeM {
			t.Error("empty mailbox key changed counters")
		}
	})
	t.Run("prometheus_stays_free_of_per_key_labels", func(t *testing.T) {
		body := RenderPrometheus(Snapshot(false, 0), nil)
		for _, private := range []string{stableTenant, "metrics.example.test", "r5-excess-tenant", "tenant=", "mailbox="} {
			if strings.Contains(body, private) {
				t.Fatalf("Prometheus output contains high-cardinality identity %q", private)
			}
		}
		if !strings.Contains(body, fmt.Sprintf("tabmail_smtp_deliveries_succeeded_total %d", smtpDeliveriesSucceeded.Load())) {
			t.Fatal("global delivery total missing from Prometheus")
		}
	})
}
