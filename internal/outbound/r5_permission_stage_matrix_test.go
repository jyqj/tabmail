package outbound

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type r5StageReader struct {
	*testutil.FakeStore
	permission *models.EffectivePermission
	reads      int
}

func (r *r5StageReader) EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error) {
	r.reads++
	p := *r.permission
	p.AllowedZoneIDs = append([]uuid.UUID(nil), p.AllowedZoneIDs...)
	return &p, nil
}

// Fast tests isolate the durable sender/requester boundary. They deliberately
// do not claim canonical PG merge, formal-router or actual-worker acceptance.
func TestR5PermissionStageDurableCredentialMatrix(t *testing.T) {
	for _, kind := range []string{"jwt-user", "jwt-admin", "owned-user-key", "owned-admin-key"} {
		for _, mode := range []string{"all", "list", "none", "legacy-inherited-list"} {
			for _, canSend := range []bool{false, true} {
				for _, zoneAllows := range []bool{false, true} {
					for _, grant := range []string{"none", "read-only", "send"} {
						t.Run(fmt.Sprintf("%s/%s/send=%t/zone=%t/grant=%s", kind, mode, canSend, zoneAllows, grant), func(t *testing.T) {
							ctx := context.Background()
							tenant, zoneID, otherZone := uuid.New(), uuid.New(), uuid.New()
							r := &r5StageReader{FakeStore: testutil.NewFakeStore(), permission: &models.EffectivePermission{CanSend: canSend, DomainAccessMode: mode}}
							scopeZone := otherZone
							if zoneAllows {
								scopeZone = zoneID
							}
							if mode == "legacy-inherited-list" {
								r.permission.DomainAccessMode = ""
							}
							if mode == "list" || mode == "legacy-inherited-list" {
								r.permission.AllowedZoneIDs = []uuid.UUID{scopeZone}
							}
							role := models.RoleUser
							if kind == "jwt-admin" || kind == "owned-admin-key" {
								role = models.RoleAdmin
							}
							u := &models.User{ID: uuid.New(), TenantID: tenant, IsActive: true, Role: role}
							if err := r.CreateUser(ctx, u); err != nil {
								t.Fatal(err)
							}
							r.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenant, Domain: "stage.test", IsVerified: true, MXVerified: true})
							mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zoneID, FullAddress: "shared@stage.test", Kind: "shared"}
							r.SeedMailbox(mb)
							if grant != "none" {
								if err := r.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: tenant, MailboxID: mb.ID, UserID: u.ID, CanRead: true, CanSend: grant == "send"}); err != nil {
									t.Fatal(err)
								}
							}
							j := &models.OutboundJob{TenantID: tenant, ZoneID: zoneID, SenderUserID: &u.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress}
							a := authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: tenant, Role: role, Permission: &models.EffectivePermission{CanSend: true}}
							if kind == "owned-user-key" || kind == "owned-admin-key" {
								k := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenant, OwnerUserID: &u.ID, Scopes: []string{"send:write"}}
								if err := r.CreateAPIKey(ctx, k); err != nil {
									t.Fatal(err)
								}
								j.SenderKeyID = &k.ID
								a = authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: tenant, OwnerUserID: &u.ID, Permission: &models.EffectivePermission{CanSend: true}}
							}
							profileZone := mode == "all" || (mode != "none" && zoneAllows)
							want := grant == "send" && (kind == "jwt-admin" || canSend && profileZone)
							for _, stage := range []struct {
								name string
								run  func() error
							}{{"worker", func() error { return ValidateJobAuthorization(ctx, r, nil, j) }}, {"retry", func() error { return ValidateRetryRequester(ctx, r, a, j) }}} {
								err := stage.run()
								if (err == nil) != want {
									t.Fatalf("%s=%v want allow=%t", stage.name, err, want)
								}
								if !want && !authz.IsAuthzError(err) {
									t.Fatalf("%s wrong denial classification: %v", stage.name, err)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestR5PermissionStageKeyMutationRechecks(t *testing.T) {
	for _, mutation := range []string{"can-send-revoke", "domain-none", "key-scope-shrink", "key-zone-shrink", "key-expire-now", "key-owner-reassign", "owner-freeze", "mailbox-owner-reassign"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			tenant, zoneID := uuid.New(), uuid.New()
			r := &r5StageReader{FakeStore: testutil.NewFakeStore(), permission: &models.EffectivePermission{CanSend: true}}
			u := &models.User{ID: uuid.New(), TenantID: tenant, IsActive: true, Role: models.RoleAdmin}
			if err := r.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			r.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenant, Domain: "stage.test", IsVerified: true, MXVerified: true})
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zoneID, FullAddress: "owner@stage.test", OwnerUserID: &u.ID}
			r.SeedMailbox(mb)
			k := &models.TenantAPIKey{ID: uuid.New(), TenantID: tenant, OwnerUserID: &u.ID, Scopes: []string{"send:write"}}
			if err := r.CreateAPIKey(ctx, k); err != nil {
				t.Fatal(err)
			}
			j := &models.OutboundJob{TenantID: tenant, ZoneID: zoneID, SenderUserID: &u.ID, SenderKeyID: &k.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress}
			a := authz.Actor{Type: authz.PrincipalAPIKey, ID: k.ID, TenantID: tenant, OwnerUserID: &u.ID, Permission: &models.EffectivePermission{CanSend: true}}
			if err := ValidateJobAuthorization(ctx, r, nil, j); err != nil {
				t.Fatal("worker positive control", err)
			}
			if err := ValidateRetryRequester(ctx, r, a, j); err != nil {
				t.Fatal("retry positive control", err)
			}
			other := uuid.New()
			switch mutation {
			case "can-send-revoke":
				r.permission.CanSend = false
			case "domain-none":
				r.permission.DomainAccessMode = "none"
			case "key-scope-shrink":
				k.Scopes = []string{"send:read"}
			case "key-zone-shrink":
				k.AllowedZoneIDs = []uuid.UUID{other}
			case "key-expire-now":
				now := time.Now()
				k.ExpiresAt = &now
			case "key-owner-reassign":
				k.OwnerUserID = &other
			case "owner-freeze":
				u.IsActive = false
				if err := r.CreateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
			case "mailbox-owner-reassign":
				mb.OwnerUserID = &other
				if err := r.DeleteMailbox(ctx, mb.ID); err != nil {
					t.Fatal(err)
				}
				if err := r.CreateMailbox(ctx, mb); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.CreateAPIKey(ctx, k); err != nil {
				t.Fatal(err)
			}
			for _, err := range []error{ValidateJobAuthorization(ctx, r, nil, j), ValidateRetryRequester(ctx, r, a, j)} {
				if err == nil || !authz.IsAuthzError(err) {
					t.Fatalf("stale authority accepted or non-authz failure: %v", err)
				}
			}
		})
	}
}
