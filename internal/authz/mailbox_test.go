package authz_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"testing"
)

func TestP0MailboxGrantActionsAreIndependent(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewFakeStore()
	tenant := uuid.New()
	uid := uuid.New()
	st.SeedTenant(&models.Tenant{ID: tenant})
	_ = st.CreateUser(ctx, &models.User{ID: uid, TenantID: tenant, IsActive: true})
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: uuid.New(), AccessMode: models.AccessAPIKey}
	st.SeedMailbox(mb)
	g := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: uid, CanSend: true}
	if err := st.SetMailboxGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	rights, err := authz.MailboxRights(ctx, st, tenant, &uid, mb)
	if err != nil || rights == nil || !rights.CanSend || rights.CanRead || rights.CanOrganize {
		t.Fatalf("send must not imply read: %+v %v", rights, err)
	}
	a := authz.Actor{Type: authz.PrincipalUser, ID: uid, TenantID: tenant}
	if err := authz.CheckMailboxSender(ctx, st, a, mb, false); err != nil {
		t.Fatal(err)
	}
	g.CanSend = false
	g.CanRead = true
	if err := st.SetMailboxGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := authz.CheckMailboxSender(ctx, st, a, mb, false); err == nil {
		t.Fatal("read grant allowed sending")
	}
	if rights, _ = authz.MailboxRights(ctx, st, uuid.New(), &uid, mb); rights != nil {
		t.Fatal("cross-tenant rights")
	}
	g.CanSend = true
	g.TemplateOnly = true
	_ = st.SetMailboxGrant(ctx, g)
	if err := authz.CheckMailboxSender(ctx, st, a, mb, false); err == nil {
		t.Fatal("legacy template name bypassed published-template restriction")
	}
	if err := authz.CheckMailboxSender(ctx, st, a, mb, true); err != nil {
		t.Fatal("published-template path was rejected", err)
	}
	mb.OwnerUserID = &uid
	rights, err = authz.MailboxRights(ctx, st, tenant, &uid, mb)
	if err != nil || !rights.CanRead || !rights.CanOrganize || !rights.CanSend {
		t.Fatal("owner must have normal mailbox rights")
	}
}
