package authz_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"testing"
	"time"
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

// EvaluateMailboxAccess is the single decision point shared by the store and
// the handlers. The table pins its contract: content rights (read/organize/
// send) come only from ownership or an explicit grant — an admin role yields
// management visibility (CanManage) but never content access, and the
// profile-level CanSend capability vetoes sending for non-admins.
func TestEvaluateMailboxAccess(t *testing.T) {
	tenant := uuid.New()
	zone := uuid.New()
	owner := uuid.New()
	employee := uuid.New()
	adminID := uuid.New()
	other := uuid.New()
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, Kind: "shared"}
	personal := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, OwnerUserID: &owner}
	expired := time.Now().Add(-time.Hour)
	readGrant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: employee, CanRead: true}
	sendGrant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: employee, CanSend: true}
	templateGrant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: employee, CanSend: true, TemplateOnly: true}

	user := func(id uuid.UUID) authz.Actor {
		return authz.Actor{Type: authz.PrincipalUser, ID: id, TenantID: tenant}
	}
	admin := func(id uuid.UUID) authz.Actor {
		a := user(id)
		a.IsAdmin = true
		return a
	}
	sendProfile := func(a authz.Actor) authz.Actor {
		a.Permission = &models.EffectivePermission{CanSend: true}
		return a
	}
	zero := authz.MailboxDecision{}
	adminGrant := &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: adminID, CanSend: true}

	cases := []struct {
		name    string
		actor   authz.Actor
		mailbox *models.Mailbox
		grant   *models.MailboxGrant
		want    authz.MailboxDecision
	}{
		{"owner holds full content rights", sendProfile(user(owner)), personal, nil, authz.MailboxDecision{CanRead: true, CanOrganize: true, CanSend: true}},
		{"granted employee read-only", user(employee), mb, readGrant, authz.MailboxDecision{CanRead: true}},
		{"granted employee send", sendProfile(user(employee)), mb, sendGrant, authz.MailboxDecision{CanSend: true}},
		{"template-only grant keeps flag", sendProfile(user(employee)), mb, templateGrant, authz.MailboxDecision{CanSend: true, TemplateOnly: true}},
		{"admin without grant sees management metadata only", admin(adminID), mb, nil, authz.MailboxDecision{CanManage: true}},
		{"admin without grant cannot read employee mailbox", admin(adminID), personal, nil, authz.MailboxDecision{CanManage: true}},
		{"super admin without grant sees management metadata only", func() authz.Actor { a := user(adminID); a.IsSuperAdmin = true; return a }(), mb, nil, authz.MailboxDecision{CanManage: true}},
		{"shared mailbox is invisible to unaffiliated employee", user(other), mb, nil, zero},
		{"unaffiliated employee denied on personal mailbox", user(other), personal, nil, zero},
		{"foreign grant is ignored", user(other), mb, readGrant, zero},
		{"expired mailbox yields no content rights", admin(owner), &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, OwnerUserID: &owner, ExpiresAt: &expired}, nil, authz.MailboxDecision{CanManage: true}},
		{"zone-restricted profile loses content rights", func() authz.Actor { a := user(owner); a.Permission = &models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{other}}; return a }(), personal, nil, zero},
		{"profile without can_send vetoes owner send", func() authz.Actor { a := user(owner); a.Permission = &models.EffectivePermission{}; return a }(), personal, nil, authz.MailboxDecision{CanRead: true, CanOrganize: true}},
		{"profile without can_send vetoes grant send", func() authz.Actor { a := user(employee); a.Permission = &models.EffectivePermission{}; return a }(), mb, sendGrant, zero},
		{"missing profile vetoes employee send", user(employee), mb, sendGrant, zero},
		{"admin grant send survives the profile veto", func() authz.Actor { a := admin(adminID); a.Permission = &models.EffectivePermission{}; return a }(), mb, adminGrant, authz.MailboxDecision{CanSend: true, CanManage: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authz.EvaluateMailboxAccess(tc.actor, tc.mailbox, tc.grant)
			if got != tc.want {
				t.Fatalf("EvaluateMailboxAccess()=%+v, want %+v", got, tc.want)
			}
		})
	}
	if d := authz.EvaluateMailboxAccess(user(employee), nil, readGrant); d != zero {
		t.Fatalf("nil mailbox must deny: %+v", d)
	}
}
