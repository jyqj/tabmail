package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// convergenceRaw is a well-formed plain-text message exercising every shared
// extraction surface: subject, the full 9-header allowlist (including the
// thread headers) and an OTP-bearing body.
const convergenceRaw = "From: sender@example.test\r\n" +
	"To: recipient@example.test\r\n" +
	"Cc: copied@example.test\r\n" +
	"Subject: login\r\n" +
	"Date: Mon, 29 Sep 2026 10:00:00 +0000\r\n" +
	"Message-Id: <child@example.test>\r\n" +
	"In-Reply-To: <parent@example.test>\r\n" +
	"References: <root@example.test> <parent@example.test>\r\n" +
	"Reply-To: noreply@example.test\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Your verification code: 482913\r\n"

// badMIMERaw hard-fails the shared bounded parse (see the fixture note in
// internal/mailcontent/parser_test.go): both paths must still store the raw
// bytes and deliver, minus the extracted fields.
const badMIMERaw = "Subject broken no colon\r\n\r\nbody"

func TestParseEnvelopeContentSharedKernel(t *testing.T) {
	c := parseEnvelopeContent(zerolog.Nop(), []byte(convergenceRaw))
	if c.subject != "login" {
		t.Fatalf("subject %q", c.subject)
	}
	hm := map[string]string{}
	if err := json.Unmarshal(c.headers, &hm); err != nil {
		t.Fatal(err)
	}
	for _, key := range ingestHeaders {
		if hm[key] == "" {
			t.Fatalf("header %q missing from shared extraction: %v", key, hm)
		}
	}
	if len(hm) != len(ingestHeaders) {
		t.Fatalf("unexpected extra headers: %v", hm)
	}

	bad := parseEnvelopeContent(zerolog.Nop(), []byte(badMIMERaw))
	if bad.env != nil || bad.subject != "" || bad.headers != nil {
		t.Fatalf("bad MIME must degrade to raw-only: %+v", bad)
	}

	oversize := parseEnvelopeContent(zerolog.Nop(), bytes.Repeat([]byte("x"), int(mailcontent.MaxBytes)+1))
	if oversize.env != nil || oversize.subject != "" || oversize.headers != nil {
		t.Fatalf("oversized source must degrade to raw-only: %+v", oversize)
	}

	// 513 attachment parts exceeds mailcontent's 512-part limit; the ingest path
	// must degrade identically instead of extracting from the flood.
	flood := parseEnvelopeContent(zerolog.Nop(), partFloodRaw(513))
	if flood.env != nil || flood.headers != nil {
		t.Fatalf("part flood must degrade to raw-only: %+v", flood)
	}
}

func partFloodRaw(n int) []byte {
	var b bytes.Buffer
	b.WriteString("From: sender@example.test\r\nSubject: flood\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "--B\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=p%d.txt\r\n\r\npart\r\n", i)
	}
	b.WriteString("--B--\r\n")
	return b.Bytes()
}

// newConvergenceFixture seeds one tenant/zone/route with two existing mailboxes
// and returns one immediate and one durable service over the same store, so the
// two delivery paths can be compared on identical input.
func newConvergenceFixture(t *testing.T) (*testutil.FakeStore, *testutil.MemoryObjectStore, *Service, *Service) {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID: planID, Name: "converge-test", MaxDomains: 10, MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000, MaxMessageBytes: 1024 * 1024, RetentionHours: 24,
		RPMLimit: 1000, DailyQuota: 1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: zoneID, RouteType: models.RouteExact, MatchValue: "mail.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})
	for _, local := range []string{"alice", "bob"} {
		st.SeedMailbox(&models.Mailbox{
			ID: uuid.New(), TenantID: tenantID, ZoneID: zoneID, LocalPart: local,
			ResolvedDomain: "mail.test", FullAddress: local + "@mail.test",
			AccessMode: models.AccessPublic, CreatedAt: time.Now(),
		})
	}

	immediate := NewService(st, obj, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop())
	durable := NewService(st, obj, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: true, BatchSize: 10}, zerolog.Nop())
	return st, obj, immediate, durable
}

func fetchSingleMessage(t *testing.T, st *testutil.FakeStore, addr string) *models.Message {
	t.Helper()
	mb, err := st.GetMailboxByAddress(context.Background(), addr)
	if err != nil || mb == nil {
		t.Fatalf("mailbox %s: %v", addr, err)
	}
	msgs, total, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil || total != 1 || len(msgs) != 1 {
		t.Fatalf("messages for %s: total=%d len=%d err=%v", addr, total, len(msgs), err)
	}
	return msgs[0]
}

// TestBothPathsStoreIdenticalHeadersAndOTP locks the convergence contract on
// the normal path: the immediate (Durable=false) and durable paths must extract
// identical subject, header set and OTP signal from the same bytes.
func TestBothPathsStoreIdenticalHeadersAndOTP(t *testing.T) {
	st, _, immediate, durable := newConvergenceFixture(t)
	ctx := context.Background()

	res, err := immediate.Accept(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(convergenceRaw))
	if err != nil || res.Delivered != 1 {
		t.Fatalf("immediate path: %#v %v", res, err)
	}
	queued, err := durable.Accept(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"bob@mail.test"},
	}, []byte(convergenceRaw))
	if err != nil || !queued.Queued {
		t.Fatalf("durable path: %#v %v", queued, err)
	}
	durable.ProcessBatch(ctx)

	imm := fetchSingleMessage(t, st, "alice@mail.test")
	dur := fetchSingleMessage(t, st, "bob@mail.test")
	if imm.Subject != "login" || dur.Subject != "login" {
		t.Fatalf("subjects %q / %q", imm.Subject, dur.Subject)
	}
	if !bytes.Equal(imm.HeadersJSON, dur.HeadersJSON) {
		t.Fatalf("header extraction diverged:\n%s\n%s", imm.HeadersJSON, dur.HeadersJSON)
	}
	hm := map[string]string{}
	if err := json.Unmarshal(dur.HeadersJSON, &hm); err != nil {
		t.Fatal(err)
	}
	if hm["In-Reply-To"] != "<parent@example.test>" || hm["References"] != "<root@example.test> <parent@example.test>" {
		t.Fatalf("thread headers missing: %v", hm)
	}
	if imm.OTPCode == "" || imm.OTPCode != dur.OTPCode || imm.OTPConfidence != dur.OTPConfidence {
		t.Fatalf("OTP extraction diverged: %q/%v vs %q/%v",
			imm.OTPCode, imm.OTPConfidence, dur.OTPCode, dur.OTPConfidence)
	}
}

// TestBothPathsDegradeToRawOnlyOnUnusableMIME locks the degraded contract: a
// message whose MIME cannot be parsed is still delivered by both paths (raw is
// authoritative), with empty extracted fields — never bounced or dropped.
func TestBothPathsDegradeToRawOnlyOnUnusableMIME(t *testing.T) {
	st, _, immediate, durable := newConvergenceFixture(t)
	ctx := context.Background()

	res, err := immediate.Accept(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(badMIMERaw))
	if err != nil || res.Delivered != 1 {
		t.Fatalf("immediate path must store raw: %#v %v", res, err)
	}
	queued, err := durable.Accept(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"bob@mail.test"},
	}, []byte(badMIMERaw))
	if err != nil || !queued.Queued {
		t.Fatalf("durable path must queue raw: %#v %v", queued, err)
	}
	durable.ProcessBatch(ctx)

	for _, addr := range []string{"alice@mail.test", "bob@mail.test"} {
		m := fetchSingleMessage(t, st, addr)
		if m.Subject != "" || len(m.HeadersJSON) != 0 || m.OTPCode != "" {
			t.Fatalf("%s: degraded message carries extracted fields: subject=%q headers=%s otp=%q",
				addr, m.Subject, m.HeadersJSON, m.OTPCode)
		}
	}
}
