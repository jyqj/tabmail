package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
	"testing"
)

func TestReleasePostgresOwnerPrivateMailboxWithoutSecondPassword(t *testing.T) {
	st, _, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	company := rbPGTenant(t, st)
	owner := rbPGUser(t, st, company.ID, models.RoleUser)
	z := &models.DomainZone{TenantID: company.ID, Domain: "private.company.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, z))
	mb := &models.Mailbox{TenantID: company.ID, ZoneID: z.ID, OwnerUserID: &owner.ID, LocalPart: "owner", ResolvedDomain: z.Domain, FullAddress: "owner@private.company.test", AccessMode: models.AccessToken}
	must(t, st.CreateMailbox(ctx, mb))
	got, err := st.GetMailbox(ctx, mb.ID)
	must(t, err)
	if got.OwnerUserID == nil || *got.OwnerUserID != owner.ID || got.PasswordHash != nil || got.AccessMode != models.AccessToken {
		t.Fatal("private owner credentials changed")
	}
	bad := *mb
	bad.ID = uuid.New()
	bad.OwnerUserID = nil
	bad.FullAddress = "unowned@private.company.test"
	bad.LocalPart = "unowned"
	if err = st.CreateMailbox(ctx, &bad); err == nil {
		t.Fatal("ownerless token mailbox accepted without credentials")
	}
}
