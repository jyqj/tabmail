//go:build r5fixtures

package testpg_test

import (
	"context"
	"tabmail/internal/testpg"
	"testing"
)

func TestR5FixtureTenantRolesAndMailboxIdentity(t *testing.T) {
	f := testpg.NewR5Fixture(t)
	ctx := context.Background()
	if f.Companies[0].Tenant.ID == f.Companies[1].Tenant.ID {
		t.Fatal("tenant identities collapsed")
	}
	for _, c := range f.Companies {
		if c.Shared.Kind != "shared" || c.Shared.TenantID != c.Tenant.ID {
			t.Fatal("shared mailbox provenance mismatch")
		}
		for role, user := range c.Users {
			mailbox := c.Personal[role]
			if user.TenantID != c.Tenant.ID || mailbox.OwnerUserID == nil || *mailbox.OwnerUserID != user.ID || mailbox.Kind != "personal" {
				t.Fatal("employee/mailbox provenance mismatch")
			}
		}
		if c.Users["frozen"].IsActive {
			t.Fatal("guarded freeze did not persist")
		}
		for _, role := range []string{"reader", "organizer", "sender"} {
			box, err := f.Store.GetWorkMailbox(ctx, c.UserActor(role), c.Shared.ID)
			if err != nil || box == nil || !box.CanRead || box.CanOrganize != (role == "organizer") || box.CanSend != (role == "sender") {
				t.Fatalf("actual %s grant differs from legal fixture", role)
			}
		}
	}
	foreign := f.Companies[1].UserActor("reader")
	if box, err := f.Store.GetWorkMailbox(ctx, foreign, f.Companies[0].Shared.ID); err == nil && box != nil {
		t.Fatal("cross-tenant fixture lookup returned foreign mailbox")
	}
}
