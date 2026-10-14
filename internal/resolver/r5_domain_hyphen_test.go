package resolver

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestR5ASCIIIDNADomainResolvesProvisionedMailbox(t *testing.T) {
	st := testutil.NewFakeStore()
	tenantID, zoneID, mailboxID := uuid.New(), uuid.New(), uuid.New()
	st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "xn--bcher-kva.example", IsVerified: true, MXVerified: true})
	st.SeedMailbox(&models.Mailbox{
		ID: mailboxID, TenantID: tenantID, ZoneID: zoneID,
		LocalPart: "reader", ResolvedDomain: "xn--bcher-kva.example", FullAddress: "reader@xn--bcher-kva.example",
	})
	rv := New(st, policy.NamingFull, true)
	for _, address := range []string{"reader@xn--bcher-kva.example", "READER+Tag@XN--BCHER-KVA.EXAMPLE"} {
		t.Run(address, func(t *testing.T) {
			got, err := rv.Check(context.Background(), address)
			if err != nil || got == nil || got.Mailbox == nil {
				t.Fatalf("provisioned ASCII IDNA mailbox rejected: %#v, %v", got, err)
			}
			if got.Mailbox.ID != mailboxID || got.Zone.ID != zoneID || got.Created {
				t.Fatalf("existing mailbox identity changed: %#v", got)
			}
		})
	}
}
