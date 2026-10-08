package ingest

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

type rcptSnapshotStore struct {
	*testutil.FakeStore
	mailboxReads atomic.Int32
	zoneReads    atomic.Int32
	zoneError    error
	zoneObserved func(*models.DomainZone)
}

func (s *rcptSnapshotStore) GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error) {
	s.mailboxReads.Add(1)
	return s.FakeStore.GetMailboxByAddress(ctx, address)
}
func (s *rcptSnapshotStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	s.zoneReads.Add(1)
	if s.zoneError != nil {
		return nil, s.zoneError
	}
	z, err := s.FakeStore.GetZoneByDomain(ctx, domain)
	if s.zoneObserved != nil {
		s.zoneObserved(z)
	}
	return z, err
}
func (s *rcptSnapshotStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &models.EffectiveConfig{MaxMessagesPerMailbox: 100, MaxMessageBytes: 1024, RetentionHours: 24}, nil
}

type rcptSnapshotFixture struct {
	store   *rcptSnapshotStore
	objects *testutil.MemoryObjectStore
	rv      *resolver.Resolver
	svc     *Service
	zone    *models.DomainZone
	mailbox *models.Mailbox
}

func newRCPTSnapshotFixture(t *testing.T, subdomain bool, expires *time.Time, durable bool) *rcptSnapshotFixture {
	t.Helper()
	f := &rcptSnapshotFixture{store: &rcptSnapshotStore{FakeStore: testutil.NewFakeStore()}, objects: testutil.NewMemoryObjectStore()}
	f.zone = &models.DomainZone{ID: uuid.New(), TenantID: uuid.New(), Domain: "mail.test", IsVerified: true, MXVerified: true}
	f.store.SeedZone(f.zone)
	domain := f.zone.Domain
	if subdomain {
		domain = "sub." + domain
	}
	f.mailbox = &models.Mailbox{ID: uuid.New(), TenantID: f.zone.TenantID, ZoneID: f.zone.ID, LocalPart: "reader", ResolvedDomain: domain, FullAddress: "reader@" + domain, AccessMode: models.AccessPublic, ExpiresAt: expires}
	f.store.SeedMailbox(f.mailbox)
	f.rv = resolver.New(f.store, policy.NamingFull, true)
	f.svc = NewService(f.store, f.objects, f.rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: durable}, zerolog.Nop())
	return f
}
func (f *rcptSnapshotFixture) check(t *testing.T) *resolver.Result {
	t.Helper()
	r, err := f.rv.Check(context.Background(), f.mailbox.FullAddress)
	if err != nil || r == nil || r.Mailbox == nil || r.Zone == nil {
		t.Fatalf("actual RCPT check failed: %v %v", r, err)
	}
	return r
}
func (f *rcptSnapshotFixture) revokeZone() {
	z := *f.zone
	z.MXVerified = false
	f.store.SeedZone(&z)
}
func (f *rcptSnapshotFixture) insertChildZone() {
	z := *f.zone
	z.ID, z.Domain = uuid.New(), "sub.mail.test"
	f.store.SeedZone(&z)
}
func (f *rcptSnapshotFixture) accept(t *testing.T, result *resolver.Result, wantDelivery bool) {
	t.Helper()
	out, err := f.svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{f.mailbox.FullAddress}}, []byte("Subject: snapshot freshness\r\n\r\nowned synthetic original"), WithResolved(f.mailbox.FullAddress, result))
	_, count, readErr := f.store.ListMessages(context.Background(), f.mailbox.ID, models.Page{Page: 1, PerPage: 10})
	if readErr != nil {
		t.Fatal(readErr)
	}
	if wantDelivery {
		if err != nil || out.Delivered != 1 || out.Queued || count != 1 {
			t.Errorf("valid RCPT was not delivered: result=%+v error=%v messages=%d", out, err, count)
		}
	} else if err == nil || out.Delivered != 0 || out.Queued || count != 0 || f.objects.Count() != 0 {
		t.Errorf("stale RCPT acknowledged or retained content: result=%+v error=%v messages=%d objects=%d", out, err, count, f.objects.Count())
	}
}

// Actual Check -> WithResolved -> Accept, using a deterministic clock and the
// real resolver/cache/raw-object paths. Metadata is synthetic, not a claim of
// PostgreSQL atomic authorization or a change to the existing 15-second TTL.
func TestContinueRCPTSnapshotFreshness(t *testing.T) {
	for _, name := range []string{
		"fresh snapshot still avoids mailbox requery",
		"mailbox expired after RCPT", "mailbox exactly at expiry", "trusted result mailbox expiry",
		"zone TTL expired", "zone exactly at TTL", "RCPT does not restart existing zone TTL",
		"explicit zone invalidation", "invalidation read error", "new child zone invalidates parent result",
		"negative child TTL bounds refreshed parent", "durable acceptance still resolves current zone",
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var expires *time.Time
				if name == "mailbox expired after RCPT" || name == "mailbox exactly at expiry" || name == "trusted result mailbox expiry" {
					at := time.Now().Add(5 * time.Second)
					expires = &at
				}
				child := name == "new child zone invalidates parent result" || name == "negative child TTL bounds refreshed parent"
				f := newRCPTSnapshotFixture(t, child, expires, name == "durable acceptance still resolves current zone")
				r := f.check(t)
				if !r.Reusable() {
					t.Fatal("initial existing mailbox result should be reusable")
				}
				switch name {
				case "fresh snapshot still avoids mailbox requery":
					time.Sleep(14 * time.Second)
					f.accept(t, r, true)
					if f.store.mailboxReads.Load() != 1 || f.store.zoneReads.Load() != 1 {
						t.Error("valid snapshot lost the existing RCPT reuse fast path")
					}
					return
				case "mailbox expired after RCPT":
					time.Sleep(6 * time.Second)
				case "mailbox exactly at expiry":
					time.Sleep(5 * time.Second)
				case "trusted result mailbox expiry":
					r = &resolver.Result{Zone: r.Zone, Mailbox: r.Mailbox}
					time.Sleep(6 * time.Second)
				case "zone TTL expired":
					f.revokeZone()
					time.Sleep(16 * time.Second)
				case "zone exactly at TTL":
					f.revokeZone()
					time.Sleep(15 * time.Second)
				case "RCPT does not restart existing zone TTL":
					time.Sleep(14 * time.Second)
					r = f.check(t)
					f.revokeZone()
					time.Sleep(2 * time.Second)
				case "explicit zone invalidation", "durable acceptance still resolves current zone":
					f.revokeZone()
					f.rv.InvalidateZone(f.zone.Domain)
				case "invalidation read error":
					f.store.zoneError = errors.New("synthetic zone lookup unavailable")
					f.rv.InvalidateZone(f.zone.Domain)
				case "new child zone invalidates parent result":
					f.insertChildZone()
					f.rv.InvalidateZone("sub.mail.test")
				case "negative child TTL bounds refreshed parent":
					// The child miss expires at t=15, but this explicit parent
					// refresh lives until t=25. RCPT must inherit the earlier bound.
					time.Sleep(10 * time.Second)
					f.rv.InvalidateZone(f.zone.Domain)
					r = f.check(t)
					f.insertChildZone() // another writer missed local invalidation
					time.Sleep(6 * time.Second)
				}
				if name != "durable acceptance still resolves current zone" && r.Reusable() {
					t.Error("expired or invalidated result still advertises reuse")
				}
				f.accept(t, r, false)
			})
		})
	}
}

func TestContinueRCPTInflightInvalidationCannotStampOldZoneFresh(t *testing.T) {
	f := newRCPTSnapshotFixture(t, false, nil, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	f.store.zoneObserved = func(z *models.DomainZone) {
		if z != nil && first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}
	type result struct {
		value *resolver.Result
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, err := f.rv.Check(context.Background(), f.mailbox.FullAddress)
		done <- result{v, err}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		<-done
		t.Fatal("zone lookup did not reach its observed-old-value barrier")
	}
	f.revokeZone()
	f.rv.InvalidateZone(f.zone.Domain)
	close(release)
	got := <-done
	if got.err != nil || got.value == nil || !got.value.Zone.CanReceiveMessage() {
		t.Fatalf("the in-flight call must return its original observed value: %v %v", got.value, got.err)
	}
	if got.value.Reusable() {
		t.Error("old load was stamped with the post-invalidation generation")
	}
	f.accept(t, got.value, false)
}
