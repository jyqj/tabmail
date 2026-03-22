package smtp_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	smtpsrv "tabmail/internal/smtp"
	"tabmail/internal/testutil"
)

func TestSMTPMessageLifecycle(t *testing.T) {
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "test",
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              1000,
		DailyQuota:            1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=test",
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneID,
		RouteType:         models.RouteExact,
		MatchValue:        "mail.test",
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessPublic,
	})

	addr := freeAddr(t)
	srv := smtpsrv.NewServer(config.SMTP{
		Addr:            addr,
		Domain:          "mx.mail.test",
		MaxRecipients:   10,
		MaxMessageBytes: 1024 * 1024,
		Timeout:         5 * time.Second,
		DefaultAccept:   true,
		DefaultStore:    true,
	}, 24, st, obj, resolver.New(st, policy.NamingFull, true), realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()), models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Start(ctx)
	}()
	waitTCP(t, addr)
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	expectCode(t, r, "220")
	sendLine(t, conn, "HELO localhost")
	expectCode(t, r, "250")
	sendLine(t, conn, "MAIL FROM:<sender@example.org>")
	expectCode(t, r, "250")
	sendLine(t, conn, "RCPT TO:<user@mail.test>")
	expectCode(t, r, "250")
	sendLine(t, conn, "DATA")
	expectCode(t, r, "354")
	sendLine(t, conn, "Subject: hello")
	sendLine(t, conn, "From: sender@example.org")
	sendLine(t, conn, "To: user@mail.test")
	sendLine(t, conn, "")
	sendLine(t, conn, "hello smtp")
	sendLine(t, conn, ".")
	expectCode(t, r, "250")
	sendLine(t, conn, "QUIT")

	mb, err := st.GetMailboxByAddress(context.Background(), "user@mail.test")
	if err != nil {
		t.Fatal(err)
	}
	if mb == nil {
		t.Fatal("expected mailbox to be auto-created")
	}
	msgs, total, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(msgs) != 1 {
		t.Fatalf("expected 1 message, total=%d len=%d", total, len(msgs))
	}
	if msgs[0].Subject != "hello" {
		t.Fatalf("unexpected subject: %q", msgs[0].Subject)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("smtp server did not start on %s", addr)
}

func sendLine(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
		t.Fatal(err)
	}
}

func expectCode(t *testing.T, r *bufio.Reader, prefix string) {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("expected %s, got %q", prefix, line)
	}
}
