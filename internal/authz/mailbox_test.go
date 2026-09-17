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
		{"zone-restricted profile loses content rights", func() authz.Actor {
			a := user(owner)
			a.Permission = &models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{other}}
			return a
		}(), personal, nil, zero},
		{"profile without can_send vetoes owner send", func() authz.Actor { a := user(owner); a.Permission = &models.EffectivePermission{}; return a }(), personal, nil, authz.MailboxDecision{CanRead: true, CanOrganize: true}},
		{"profile without can_send vetoes grant send", func() authz.Actor { a := user(employee); a.Permission = &models.EffectivePermission{}; return a }(), mb, sendGrant, zero},
		{"missing profile vetoes employee send", user(employee), mb, sendGrant, zero},
		{"admin grant send survives the profile veto", func() authz.Actor { a := admin(adminID); a.Permission = &models.EffectivePermission{}; return a }(), mb, adminGrant, authz.MailboxDecision{CanSend: true, CanManage: true}},
		// Effective send policy rides on the mailbox row (nil = free) and
		// binds the owner exactly like a grant holder.
		{"owner cannot send under disabled policy", sendProfile(user(owner)), withPolicy(personal, authz.SendPolicyDisabled), nil, authz.MailboxDecision{CanRead: true, CanOrganize: true}},
		{"owner send becomes template-only under template_required", sendProfile(user(owner)), withPolicy(personal, authz.SendPolicyTemplateRequired), nil, authz.MailboxDecision{CanRead: true, CanOrganize: true, CanSend: true, TemplateOnly: true}},
		{"disabled policy strips grant send", sendProfile(user(employee)), withPolicy(mb, authz.SendPolicyDisabled), sendGrant, zero},
		{"template_required imposes template-only on a plain send grant", sendProfile(user(employee)), withPolicy(mb, authz.SendPolicyTemplateRequired), sendGrant, authz.MailboxDecision{CanSend: true, TemplateOnly: true}},
		{"admin keeps management visibility under disabled policy", admin(adminID), withPolicy(mb, authz.SendPolicyDisabled), nil, authz.MailboxDecision{CanManage: true}},
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

// withPolicy copies the mailbox and stamps an effective send policy on it,
// mirroring what the store's canonical mailbox select (COALESCE of the
// mailbox override over the tenant default) delivers on every row.
func withPolicy(mb *models.Mailbox, p authz.MailSendPolicy) *models.Mailbox {
	cp := *mb
	s := string(p)
	cp.SendPolicy = &s
	return &cp
}

// TestCheckMailboxSenderPolicy pins the send-policy verdicts of the shared
// send gate: the policy binds owners, tenant admins and global admins alike;
// only a mailbox-less address (the verified-identity fallback, which has no
// mailbox for a policy to gate) retains the global admin's historical reach.
func TestCheckMailboxSenderPolicy(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	owner := uuid.New()
	adminID := uuid.New()
	employee := uuid.New()

	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: uuid.New(), FullAddress: "box@send.test", OwnerUserID: &owner}
	shared := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: uuid.New(), FullAddress: "shared@send.test"}

	actor := func(id uuid.UUID) authz.Actor {
		return authz.Actor{Type: authz.PrincipalUser, ID: id, TenantID: tenant}
	}
	ownerActor, tenantAdmin, globalAdmin := actor(owner), func() authz.Actor { a := actor(adminID); a.IsAdmin = true; return a }(), func() authz.Actor { a := actor(adminID); a.IsSuperAdmin = true; return a }()

	cases := []struct {
		name                 string
		actor                authz.Actor
		mailbox              *models.Mailbox
		grant                *models.MailboxGrant
		hasPublished         bool
		wantErr              string
		wantIdentityFallback bool
	}{
		{"owner send under free policy still passes", ownerActor, withPolicy(mb, authz.SendPolicyFree), nil, false, "", false},
		{"owner is bound by disabled policy", ownerActor, withPolicy(mb, authz.SendPolicyDisabled), nil, false, "mailbox sending disabled by company policy", false},
		{"owner free-form send under template_required is refused", ownerActor, withPolicy(mb, authz.SendPolicyTemplateRequired), nil, false, "a granted published template version is required", false},
		{"owner template-path send under template_required passes", ownerActor, withPolicy(mb, authz.SendPolicyTemplateRequired), nil, true, "", false},
		{"tenant admin without rights cannot send even under free policy", tenantAdmin, withPolicy(mb, authz.SendPolicyFree), nil, false, "exact mailbox send_as permission required", false},
		{"tenant admin grant is bound by disabled policy", tenantAdmin, withPolicy(shared, authz.SendPolicyDisabled), &models.MailboxGrant{TenantID: tenant, MailboxID: shared.ID, UserID: adminID, CanSend: true}, false, "mailbox sending disabled by company policy", false},
		{"global admin without rights cannot send from a mailbox", globalAdmin, withPolicy(mb, authz.SendPolicyFree), nil, false, "exact mailbox send_as permission required", false},
		{"global admin ownership is bound by disabled policy", globalAdmin, withPolicy(&models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: uuid.New(), FullAddress: "adminbox@send.test", OwnerUserID: &adminID}, authz.SendPolicyDisabled), nil, false, "mailbox sending disabled by company policy", false},
		{"global admin grant is bound by disabled policy", globalAdmin, withPolicy(shared, authz.SendPolicyDisabled), &models.MailboxGrant{TenantID: tenant, MailboxID: shared.ID, UserID: adminID, CanSend: true}, false, "mailbox sending disabled by company policy", false},
		{"global admin keeps the verified-identity fallback on a mailbox-less address", globalAdmin, nil, nil, false, "", true},
		{"plain send grant under template_required requires the published template", actor(employee), withPolicy(shared, authz.SendPolicyTemplateRequired), &models.MailboxGrant{TenantID: tenant, MailboxID: shared.ID, UserID: employee, CanSend: true}, false, "a granted published template version is required", false},
		{"grant holder template-path send under template_required passes", actor(employee), withPolicy(shared, authz.SendPolicyTemplateRequired), &models.MailboxGrant{TenantID: tenant, MailboxID: shared.ID, UserID: employee, CanSend: true}, true, "", false},
		{"grant TemplateOnly semantics unchanged under free policy", actor(employee), withPolicy(shared, authz.SendPolicyFree), &models.MailboxGrant{TenantID: tenant, MailboxID: shared.ID, UserID: employee, CanSend: true, TemplateOnly: true}, false, "a granted published template version is required", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			for _, m := range []*models.Mailbox{mb, shared} {
				st.SeedMailbox(m)
			}
			for _, uid := range []uuid.UUID{owner, adminID, employee} {
				_ = st.CreateUser(ctx, &models.User{ID: uid, TenantID: tenant, IsActive: true})
			}
			if tc.grant != nil {
				if err := st.SetMailboxGrant(ctx, tc.grant); err != nil {
					t.Fatal(err)
				}
			}
			err := authz.CheckMailboxSender(ctx, st, tc.actor, tc.mailbox, tc.hasPublished)
			if tc.wantIdentityFallback {
				if err != nil {
					t.Fatalf("identity fallback must stay reachable, got %v", err)
				}
				return
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckMailboxSender = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("CheckMailboxSender = %v, want %q", err, tc.wantErr)
			}
			if !authz.IsAuthzError(err) {
				t.Fatalf("policy refusal must be an authz error, got %T", err)
			}
		})
	}
}
