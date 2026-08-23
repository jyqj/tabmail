package postgres

// DB-backed tests for the postgres store. They are skipped unless
// TABMAIL_TEST_DATABASE_URL (or DATABASE_URL) points at a reachable Postgres
// server; CI provides one as a service container. Each test creates and drops
// its own throwaway database, so tests never share or leak state.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/config"
	"tabmail/internal/models"
)

func newTestStore(t *testing.T) *PgStore {
	t.Helper()
	dsn := os.Getenv("TABMAIL_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TABMAIL_TEST_DATABASE_URL or DATABASE_URL to run postgres integration tests")
	}

	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("integration tests need a URL-style DSN (postgres://...), got parse error %v", err)
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	dbName := "tabmail_it_" + hex.EncodeToString(buf)
	quoted := pgx.Identifier{dbName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database %s: %v", dbName, err)
		}
		_ = admin.Close(cleanupCtx)
	})

	parsed.Path = "/" + dbName
	st, err := New(ctx, config.DB{
		DSN:             parsed.String(),
		MaxOpenConns:    4,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("initialize store against test database: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seedTenant(t *testing.T, st *PgStore) *models.Tenant {
	t.Helper()
	ctx := context.Background()
	plan := &models.Plan{
		Name:                  "it-plan-" + uuid.NewString(),
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              600,
		DailyQuota:            5000,
	}
	if err := st.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	tenant := &models.Tenant{Name: "it-tenant-" + uuid.NewString(), PlanID: plan.ID}
	if err := st.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return tenant
}

func seedZone(t *testing.T, st *PgStore, tenantID uuid.UUID) *models.DomainZone {
	t.Helper()
	zone := &models.DomainZone{
		TenantID:   tenantID,
		Domain:     uuid.NewString() + ".mail.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=it",
	}
	if err := st.CreateZone(context.Background(), zone); err != nil {
		t.Fatalf("create zone: %v", err)
	}
	return zone
}

func seedMailbox(t *testing.T, st *PgStore, tenantID, zoneID uuid.UUID, domain string) *models.Mailbox {
	t.Helper()
	local := "u" + hex.EncodeToString([]byte(uuid.NewString()[:6]))
	mb := &models.Mailbox{
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      local,
		ResolvedDomain: domain,
		FullAddress:    local + "@" + domain,
		AccessMode:     models.AccessPublic,
	}
	if err := st.CreateMailbox(context.Background(), mb); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	return mb
}

func seedMessage(t *testing.T, st *PgStore, mb *models.Mailbox, subject string) *models.Message {
	t.Helper()
	msg := &models.Message{
		TenantID:   mb.TenantID,
		MailboxID:  mb.ID,
		ZoneID:     mb.ZoneID,
		Sender:     "sender@example.org",
		Recipients: []string{mb.FullAddress},
		Subject:    subject,
		Size:       128,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	if err := st.CreateMessage(context.Background(), msg); err != nil {
		t.Fatalf("create message: %v", err)
	}
	return msg
}

func TestIntegrationTenantLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, st)

	got, err := st.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != tenant.Name || got.PlanID != tenant.PlanID {
		t.Fatalf("GetTenant mismatch: %#v", got)
	}

	// schema.sql seeds a bootstrap tenant, so assert membership, not count.
	all, err := st.ListTenants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range all {
		if item.ID == tenant.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tenant %s in ListTenants, got %d others", tenant.ID, len(all))
	}

	cfg, err := st.EffectiveConfig(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionHours != 24 || cfg.DailyQuota != 5000 {
		t.Fatalf("expected plan values in effective config, got %#v", cfg)
	}

	retention := 48
	if err := st.UpsertOverride(ctx, &models.TenantOverride{
		TenantID:       tenant.ID,
		RetentionHours: &retention,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err = st.EffectiveConfig(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionHours != 48 {
		t.Fatalf("expected override retention 48, got %d", cfg.RetentionHours)
	}
	if cfg.DailyQuota != 5000 {
		t.Fatalf("expected non-overridden fields to fall back to plan, got %#v", cfg)
	}

	if err := st.DeleteTenant(ctx, tenant.ID); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected tenant deleted, got %#v", got)
	}
}

func TestIntegrationZoneLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, st)
	zone := seedZone(t, st, tenant.ID)

	got, err := st.GetZone(ctx, zone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Domain != zone.Domain || !got.IsVerified {
		t.Fatalf("GetZone mismatch: %#v", got)
	}

	byDomain, err := st.GetZoneByDomain(ctx, zone.Domain)
	if err != nil {
		t.Fatal(err)
	}
	if byDomain == nil || byDomain.ID != zone.ID {
		t.Fatalf("GetZoneByDomain mismatch: %#v", byDomain)
	}

	zones, err := st.ListZones(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(zones))
	}

	zone.IsVerified = false
	zone.MXVerified = false
	if err := st.UpdateZone(ctx, zone); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetZone(ctx, zone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsVerified || got.MXVerified {
		t.Fatalf("expected verification cleared, got %#v", got)
	}

	n, err := st.CountZones(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected zone count 1, got %d", n)
	}

	if err := st.DeleteZone(ctx, zone.ID); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetZone(ctx, zone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected zone deleted, got %#v", got)
	}
}

func TestIntegrationMailboxLifecycleAndTenantScoping(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, st)
	otherTenant := seedTenant(t, st)
	zone := seedZone(t, st, tenant.ID)
	mb := seedMailbox(t, st, tenant.ID, zone.ID, zone.Domain)

	got, err := st.GetMailboxByAddress(ctx, mb.FullAddress)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != mb.ID {
		t.Fatalf("GetMailboxByAddress mismatch: %#v", got)
	}

	items, total, err := st.ListMailboxes(ctx, tenant.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 mailbox, total=%d len=%d", total, len(items))
	}

	// The tenant-scoped view must report other tenants' rows as missing.
	scoped, err := st.ForTenant(otherTenant.ID).GetMailboxByAddress(ctx, mb.FullAddress)
	if err != nil {
		t.Fatal(err)
	}
	if scoped != nil {
		t.Fatalf("expected cross-tenant lookup to miss, got %#v", scoped)
	}
	scoped, err = st.ForTenant(tenant.ID).GetMailboxByAddress(ctx, mb.FullAddress)
	if err != nil {
		t.Fatal(err)
	}
	if scoped == nil || scoped.ID != mb.ID {
		t.Fatalf("expected same-tenant lookup to hit, got %#v", scoped)
	}

	if err := st.DeleteMailbox(ctx, mb.ID); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetMailboxByAddress(ctx, mb.FullAddress)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected mailbox deleted, got %#v", got)
	}
}

func TestIntegrationMessageLifecycleAndPagination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, st)
	zone := seedZone(t, st, tenant.ID)
	mb := seedMailbox(t, st, tenant.ID, zone.ID, zone.Domain)

	const totalMessages = 7
	created := make([]*models.Message, 0, totalMessages)
	for i := range totalMessages {
		created = append(created, seedMessage(t, st, mb, fmt.Sprintf("subject-%d", i)))
	}

	items, total, err := st.ListMessages(ctx, mb.ID, models.Page{Page: 1, PerPage: 3})
	if err != nil {
		t.Fatal(err)
	}
	if total != totalMessages || len(items) != 3 {
		t.Fatalf("expected total=%d page len=3, got total=%d len=%d", totalMessages, total, len(items))
	}
	for i := 1; i < len(items); i++ {
		if items[i].ReceivedAt.After(items[i-1].ReceivedAt) {
			t.Fatalf("expected received_at DESC ordering, got %v after %v",
				items[i].ReceivedAt, items[i-1].ReceivedAt)
		}
	}

	// Keyset pagination must walk every message exactly once.
	seen := map[uuid.UUID]int{}
	var cursor *models.MessageCursor
	for {
		page, err := st.ListMessagesKeyset(ctx, mb.ID, cursor, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, m := range page {
			seen[m.ID]++
		}
		last := page[len(page)-1]
		cursor = &models.MessageCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}
	}
	if len(seen) != totalMessages {
		t.Fatalf("keyset walk visited %d unique messages, want %d", len(seen), totalMessages)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("keyset walk visited %s %d times", id, n)
		}
	}

	// Tenant-scoped message reads must miss for foreign tenants.
	other := seedTenant(t, st)
	msg, err := st.ForTenant(other.ID).GetMessage(ctx, created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if msg != nil {
		t.Fatalf("expected cross-tenant message lookup to miss, got %#v", msg)
	}

	if err := st.MarkSeen(ctx, created[0].ID); err != nil {
		t.Fatal(err)
	}
	msg, err = st.GetMessage(ctx, created[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || !msg.Seen {
		t.Fatalf("expected message marked seen, got %#v", msg)
	}

	if err := st.DeleteMessage(ctx, created[0].ID); err != nil {
		t.Fatal(err)
	}
	count, err := st.CountMessages(ctx, mb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != totalMessages-1 {
		t.Fatalf("expected message_count %d after delete, got %d", totalMessages-1, count)
	}

	if err := st.PurgeMailbox(ctx, mb.ID); err != nil {
		t.Fatal(err)
	}
	count, err = st.CountMessages(ctx, mb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected message_count 0 after purge, got %d", count)
	}
}

// CreateMessageWithQuota is the SMTP delivery write path; before these tests
// its quota short-circuit was only exercised against the in-memory FakeStore.
func TestIntegrationCreateMessageWithQuota(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	tenant := seedTenant(t, st)
	zone := seedZone(t, st, tenant.ID)
	mb := seedMailbox(t, st, tenant.ID, zone.ID, zone.Domain)

	newMessage := func() *models.Message {
		return &models.Message{
			TenantID:   mb.TenantID,
			MailboxID:  mb.ID,
			ZoneID:     mb.ZoneID,
			Sender:     "sender@example.org",
			Recipients: []string{mb.FullAddress},
			ExpiresAt:  time.Now().Add(time.Hour),
		}
	}

	const maxMessages = 2
	for i := range maxMessages {
		ok, err := st.CreateMessageWithQuota(ctx, newMessage(), maxMessages)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("expected message %d to fit under quota", i)
		}
	}
	ok, err := st.CreateMessageWithQuota(ctx, newMessage(), maxMessages)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected quota to reject the third message")
	}

	count, err := st.CountMessages(ctx, mb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != maxMessages {
		t.Fatalf("expected message_count %d, got %d", maxMessages, count)
	}
	all, err := st.CountAllMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all != maxMessages {
		t.Fatalf("expected %d stored rows, got %d", maxMessages, all)
	}
}

func TestIntegrationClaimIngestJobsLease(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := range 2 {
		if err := st.CreateIngestJob(ctx, &models.IngestJob{
			Source:       "smtp",
			MailFrom:     "sender@example.org",
			Recipients:   []string{fmt.Sprintf("rcpt%d@mail.test", i)},
			RawObjectKey: fmt.Sprintf("sha256/ab/job-%d.eml", i),
			// In the past so claiming at `now` sees the jobs as due.
			NextAttemptAt: now.Add(-time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	claimed, err := st.ClaimIngestJobs(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 2 {
		t.Fatalf("expected to claim 2 jobs, got %d", len(claimed))
	}
	for _, job := range claimed {
		if job.State != "processing" || job.Attempts != 1 {
			t.Fatalf("expected processing state and 1 attempt, got %#v", job)
		}
		if job.LeaseUntil == nil || !job.LeaseUntil.After(now) {
			t.Fatalf("expected a future lease, got %#v", job.LeaseUntil)
		}
	}

	// While leases are held nothing is claimable.
	again, err := st.ClaimIngestJobs(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("expected leased jobs to be unclaimable, got %d", len(again))
	}

	if err := st.MarkIngestJobDone(ctx, claimed[0].ID); err != nil {
		t.Fatal(err)
	}
	retryAt := now.Add(time.Minute)
	if err := st.MarkIngestJobRetry(ctx, claimed[1].ID, "boom", retryAt, false); err != nil {
		t.Fatal(err)
	}

	// Before the retry time nothing is due; after it only the retry job is.
	due, err := st.ClaimIngestJobs(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("expected nothing claimable before retry time, got %d", len(due))
	}
	due, err = st.ClaimIngestJobs(ctx, retryAt.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != claimed[1].ID {
		t.Fatalf("expected only the retry job to be claimable, got %#v", due)
	}
	if due[0].Attempts != 2 {
		t.Fatalf("expected second attempt, got %d", due[0].Attempts)
	}
}
