package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// coreRaw is a minimal deliverable envelope body shared by the delivery-kernel
// equivalence tests (no OTP signal, plain subject).
const coreRaw = "Subject: core\r\n\r\nhello kernel"

// newCoreFixture seeds one tenant/zone/route with two pre-existing mailboxes
// (alice/bob) and returns paired immediate/durable services over the same
// store. seedPlan=false leaves the tenant pointing at an unseeded plan so
// EffectiveConfig degrades to (nil, nil) — the tenant-config failure path.
func newCoreFixture(t *testing.T, mutatePlan func(*models.Plan), seedPlan bool, mbOverride *int) (*testutil.FakeStore, *testutil.MemoryObjectStore, *Service, *Service, *models.Mailbox, *models.Mailbox) {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	plan := &models.Plan{ID: uuid.New(), Name: "core-test", MaxDomains: 10, MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000, MaxMessageBytes: 1024 * 1024, RetentionHours: 24,
		RPMLimit: 1000, DailyQuota: 1000}
	if mutatePlan != nil {
		mutatePlan(plan)
	}
	tenantPlanID := plan.ID
	if !seedPlan {
		tenantPlanID = uuid.New()
	}
	if seedPlan {
		st.SeedPlan(plan)
	}
	tenantID := uuid.New()
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "core-tenant", PlanID: tenantPlanID})
	zoneID := uuid.New()
	st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: zoneID, RouteType: models.RouteExact,
		MatchValue: "mail.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})

	var alice, bob *models.Mailbox
	for _, e := range []struct {
		local string
		mb    **models.Mailbox
	}{{"alice", &alice}, {"bob", &bob}} {
		m := &models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zoneID, LocalPart: e.local,
			ResolvedDomain: "mail.test", FullAddress: e.local + "@mail.test", AccessMode: models.AccessPublic,
			RetentionHoursOverride: mbOverride, CreatedAt: time.Now()}
		st.SeedMailbox(m)
		*e.mb = m
	}

	immediate := NewService(st, obj, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop())
	durable := NewService(st, obj, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: true, BatchSize: 10}, zerolog.Nop())
	return st, obj, immediate, durable, alice, bob
}

// acceptCoreReceipt durably accepts one envelope and returns the receipt.
func acceptCoreReceipt(t *testing.T, svc *Service, st *testutil.FakeStore, rcpt string) *models.IngestJob {
	t.Helper()
	res, err := svc.Accept(context.Background(), Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{rcpt},
	}, []byte(coreRaw))
	if err != nil || !res.Queued {
		t.Fatalf("durable accept: %#v %v", res, err)
	}
	jobs, _, err := st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("receipts: len=%d err=%v", len(jobs), err)
	}
	return jobs[0]
}

func mailboxMessageCount(t *testing.T, st *testutil.FakeStore, addr string) int {
	t.Helper()
	mb, err := st.GetMailboxByAddress(context.Background(), addr)
	if err != nil || mb == nil {
		t.Fatalf("mailbox %s: %v", addr, err)
	}
	_, total, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// TestPrepareDeliveryRetentionPrecedence locks the shared kernel's retention
// and Message-construction contract directly: mailbox > route > tenant >
// fallback precedence, the legacy non-nil-tenant-0 NULL expiry, and the
// constructed identity fields.
func TestPrepareDeliveryRetentionPrecedence(t *testing.T) {
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()
	svc := NewService(st, obj, resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop())

	tenantID := uuid.New()
	mbOverride, routeOverride := 6, 48
	zoneID := uuid.New()
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zoneID,
		ResolvedDomain: "mail.test", FullAddress: "u@mail.test", RetentionHoursOverride: &mbOverride}
	route := &models.DomainRoute{ID: uuid.New(), RetentionHoursOverride: &routeOverride}
	cfg := func(hours int) *models.EffectiveConfig {
		return &models.EffectiveConfig{RetentionHours: hours, MaxMessagesPerMailbox: 100,
			MaxMessageBytes: 1024, DailyQuota: 100}
	}
	at := time.Now()
	in := deliveryInput{
		pol: &models.SMTPPolicy{DefaultAccept: true, DefaultStore: true},
		mb:  mb, rcpt: "user+tag@mail.test", raw: []byte(coreRaw), objKey: "core-key",
		from: "sender@example.test", at: at,
		cfgCache: map[uuid.UUID]*models.EffectiveConfig{tenantID: cfg(24)},
		routeFn:  func(context.Context) (*models.DomainRoute, error) { return route, nil },
	}
	expectHours := func(plan *deliveryPlan, pf *deliveryFailure, want int) {
		t.Helper()
		if pf != nil {
			t.Fatalf("unexpected failure: %+v", pf)
		}
		if plan == nil || plan.msg == nil {
			t.Fatal("no plan")
		}
		if want == 0 {
			if plan.msg.ExpiresAt != nil {
				t.Fatalf("want NULL expiry, got %v", plan.msg.ExpiresAt)
			}
			return
		}
		if plan.msg.ExpiresAt == nil {
			t.Fatalf("want ~%dh expiry, got nil", want)
		}
		if d := plan.msg.ExpiresAt.Sub(at); d < time.Duration(want-1)*time.Hour || d > time.Duration(want+1)*time.Hour {
			t.Fatalf("retention delta %v, want ~%dh", d, want)
		}
	}

	plan, pf := svc.prepareDelivery(context.Background(), in)
	expectHours(plan, pf, 6) // mailbox > route > tenant
	if plan.msg.TenantID != tenantID || plan.msg.MailboxID != mb.ID || plan.msg.ZoneID != zoneID {
		t.Fatalf("identity fields: tenant=%s mailbox=%s zone=%s", plan.msg.TenantID, plan.msg.MailboxID, plan.msg.ZoneID)
	}
	if got := plan.msg.Recipients; len(got) != 1 || got[0] != "user+tag@mail.test" {
		t.Fatalf("recipients must keep the envelope address, got %v", got)
	}
	if plan.msg.Sender != "sender@example.test" || plan.msg.Size != int64(len(coreRaw)) || plan.msg.RawObjectKey != "core-key" {
		t.Fatalf("message fields: %#v", plan.msg)
	}

	in.mb.RetentionHoursOverride = nil
	plan, pf = svc.prepareDelivery(context.Background(), in)
	expectHours(plan, pf, 48) // route > tenant

	in.routeFn = func(context.Context) (*models.DomainRoute, error) { return nil, nil }
	plan, pf = svc.prepareDelivery(context.Background(), in)
	expectHours(plan, pf, 24) // tenant level

	in.cfgCache[tenantID] = cfg(0)
	plan, pf = svc.prepareDelivery(context.Background(), in)
	expectHours(plan, pf, 0) // legacy: non-nil tenant 0 = immediate expiry (NULL)

	// size gate fires on the cached config without touching routeFn
	limited := cfg(4)
	limited.MaxMessageBytes = 4
	in.cfgCache[tenantID] = limited
	plan, pf = svc.prepareDelivery(context.Background(), in)
	if plan != nil || pf == nil || !pf.terminal() || pf.code != rejectMaxMessageBytes {
		t.Fatalf("size gate: plan=%v pf=%+v", plan, pf)
	}
}

// TestDeliveryCoreBothPathsEquivalentNormalRetention pins cross-path
// equivalence on the normal path: the same bytes delivered through the
// immediate and durable shells produce the same subject and the same
// mailbox-override retention window.
func TestDeliveryCoreBothPathsEquivalentNormalRetention(t *testing.T) {
	ov := 6
	st, _, immediate, durable, _, _ := newCoreFixture(t, nil, true, &ov)
	ctx := context.Background()

	res, err := immediate.Accept(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(coreRaw))
	if err != nil || res.Delivered != 1 {
		t.Fatalf("immediate: %#v %v", res, err)
	}
	acceptCoreReceipt(t, durable, st, "bob@mail.test")
	durable.ProcessBatch(ctx)

	imm := fetchSingleMessage(t, st, "alice@mail.test")
	dur := fetchSingleMessage(t, st, "bob@mail.test")
	if imm.Subject != dur.Subject || imm.Subject != "core" {
		t.Fatalf("subjects diverged: %q / %q", imm.Subject, dur.Subject)
	}
	for _, e := range []struct {
		name string
		m    *models.Message
	}{{"immediate", imm}, {"durable", dur}} {
		if e.m.ExpiresAt == nil {
			t.Fatalf("%s: nil expiry despite 6h override", e.name)
		}
		if d := e.m.ExpiresAt.Sub(e.m.ReceivedAt); d < 5*time.Hour || d > 7*time.Hour {
			t.Fatalf("%s: retention delta %v, want ~6h (mailbox override not shared)", e.name, d)
		}
	}
}

// TestDeliveryCoreBothPathsRejectStorePolicyDiscard pins the store-policy
// rejection classification on both paths: the immediate shell rejects the
// recipient terminally, the durable shell holds the accepted bytes for review
// (never discards) with the historical permanentIngress message.
func TestDeliveryCoreBothPathsRejectStorePolicyDiscard(t *testing.T) {
	st, obj, immediate, durable, _, _ := newCoreFixture(t, nil, true, nil)
	immediate.defaultPolicy.DefaultStore = false
	durable.defaultPolicy.DefaultStore = false
	ctx := context.Background()

	outcomes, err := immediate.deliver(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(coreRaw))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != RecipientRejected || outcomes[0].Reason != rejectStorePolicyDiscard {
		t.Fatalf("immediate: %#v", outcomes)
	}
	if n := mailboxMessageCount(t, st, "alice@mail.test"); n != 0 {
		t.Fatalf("immediate stored %d messages", n)
	}

	j := acceptCoreReceipt(t, durable, st, "bob@mail.test")
	durable.ProcessBatch(ctx)
	jobs, _, err := st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || len(jobs) != 1 || jobs[0].State != "dead" {
		t.Fatalf("durable must hold permanently rejected receipt: %#v %v", jobs, err)
	}
	targets, err := st.ListIngressTargets(ctx, j.ID)
	if err != nil || len(targets) != 1 || targets[0].State != "held" ||
		targets[0].LastError != "storage policy blocks delivery; accepted bytes retained" {
		t.Fatalf("durable target: %#v %v", targets, err)
	}
	if n := mailboxMessageCount(t, st, "bob@mail.test"); n != 0 {
		t.Fatalf("durable stored %d messages", n)
	}
	if exists, err := obj.Exists(ctx, j.RawObjectKey); err != nil || !exists {
		t.Fatal("held original lost")
	}
}

// TestDeliveryCoreBothPathsRejectOversize pins the size-gate classification on
// both paths: terminal rejection with the shared kernel code on the immediate
// shell, permanentIngress hold with the historical message on the durable
// shell — and no route lookup side effects for a size-rejected target.
func TestDeliveryCoreBothPathsRejectOversize(t *testing.T) {
	st, obj, immediate, durable, _, _ := newCoreFixture(t, func(p *models.Plan) { p.MaxMessageBytes = 8 }, true, nil)
	ctx := context.Background()

	outcomes, err := immediate.deliver(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(coreRaw))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != RecipientRejected || outcomes[0].Reason != rejectMaxMessageBytes {
		t.Fatalf("immediate: %#v", outcomes)
	}
	if n := mailboxMessageCount(t, st, "alice@mail.test"); n != 0 {
		t.Fatalf("immediate stored %d messages", n)
	}

	j := acceptCoreReceipt(t, durable, st, "bob@mail.test")
	durable.ProcessBatch(ctx)
	jobs, _, err := st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || len(jobs) != 1 || jobs[0].State != "dead" {
		t.Fatalf("durable must hold oversize receipt: %#v %v", jobs, err)
	}
	targets, err := st.ListIngressTargets(ctx, j.ID)
	if err != nil || len(targets) != 1 || targets[0].State != "held" ||
		targets[0].LastError != "tenant size limit exceeded; accepted bytes retained" {
		t.Fatalf("durable target: %#v %v", targets, err)
	}
	if n := mailboxMessageCount(t, st, "bob@mail.test"); n != 0 {
		t.Fatalf("durable stored %d messages", n)
	}
	if exists, err := obj.Exists(ctx, j.RawObjectKey); err != nil || !exists {
		t.Fatal("held original lost")
	}
}

// TestDeliveryCoreBothPathsRejectMissingTenantConfig pins the transient
// tenant-config failure on both paths: the immediate shell reports a
// per-recipient error (retryable, not a silent drop), the durable shell fails
// the target back to pending for replay — neither path accepts input the
// other rejects.
func TestDeliveryCoreBothPathsRejectMissingTenantConfig(t *testing.T) {
	st, _, immediate, durable, _, _ := newCoreFixture(t, nil, false, nil)
	ctx := context.Background()

	outcomes, err := immediate.deliver(ctx, Envelope{
		Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"alice@mail.test"},
	}, []byte(coreRaw))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != RecipientError || outcomes[0].Reason != stageTenantConfig {
		t.Fatalf("immediate: %#v", outcomes)
	}
	if n := mailboxMessageCount(t, st, "alice@mail.test"); n != 0 {
		t.Fatalf("immediate stored %d messages", n)
	}

	j := acceptCoreReceipt(t, durable, st, "bob@mail.test")
	durable.ProcessBatch(ctx)
	jobs, _, err := st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || len(jobs) != 1 {
		t.Fatalf("receipts: %#v %v", jobs, err)
	}
	if jobs[0].State != "retry" {
		t.Fatalf("transient config failure must retry, got %s", jobs[0].State)
	}
	targets, err := st.ListIngressTargets(ctx, j.ID)
	if err != nil || len(targets) != 1 || targets[0].State != "pending" ||
		targets[0].LastError != "tenant configuration unavailable" {
		t.Fatalf("durable target: %#v %v", targets, err)
	}
	if n := mailboxMessageCount(t, st, "bob@mail.test"); n != 0 {
		t.Fatalf("durable stored %d messages", n)
	}
}
