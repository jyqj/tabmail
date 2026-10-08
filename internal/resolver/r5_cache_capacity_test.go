package resolver

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

type progressCapacityResolverStore struct {
	*testutil.FakeStore
	reads map[string]int
}

func (s *progressCapacityResolverStore) GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error) {
	s.reads[domain]++
	return s.FakeStore.GetZoneByDomain(ctx, domain)
}

func TestProgressResolverBoundsUnknownRecipientCache(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		st := &progressCapacityResolverStore{FakeStore: testutil.NewFakeStore(), reads: map[string]int{}}
		rv := New(st, policy.NamingFull, true)
		for i := 0; i <= 4096; i++ {
			address := fmt.Sprintf("employee@unknown-%05d.test", i)
			if result, err := rv.Check(ctx, address); err != nil || result != nil {
				t.Fatalf("unknown recipient changed: result=%+v error=%v", result, err)
			}
		}
		// Recent negative hits still avoid queries. A cold evicted absence can
		// observe provisioning before the ordinary 15-second fallback expires.
		rv.Check(ctx, "employee@unknown-04096.test")
		if st.reads["unknown-04096.test"] != 1 {
			t.Fatal("recent negative cache entry was not reused")
		}
		zone := &models.DomainZone{ID: uuid.New(), TenantID: uuid.New(), Domain: "unknown-00000.test", IsVerified: true, MXVerified: true}
		mailbox := &models.Mailbox{ID: uuid.New(), TenantID: zone.TenantID, ZoneID: zone.ID, FullAddress: "employee@" + zone.Domain, LocalPart: "employee", ResolvedDomain: zone.Domain}
		st.SeedZone(zone)
		st.SeedMailbox(mailbox)
		result, err := rv.Check(ctx, mailbox.FullAddress)
		if err != nil || result == nil || result.Mailbox == nil || result.Mailbox.ID != mailbox.ID || st.reads[zone.Domain] != 2 {
			t.Fatalf("unbounded negative entry masked newly provisioned cold domain: result=%+v err=%v lookups=%d", result, err, st.reads[zone.Domain])
		}
		if count, err := st.CountAllMailboxes(ctx); err != nil || count != 1 {
			t.Fatalf("RCPT checks materialized unknown recipients: mailboxes=%d err=%v", count, err)
		}
	})
}
