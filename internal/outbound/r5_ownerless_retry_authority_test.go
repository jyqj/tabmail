package outbound

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func r5OwnerlessRetryFixture(t *testing.T) (*testutil.FakeStore, *models.TenantAPIKey, *models.DomainZone, *models.Mailbox, authz.Actor, *models.OutboundJob) {
	t.Helper()
	ctx := context.Background()
	st := testutil.NewFakeStore()
	tenant := uuid.New()
	z := &models.DomainZone{ID: uuid.New(), TenantID: tenant, Domain: "ownerless-retry.test", IsVerified: true, MXVerified: true}
	st.SeedZone(z)
	m := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: z.ID, FullAddress: "legacy@" + z.Domain, Kind: "legacy"}
	if e := st.CreateMailbox(ctx, m); e != nil {
		t.Fatal(e)
	}
	k := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenant, Scopes: []string{"send:read", "send:write"}}
	if e := st.CreateAPIKey(ctx, k); e != nil {
		t.Fatal(e)
	}
	a := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: tenant, TenantWide: true}
	keyID, mailboxID := k.ID, m.ID
	j := &models.OutboundJob{ID: uuid.New(), TenantID: tenant, ZoneID: z.ID, SenderKeyID: &keyID, SenderMailboxID: &mailboxID, MailFrom: m.FullAddress}
	return st, k, z, m, a, j
}

func TestR5OwnerlessRetryAuthoritySameKeyDurableLegacyMailbox(t *testing.T) {
	st, k, _, _, a, j := r5OwnerlessRetryFixture(t)
	ctx := context.Background()
	if e := ValidateJobAuthorization(ctx, st, nil, j); e != nil {
		t.Fatal("worker positive control", e)
	}
	if e := ValidateRetryRequester(ctx, st, a, j); e != nil {
		t.Fatal("same current ownerless Key lost its durable legacy send authority", e)
	}
	if authz.OutboundContentKeyMatches(a, k, j) {
		t.Fatal("send/retry authority conferred normal content-read authority")
	}
}

func TestR5OwnerlessRetryAuthorityRejectsStaleOrDifferentAuthority(t *testing.T) {
	for _, name := range []string{"different-key", "job-key-missing", "durable-owned-user", "key-deleted", "scope-read-only", "scope-wildcard", "zone-shrunk", "key-expire-at-now", "key-owner-added", "key-tenant-changed", "actor-tenant-changed", "job-tenant-changed", "mailbox-owned", "mailbox-shared", "mailbox-personal", "mailbox-unknown", "mailbox-deleted", "mailbox-address-reused", "mailbox-zone-changed", "mailbox-tenant-changed", "mailbox-expire-at-now", "from-display-name", "from-uppercase", "from-other-address", "zone-domain-changed", "zone-unverified", "zone-mx-unverified", "disabled-policy", "template-required-policy", "template-version-provenance"} {
		t.Run(name, func(t *testing.T) {
			st, k, z, m, a, j := r5OwnerlessRetryFixture(t)
			ctx := context.Background()
			other := uuid.New()
			now := time.Now()
			if e := ValidateRetryRequester(ctx, st, a, j); e != nil {
				t.Fatal("positive control", e)
			}
			mailboxChanged, keyChanged, zoneChanged := false, false, false
			switch name {
			case "different-key":
				second := &models.TenantAPIKey{ID: other, TenantID: k.TenantID, Scopes: k.Scopes}
				if e := st.CreateAPIKey(ctx, second); e != nil {
					t.Fatal(e)
				}
				a.ID = second.ID
			case "job-key-missing":
				j.SenderKeyID = nil
			case "durable-owned-user":
				j.SenderUserID = &other
			case "key-deleted":
				if e := st.DeleteAPIKey(ctx, k.ID); e != nil {
					t.Fatal(e)
				}
			case "scope-read-only":
				k.Scopes = []string{"send:read"}
				keyChanged = true
			case "scope-wildcard":
				k.Scopes = []string{"*"}
				keyChanged = true
			case "zone-shrunk":
				k.AllowedZoneIDs = []uuid.UUID{other}
				keyChanged = true
			case "key-expire-at-now":
				k.ExpiresAt = &now
				keyChanged = true
			case "key-owner-added":
				k.OwnerUserID = &other
				keyChanged = true
			case "key-tenant-changed":
				k.TenantID = other
				keyChanged = true
			case "actor-tenant-changed":
				a.TenantID = other
			case "job-tenant-changed":
				j.TenantID = other
			case "mailbox-owned":
				m.OwnerUserID = &other
				mailboxChanged = true
			case "mailbox-shared":
				m.Kind = "shared"
				mailboxChanged = true
			case "mailbox-personal":
				m.Kind = "personal"
				mailboxChanged = true
			case "mailbox-unknown":
				m.Kind = "unknown"
				mailboxChanged = true
			case "mailbox-deleted":
				if e := st.DeleteMailbox(ctx, m.ID); e != nil {
					t.Fatal(e)
				}
			case "mailbox-address-reused":
				if e := st.DeleteMailbox(ctx, m.ID); e != nil {
					t.Fatal(e)
				}
				m.ID = other
				if e := st.CreateMailbox(ctx, m); e != nil {
					t.Fatal(e)
				}
			case "mailbox-zone-changed":
				m.ZoneID = other
				mailboxChanged = true
			case "mailbox-tenant-changed":
				m.TenantID = other
				mailboxChanged = true
			case "mailbox-expire-at-now":
				m.ExpiresAt = &now
				mailboxChanged = true
			case "from-display-name":
				j.MailFrom = "Legacy <" + m.FullAddress + ">"
			case "from-uppercase":
				j.MailFrom = "LEGACY@" + z.Domain
			case "from-other-address":
				j.MailFrom = "other@" + z.Domain
			case "zone-domain-changed":
				z.Domain = "other.test"
				zoneChanged = true
			case "zone-unverified":
				z.IsVerified = false
				zoneChanged = true
			case "zone-mx-unverified":
				z.MXVerified = false
				zoneChanged = true
			case "disabled-policy":
				policy := string(authz.SendPolicyDisabled)
				m.SendPolicy = &policy
				mailboxChanged = true
			case "template-required-policy":
				policy := string(authz.SendPolicyTemplateRequired)
				m.SendPolicy = &policy
				mailboxChanged = true
			case "template-version-provenance":
				j.TemplateVersionID = &other
			}
			if keyChanged {
				if e := st.CreateAPIKey(ctx, k); e != nil {
					t.Fatal(e)
				}
			}
			if zoneChanged {
				st.SeedZone(z)
			}
			if mailboxChanged {
				if e := st.DeleteMailbox(ctx, m.ID); e != nil {
					t.Fatal(e)
				}
				if e := st.CreateMailbox(ctx, m); e != nil {
					t.Fatal(e)
				}
			}
			if e := ValidateRetryRequester(ctx, st, a, j); e == nil || !authz.IsAuthzError(e) {
				t.Fatalf("current authority mutation accepted or wrong denial: %v", e)
			}
		})
	}
}

func TestR5OwnerlessRetryAuthorityKeepsOwnedKeyAndJWTFences(t *testing.T) {
	for _, mode := range []string{"owned-key-freeze", "owned-key-to-ownerless", "owned-key-reassign", "jwt-session-revoked"} {
		t.Run(mode, func(t *testing.T) {
			st, k, _, m, _, j := r5OwnerlessRetryFixture(t)
			ctx := context.Background()
			u := &models.User{ID: uuid.New(), TenantID: k.TenantID, Role: models.RoleUser, IsActive: true, SessionVersion: 3}
			if e := st.CreateUser(ctx, u); e != nil {
				t.Fatal(e)
			}
			k.OwnerUserID = &u.ID
			if e := st.CreateAPIKey(ctx, k); e != nil {
				t.Fatal(e)
			}
			if e := st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: k.TenantID, MailboxID: m.ID, UserID: u.ID, CanRead: true, CanSend: true}); e != nil {
				t.Fatal(e)
			}
			j.SenderUserID = &u.ID
			a := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: k.TenantID, OwnerUserID: &u.ID}
			if mode == "jwt-session-revoked" {
				version := u.SessionVersion
				a = authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, SessionVersion: &version}
				j.SenderKeyID = nil
			}
			if e := ValidateRetryRequester(ctx, st, a, j); e != nil {
				t.Fatal("original principal positive control", e)
			}
			switch mode {
			case "owned-key-freeze":
				u.IsActive = false
				if e := st.CreateUser(ctx, u); e != nil {
					t.Fatal(e)
				}
			case "owned-key-to-ownerless":
				k.OwnerUserID = nil
				if e := st.CreateAPIKey(ctx, k); e != nil {
					t.Fatal(e)
				}
			case "owned-key-reassign":
				other := uuid.New()
				k.OwnerUserID = &other
				if e := st.CreateAPIKey(ctx, k); e != nil {
					t.Fatal(e)
				}
			case "jwt-session-revoked":
				u.SessionVersion++
				if e := st.CreateUser(ctx, u); e != nil {
					t.Fatal(e)
				}
			}
			if e := ValidateRetryRequester(ctx, st, a, j); e == nil || !authz.IsAuthzError(e) {
				t.Fatalf("old owned/JWT fence bypassed: %v", e)
			}
		})
	}
}

func TestR5OwnerlessRetryAuthorityResolverPolicyNeverSelfGrantsTemplate(t *testing.T) {
	for _, policy := range []string{string(authz.SendPolicyFree), string(authz.SendPolicyDisabled), string(authz.SendPolicyTemplateRequired)} {
		for _, published := range []bool{false, true} {
			t.Run(policy+"/published="+fmt.Sprint(published), func(t *testing.T) {
				st, _, _, m, a, j := r5OwnerlessRetryFixture(t)
				ctx := context.Background()
				if e := st.SetMailboxSendPolicy(ctx, m.ID, policy); e != nil {
					t.Fatal(e)
				}
				resolved, e := ResolveSendAuthorization(ctx, st, a, j.TenantID, j.MailFrom, published)
				if e != nil {
					t.Fatal(e)
				}
				want := policy == string(authz.SendPolicyFree) || policy == string(authz.SendPolicyTemplateRequired) && published
				if (resolved.WorkerFailure() == nil) != want {
					t.Fatalf("resolver policy=%s published=%t verdict=%v", policy, published, resolved.WorkerFailure())
				}
				if published {
					version := uuid.New()
					j.TemplateVersionID = &version
				}
				e = ValidateJobAuthorization(ctx, st, r5OwnerlessTemplateDeny{}, j)
				wantWorker := policy == string(authz.SendPolicyFree) && !published
				if (e == nil) != wantWorker {
					t.Fatalf("immutable/current template gate bypassed: policy=%s published=%t worker=%v", policy, published, e)
				}
			})
		}
	}
}

type r5OwnerlessTemplateDeny struct{}

func (r5OwnerlessTemplateDeny) TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error) {
	return nil, "", "", authz.ErrForbidden("published templates require an employee sender")
}
