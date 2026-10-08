package smtp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

type orphanWireObjects struct {
	*testutil.MemoryObjectStore
	failDelete bool
	deletes    atomic.Int32
}

func (o *orphanWireObjects) Delete(ctx context.Context, key string) error {
	o.deletes.Add(1)
	if o.failDelete {
		return errors.New("synthetic orphan delete failure")
	}
	return o.MemoryObjectStore.Delete(ctx, key)
}

// Real SMTP DATA must preserve its failure reply even after successful cleanup
// or durable retry handoff. Only a real stored message is acknowledged.
func TestR5NonDurableOrphanSMTPAck(t *testing.T) {
	for _, name := range []string{"released remains 451", "retry handoff remains 451", "stored remains 250"} {
		t.Run(name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			obj := &orphanWireObjects{MemoryObjectStore: testutil.NewMemoryObjectStore(), failDelete: name == "retry handoff remains 451"}
			tenant, plan, zone := uuid.New(), uuid.New(), uuid.New()
			st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: 1024, MaxMessagesPerMailbox: 100, DailyQuota: 100, RetentionHours: 24})
			st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
			st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
			st.SeedMailbox(mb)
			rv := resolver.New(st, policy.NamingFull, true)
			pol := models.SMTPPolicy{DefaultAccept: true, DefaultStore: name == "stored remains 250"}
			svc := ingest.NewService(st, obj, rv, nil, nil, pol, 24, nil, config.Ingest{}, zerolog.Nop())
			s := NewServer(config.SMTP{Domain: "mx.mail.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: 1024}, svc, rv, zerolog.Nop())
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
					t.Errorf("SMTP shutdown: %v", err)
				}
				if err := r5Result(t, started); err != nil {
					t.Errorf("SMTP join: %v", err)
				}
			})
			conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			r := bufio.NewReader(conn)
			r5CloudWire(t, conn, r, "", "220")
			r5CloudWire(t, conn, r, "EHLO client.example.test", "250")
			r5CloudWire(t, conn, r, "MAIL FROM:<sender@example.test>", "250")
			r5CloudWire(t, conn, r, "RCPT TO:<reader@mail.test>", "250")
			r5CloudWire(t, conn, r, "DATA", "354")
			body := "Subject: orphan wire\r\n\r\nsynthetic content\r\n"
			code, wantDeletes, wantObjects, wantMessages := "451", int32(1), 0, 0
			if name == "retry handoff remains 451" {
				wantObjects = 1
			}
			if name == "stored remains 250" {
				code, wantDeletes, wantObjects, wantMessages = "250", 0, 1, 1
			}
			r5CloudWire(t, conn, r, body+".", code)
			_, count, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
			if err != nil || count != wantMessages || obj.deletes.Load() != wantDeletes || obj.Count() != wantObjects {
				t.Errorf("DATA effects: messages=%d error=%v deletes=%d objects=%d", count, err, obj.deletes.Load(), obj.Count())
			}
			keys, err := st.ListPendingOrphanRetries(context.Background(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if name == "retry handoff remains 451" {
				if len(keys) != 1 || keys[0] != rawobject.Key([]byte(body)) {
					t.Errorf("wrong durable handoff: %v", keys)
				}
			} else if len(keys) != 0 {
				t.Errorf("unexpected retries: %v", keys)
			}
		})
	}
}
