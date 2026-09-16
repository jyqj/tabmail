package postgres_test

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

type localSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	rcpt     map[string]int
	messages map[string][][]byte
	wg       sync.WaitGroup
}

func newLocalSMTP(t *testing.T) *localSMTP {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	s := &localSMTP{ln: ln, rcpt: map[string]int{}, messages: map[string][][]byte{}}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.serve(c) }()
		}
	}()
	t.Cleanup(func() { ln.Close(); s.wg.Wait() })
	return s
}
func (s *localSMTP) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	p := textproto.NewConn(c)
	_ = p.PrintfLine("220 loopback-only.test ESMTP")
	rcpt := ""
	for {
		line, e := p.ReadLine()
		if e != nil {
			return
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			_ = p.PrintfLine("250 loopback-only.test")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			_ = p.PrintfLine("250 sender accepted")
		case strings.HasPrefix(upper, "RCPT TO:"):
			rcpt = strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>")
			s.mu.Lock()
			s.rcpt[rcpt]++
			n := s.rcpt[rcpt]
			s.mu.Unlock()
			if strings.HasPrefix(rcpt, "bad@") {
				_ = p.PrintfLine("550 no such recipient")
			} else if strings.HasPrefix(rcpt, "temporary@") && n == 1 {
				_ = p.PrintfLine("450 try later")
			} else {
				_ = p.PrintfLine("250 recipient accepted")
			}
		case upper == "DATA":
			_ = p.PrintfLine("354 send message")
			data, e := p.ReadDotBytes()
			if e != nil {
				return
			}
			s.mu.Lock()
			s.messages[rcpt] = append(s.messages[rcpt], data)
			s.mu.Unlock()
			if strings.HasPrefix(rcpt, "uncertain@") {
				return
			}
			_ = p.PrintfLine("250 accepted")
		case upper == "QUIT":
			_ = p.PrintfLine("221 goodbye")
			return
		default:
			_ = p.PrintfLine("250 ok")
		}
	}
}
func waitJob(t *testing.T, f *companyFixture, id uuid.UUID) *models.OutboundJob {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		j, e := f.st.GetOutboundJob(context.Background(), id)
		must(t, e)
		if j.State == models.OutboundSent || j.State == models.OutboundFailed || j.State == models.OutboundDead {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker did not reach terminal state")
	return nil
}
func TestR3RealSMTPRecipientIsolationAndUncertainRecovery(t *testing.T) {
	f := seedCompany(t)
	smtp := newLocalSMTP(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := smtp.ln.Addr().(*net.TCPAddr).Port
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: port, RelayTLS: "none", MaxRetries: 3, RetryDelay: time.Millisecond, PollInterval: 5 * time.Millisecond, BatchSize: 10}, f.st, zerolog.Nop())
	svc.StartWorker(ctx)
	t.Cleanup(func() { cancel(); svc.Stop() })
	req := outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, ZoneID: f.zone.ID, From: f.personal.FullAddress, To: []string{"bad@recipient.test", "good@recipient.test", "temporary@recipient.test"}, BCC: []string{"hidden@recipient.test"}, Subject: "SMTP isolation", TextBody: "Actual MIME body", IdempotencyKey: "smtp-isolation"}
	j, e := svc.Submit(ctx, req)
	must(t, e)
	j = waitJob(t, f, j.ID)
	if j.State != models.OutboundFailed {
		t.Fatalf("partial permanent failure: %s %s", j.State, j.LastError)
	}
	rows, e := f.st.ListOutboundRecipients(ctx, f.tenant.ID, j.ID)
	must(t, e)
	for _, r := range rows {
		expected := "accepted"
		if strings.HasPrefix(r.Address, "bad@") {
			expected = "permanent"
		}
		if r.State != expected {
			t.Fatalf("%s: %s wanted %s", r.Address, r.State, expected)
		}
	}
	smtp.mu.Lock()
	counts := map[string]int{}
	for k, v := range smtp.rcpt {
		counts[k] = v
	}
	for _, b := range smtp.messages["good@recipient.test"] {
		if strings.Contains(string(b), "hidden@recipient.test") || strings.Contains(strings.ToLower(string(b)), "\nbcc:") {
			t.Error("BCC leaked into MIME")
		}
	}
	smtp.mu.Unlock()
	if counts["good@recipient.test"] != 1 || counts["hidden@recipient.test"] != 1 || counts["bad@recipient.test"] != 1 || counts["temporary@recipient.test"] != 2 {
		t.Fatalf("incorrect retry set: %v", counts)
	}
	req.To = []string{"uncertain@recipient.test"}
	req.BCC = nil
	req.IdempotencyKey = "smtp-uncertain"
	j, e = svc.Submit(ctx, req)
	must(t, e)
	j = waitJob(t, f, j.ID)
	if j.InFlightDomain == "" || !errors.Is(svc.ValidateJobAuthorization(ctx, j), store.ErrOutboundUncertain) {
		t.Fatal("uncertainty silently cleared")
	}
	// Reconcile only after test server evidence proves DATA was received. No new
	// SMTP submission is needed when every recipient is confirmed accepted.
	_, e = f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
	must(t, e)
	must(t, f.st.ReconcileOutbound(ctx, f.a, j.ID, j.UpdatedAt, []company.Recipient{{Address: "uncertain@recipient.test", State: "accepted"}}, "Test SMTP receipt proves full DATA acceptance"))
	reconciled, e := f.st.GetOutboundJob(ctx, j.ID)
	must(t, e)
	if reconciled.State != models.OutboundSent || reconciled.InFlightDomain != "" {
		t.Fatalf("reconciliation: %+v", reconciled)
	}
	smtp.mu.Lock()
	n := smtp.rcpt["uncertain@recipient.test"]
	smtp.mu.Unlock()
	if n != 1 {
		t.Fatalf("uncertain mail repeated %d times", n)
	}
	var audit string
	must(t, f.pool.QueryRow(ctx, `SELECT details::text FROM audit_log WHERE action='outbound.reconcile' AND resource_id=$1`, j.ID).Scan(&audit))
	if strings.Contains(audit, "uncertain@recipient.test") {
		t.Fatal("recipient leaked in general audit")
	}
}

func TestR3BootstrapIsAtomicAcrossRoles(t *testing.T) {
	f := seedCompany(t)
	start := make(chan struct{})
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, e := f.st.BootstrapCompanyAdmin(context.Background(), "bootstrap@company.test", strings.Repeat("hash", 20))
			results <- ok
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		must(t, e)
	}
	n := 0
	for ok := range results {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("bootstrap created %d times", n)
	}
	var count int
	must(t, f.pool.QueryRow(context.Background(), `SELECT count(*) FROM tenants WHERE name='bootstrap@company.test'`).Scan(&count))
	if count != 1 {
		t.Fatal("orphan bootstrap tenant")
	}
}
