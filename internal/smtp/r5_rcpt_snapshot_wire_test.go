package smtp

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// Real TCP SMTP observes DATA after a successful RCPT. These cases do not
// substitute a directly supplied resolver.Result for the session's own cache.
func TestContinueSMTPCachedRCPTRechecksBeforeDATA(t *testing.T) {
	for _, name := range []string{"valid snapshot", "zone invalidated", "mailbox expires"} {
		t.Run(name, func(t *testing.T) {
			st, obj := testutil.NewFakeStore(), testutil.NewMemoryObjectStore()
			tenant, plan := uuid.New(), uuid.New()
			st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: 1024, MaxMessagesPerMailbox: 100, RetentionHours: 24})
			st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
			zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true}
			st.SeedZone(zone)
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone.ID, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
			if name == "mailbox expires" {
				expires := time.Now().Add(2 * time.Second)
				mb.ExpiresAt = &expires
			}
			st.SeedMailbox(mb)
			rv := resolver.New(st, policy.NamingFull, true)
			svc := ingest.NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{}, zerolog.Nop())
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
			r5CloudWire(t, conn, r, "EHLO fixture.example.test", "250")
			r5CloudWire(t, conn, r, "MAIL FROM:<sender@example.test>", "250")
			r5CloudWire(t, conn, r, "RCPT TO:<reader@mail.test>", "250")
			if name == "zone invalidated" {
				changed := *zone
				changed.MXVerified = false
				st.SeedZone(&changed)
				rv.InvalidateZone(zone.Domain)
			} else if name == "mailbox expires" {
				time.Sleep(time.Until(*mb.ExpiresAt) + time.Millisecond)
			}
			r5CloudWire(t, conn, r, "DATA", "354")
			code, want := "451", 0
			if name == "valid snapshot" {
				code, want = "250", 1
			}
			r5CloudWire(t, conn, r, "Subject: snapshot wire\r\n\r\nowned synthetic body\r\n.", code)
			_, count, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
			if err != nil || count != want || obj.Count() != want {
				t.Errorf("wrong DATA effects: messages=%d objects=%d error=%v, want=%d", count, obj.Count(), err, want)
			}
		})
	}
}
