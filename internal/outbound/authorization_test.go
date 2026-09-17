package outbound

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// nopGovernance is the explicit TemplateGovernance stub for service tests that
// never touch the published-template path; every lookup fails loudly.
type nopGovernance struct{}

func (nopGovernance) TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error) {
	return nil, "", "", errors.New("template governance not available in this test")
}

// TestResolveSendAuthorization covers the single send-authorization decision
// tree shared by the HTTP send handler and ValidateJobAuthorization. Both
// callers route through ResolveSendAuthorization, so this table pins the
// verdicts once for both paths.
func TestResolveSendAuthorization(t *testing.T) {
	tenantID := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenantID, Domain: "send.test", IsVerified: true, MXVerified: true}
	ownerID := uuid.New()
	otherID := uuid.New()
	keyID := uuid.New()

	cases := []struct {
		name               string
		actor              authz.Actor
		address            string
		setup              func(t *testing.T, st *testutil.FakeStore)
		hasTemplate        bool
		wantMailbox        bool
		wantIdentity       bool
		wantExpired        bool
		wantTenantWide     bool
		wantSenderDenied   bool
		wantIdentityUnver  bool
		wantWorkerFailure  string
		wantWorkerAuthzErr bool
	}{
		{
			name:    "owner sends from own mailbox",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &ownerID})
			},
			wantMailbox: true,
		},
		{
			name:    "employee without grant is denied on unassigned mailbox",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: otherID},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &ownerID})
			},
			wantMailbox:        true,
			wantSenderDenied:   true,
			wantWorkerFailure:  "exact mailbox send_as permission required",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "tenant-wide key banned from company mailbox",
			actor:   authz.Actor{Type: authz.PrincipalAPIKey, TenantID: tenantID, ID: keyID, TenantWide: true},
			address: "shared@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "shared@send.test", OwnerUserID: &ownerID})
			},
			wantMailbox:        true,
			wantTenantWide:     true,
			wantWorkerFailure:  "company mailbox requires employee sender",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "expired mailbox",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "old@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				exp := time.Now().Add(-time.Hour)
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "old@send.test", OwnerUserID: &ownerID, ExpiresAt: &exp})
			},
			wantMailbox:        true,
			wantExpired:        true,
			wantSenderDenied:   true, // CheckMailboxSender also fails eagerly; expiry still wins the report
			wantWorkerFailure:  "sender mailbox expired",
			wantWorkerAuthzErr: true,
		},
		{
			// The identity fallback is only reachable for tenant-wide
			// credentials or global admins: a plain user is stopped earlier by
			// the exact-mailbox send right (see the next case).
			name:    "verified exact send identity fallback for tenant-wide key",
			actor:   authz.Actor{Type: authz.PrincipalAPIKey, TenantID: tenantID, ID: keyID, TenantWide: true},
			address: "alias@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				if err := st.CreateSendIdentity(context.Background(), &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, Address: "alias@send.test", IdentityType: models.SendIdentityExact, Verified: true}); err != nil {
					t.Fatal(err)
				}
			},
			wantIdentity: true,
		},
		{
			name:    "plain user denied on identity-only address by exact send right",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "alias@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				if err := st.CreateSendIdentity(context.Background(), &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, Address: "alias@send.test", IdentityType: models.SendIdentityExact, Verified: true}); err != nil {
					t.Fatal(err)
				}
			},
			wantIdentity:       true,
			wantSenderDenied:   true,
			wantWorkerFailure:  "exact mailbox send_as permission required",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "unverified send identity fallback for tenant-wide key",
			actor:   authz.Actor{Type: authz.PrincipalAPIKey, TenantID: tenantID, ID: keyID, TenantWide: true},
			address: "pending@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				if err := st.CreateSendIdentity(context.Background(), &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, Address: "pending@send.test", IdentityType: models.SendIdentityExact, Verified: false}); err != nil {
					t.Fatal(err)
				}
			},
			wantIdentity:       true,
			wantIdentityUnver:  true,
			wantWorkerFailure:  "sender identity revoked",
			wantWorkerAuthzErr: true,
		},
		{
			name:               "unknown address resolves nothing",
			actor:              authz.Actor{Type: authz.PrincipalAPIKey, TenantID: tenantID, ID: keyID, TenantWide: true},
			address:            "ghost@send.test",
			setup:              func(t *testing.T, st *testutil.FakeStore) {},
			wantIdentityUnver:  true,
			wantWorkerFailure:  "sender identity revoked",
			wantWorkerAuthzErr: true,
		},
		// The effective send policy rides on the mailbox row and binds the
		// owner exactly like a grant holder; no role bypasses the gate.
		{
			name:    "owner blocked by disabled company policy",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				policy := "disabled"
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &ownerID, SendPolicy: &policy})
			},
			wantMailbox:        true,
			wantSenderDenied:   true,
			wantWorkerFailure:  "mailbox sending disabled by company policy",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "owner free-form send under template_required demands the published template",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				policy := "template_required"
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &ownerID, SendPolicy: &policy})
			},
			wantMailbox:        true,
			wantSenderDenied:   true,
			wantWorkerFailure:  "a granted published template version is required",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "owner template-path send under template_required passes",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				policy := "template_required"
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &ownerID, SendPolicy: &policy})
			},
			hasTemplate: true,
			wantMailbox: true,
		},
		{
			name:    "global admin without rights is denied on a company mailbox",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID, IsSuperAdmin: true},
			address: "owner@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				other := uuid.New()
				st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@send.test", OwnerUserID: &other})
			},
			wantMailbox:        true,
			wantSenderDenied:   true,
			wantWorkerFailure:  "exact mailbox send_as permission required",
			wantWorkerAuthzErr: true,
		},
		{
			name:    "global admin keeps the verified-identity fallback on a mailbox-less address",
			actor:   authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, ID: ownerID, IsSuperAdmin: true},
			address: "alias@send.test",
			setup: func(t *testing.T, st *testutil.FakeStore) {
				if err := st.CreateSendIdentity(context.Background(), &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, Address: "alias@send.test", IdentityType: models.SendIdentityExact, Verified: true}); err != nil {
					t.Fatal(err)
				}
			},
			wantIdentity: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			st.SeedZone(zone)
			tc.setup(t, st)
			res, err := ResolveSendAuthorization(context.Background(), st, tc.actor, tenantID, tc.address, tc.hasTemplate)
			if err != nil {
				t.Fatalf("ResolveSendAuthorization: %v", err)
			}
			if (res.Mailbox != nil) != tc.wantMailbox {
				t.Fatalf("mailbox resolved = %v, want %v", res.Mailbox != nil, tc.wantMailbox)
			}
			if (res.Identity != nil) != tc.wantIdentity {
				t.Fatalf("identity resolved = %v, want %v", res.Identity != nil, tc.wantIdentity)
			}
			if res.MailboxExpired != tc.wantExpired {
				t.Fatalf("MailboxExpired = %v, want %v", res.MailboxExpired, tc.wantExpired)
			}
			if res.TenantWideBlocked != tc.wantTenantWide {
				t.Fatalf("TenantWideBlocked = %v, want %v", res.TenantWideBlocked, tc.wantTenantWide)
			}
			if (res.MailboxSenderErr != nil) != tc.wantSenderDenied {
				t.Fatalf("MailboxSenderErr set = %v, want %v (%v)", res.MailboxSenderErr != nil, tc.wantSenderDenied, res.MailboxSenderErr)
			}
			if res.IdentityUnverified != tc.wantIdentityUnver {
				t.Fatalf("IdentityUnverified = %v, want %v", res.IdentityUnverified, tc.wantIdentityUnver)
			}
			workerErr := res.WorkerFailure()
			if tc.wantWorkerFailure == "" {
				if workerErr != nil {
					t.Fatalf("WorkerFailure = %v, want nil", workerErr)
				}
				return
			}
			if workerErr == nil || workerErr.Error() != tc.wantWorkerFailure {
				t.Fatalf("WorkerFailure = %v, want %q", workerErr, tc.wantWorkerFailure)
			}
			if authz.IsAuthzError(workerErr) != tc.wantWorkerAuthzErr {
				t.Fatalf("WorkerFailure authz classification = %v, want %v", authz.IsAuthzError(workerErr), tc.wantWorkerAuthzErr)
			}
		})
	}
}

// TestValidateJobAuthorizationUsesSharedResolver pins that the worker-side
// re-authorization reports the same verdict as the shared decision function
// for the same input: a tenant-wide key over a company mailbox must stay
// forbidden across submit and every delivery attempt.
func TestValidateJobAuthorizationUsesSharedResolver(t *testing.T) {
	tenantID := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenantID, Domain: "worker.test", IsVerified: true, MXVerified: true}
	ownerID := uuid.New()
	st := testutil.NewFakeStore()
	st.SeedZone(zone)
	st.SeedMailbox(&models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "shared@worker.test", OwnerUserID: &ownerID})

	svc := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop())

	key := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenantID, Scopes: []string{"send:write"}}
	if err := st.CreateAPIKey(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	job := &models.OutboundJob{
		ID: uuid.New(), TenantID: tenantID, SenderKeyID: &key.ID, ZoneID: zone.ID,
		MailFrom: "shared@worker.test", RcptTo: []string{"rcpt@example.test"},
		Subject: "s", TextBody: "b", State: models.OutboundPending,
	}
	err := svc.ValidateJobAuthorization(context.Background(), job)
	if err == nil || err.Error() != "company mailbox requires employee sender" {
		t.Fatalf("ValidateJobAuthorization = %v, want company mailbox tenant-wide ban", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("expected authz error, got %T: %v", err, err)
	}
}

// TestValidateJobAuthorizationPicksUpPolicyChange proves the worker side
// inherits send-policy changes with no code of its own: a free-form job
// enqueued under the 'free' default passes its first re-validation, and once
// the mailbox's effective policy flips (template_required, then disabled)
// the NEXT delivery attempt is refused by the same shared decision tree —
// ValidateJobAuthorization re-runs ResolveSendAuthorization every attempt.
func TestValidateJobAuthorizationPicksUpPolicyChange(t *testing.T) {
	tenantID := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenantID, Domain: "policy.test", IsVerified: true, MXVerified: true}
	ownerID := uuid.New()
	st := testutil.NewFakeStore()
	st.SeedZone(zone)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zone.ID, FullAddress: "owner@policy.test", OwnerUserID: &ownerID}
	st.SeedMailbox(mb)
	if err := st.CreateUser(context.Background(), &models.User{ID: ownerID, TenantID: tenantID, Email: "owner@policy.test", IsActive: true, Role: models.RoleUser}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(config.Outbound{Enabled: true}, st, nopGovernance{}, zerolog.Nop())
	job := &models.OutboundJob{
		ID: uuid.New(), TenantID: tenantID, SenderUserID: &ownerID, SenderMailboxID: &mb.ID, ZoneID: zone.ID,
		MailFrom: "owner@policy.test", RcptTo: []string{"rcpt@example.test"},
		Subject: "s", TextBody: "b", State: models.OutboundPending,
	}
	if err := svc.ValidateJobAuthorization(context.Background(), job); err != nil {
		t.Fatalf("free policy must let the enqueued free-form job pass: %v", err)
	}

	if err := st.SetMailboxSendPolicy(context.Background(), mb.ID, "template_required"); err != nil {
		t.Fatal(err)
	}
	err := svc.ValidateJobAuthorization(context.Background(), job)
	if err == nil || err.Error() != "a granted published template version is required" {
		t.Fatalf("next attempt after template_required flip = %v, want template refusal", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("policy refusal must be an authz error, got %T: %v", err, err)
	}

	if err := st.SetMailboxSendPolicy(context.Background(), mb.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	err = svc.ValidateJobAuthorization(context.Background(), job)
	if err == nil || err.Error() != "mailbox sending disabled by company policy" {
		t.Fatalf("next attempt after disabled flip = %v, want policy refusal", err)
	}
	if !authz.IsAuthzError(err) {
		t.Fatalf("policy refusal must be an authz error, got %T: %v", err, err)
	}
}
