package outbound

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type enqueueQuotaReader struct {
	*testutil.FakeStore
	user                       *models.User
	permission                 *models.EffectivePermission
	userErr, permissionErr     error
	userCalls, permissionCalls int
	wantUser                   uuid.UUID
}

func (r *enqueueQuotaReader) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	r.userCalls++
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if id != r.wantUser {
		return nil, errors.New("wrong quota user")
	}
	return r.user, r.userErr
}
func (r *enqueueQuotaReader) EffectivePermission(ctx context.Context, id uuid.UUID) (*models.EffectivePermission, error) {
	r.permissionCalls++
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if id != r.wantUser {
		return nil, errors.New("wrong permission user")
	}
	return r.permission, r.permissionErr
}

func TestCurrentEnqueueQuotaUsesCurrentRoleAndOwnedKeyPolicy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		principal       authz.PrincipalType
		role            models.UserRole
		staleAdmin      bool
		limit, want     int
		permissionCalls int
	}{
		{"employee", authz.PrincipalUser, models.RoleUser, false, 7, 7, 1},
		{"employee-unlimited", authz.PrincipalUser, models.RoleUser, false, 0, 0, 1},
		{"current-admin", authz.PrincipalUser, models.RoleAdmin, false, 7, 0, 0},
		{"current-superadmin", authz.PrincipalUser, models.RoleSuperAdmin, false, 7, 0, 0},
		{"demoted-admin", authz.PrincipalUser, models.RoleUser, true, 7, 7, 1},
		{"owned-key-employee", authz.PrincipalAPIKey, models.RoleUser, false, 7, 7, 1},
		{"owned-key-admin", authz.PrincipalAPIKey, models.RoleAdmin, true, 7, 7, 1},
		{"owned-key-superadmin", authz.PrincipalAPIKey, models.RoleSuperAdmin, true, 7, 7, 1},
		{"owned-key-unlimited", authz.PrincipalAPIKey, models.RoleAdmin, true, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, tenant := uuid.New(), uuid.New()
			a := authz.Actor{Type: tc.principal, ID: uid, TenantID: tenant, IsAdmin: tc.staleAdmin, Permission: &models.EffectivePermission{DailySendQuota: 999}}
			if tc.principal == authz.PrincipalAPIKey {
				a.ID = uuid.New()
				a.OwnerUserID = &uid
			}
			j := &models.OutboundJob{TenantID: tenant, SenderUserID: &uid}
			r := &enqueueQuotaReader{FakeStore: testutil.NewFakeStore(), wantUser: uid, user: &models.User{ID: uid, TenantID: tenant, Role: tc.role, IsActive: true}, permission: &models.EffectivePermission{CanSend: true, DailySendQuota: tc.limit}}
			old := &store.OutboundUserDailyQuota{UserID: &uid, Limit: 999, Since: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
			sendAs := &store.OutboundSendAsDailyQuota{IdentityID: uuid.New(), Limit: 3, Since: old.Since}
			reservation := store.OutboundQuotaReservation{UserDaily: old, SendAsDaily: sendAs}
			before := time.Now().UTC().Truncate(24 * time.Hour)
			got, e := CurrentEnqueueQuota(context.Background(), r, &a, j, reservation)
			if e != nil {
				t.Fatal(e)
			}
			after := time.Now().UTC().Truncate(24 * time.Hour)
			if got.UserDaily == nil || got.UserDaily.UserID == nil || *got.UserDaily.UserID != uid || got.UserDaily.Limit != tc.want || !got.CurrentUserPolicy {
				t.Fatalf("wrong current quota: %+v", got)
			}
			if !got.UserDaily.Since.Equal(before) && !got.UserDaily.Since.Equal(after) {
				t.Fatalf("not UTC calendar boundary: %s", got.UserDaily.Since)
			}
			if r.userCalls != 1 || r.permissionCalls != tc.permissionCalls {
				t.Fatalf("quota reader calls: user=%d permission=%d", r.userCalls, r.permissionCalls)
			}
			if got.UserDaily == old || old.Limit != 999 || !old.Since.Equal(sendAs.Since) || reservation.CurrentUserPolicy || got.SendAsDaily != sendAs {
				t.Fatal("refreshed user policy mutated the early reservation or changed send-as policy")
			}
			got.UserDaily.Limit = 123
			if old.Limit != 999 {
				t.Fatal("new quota aliases the captured early quota")
			}
		})
	}
}

func TestCurrentEnqueueQuotaPreservesExplicitInternalAndOwnerlessReservations(t *testing.T) {
	for _, mode := range []string{"internal", "ownerless"} {
		t.Run(mode, func(t *testing.T) {
			var principal *authz.Actor
			if mode == "ownerless" {
				principal = &authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: uuid.New(), TenantWide: true}
			}
			uid := uuid.New()
			q := store.OutboundQuotaReservation{UserDaily: &store.OutboundUserDailyQuota{UserID: &uid, Limit: 3, Since: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, SendAsDaily: &store.OutboundSendAsDailyQuota{Limit: 4}}
			r := &enqueueQuotaReader{FakeStore: testutil.NewFakeStore(), userErr: errors.New("must not read")}
			got, e := CurrentEnqueueQuota(context.Background(), r, principal, nil, q)
			if e != nil || !reflect.DeepEqual(got, q) || got.UserDaily != q.UserDaily || got.SendAsDaily != q.SendAsDaily || r.userCalls != 0 || r.permissionCalls != 0 {
				t.Fatalf("explicit integration reservation changed: got=%+v err=%v", got, e)
			}
		})
	}
}

func TestCurrentEnqueueQuotaRejectsUnavailablePolicyAndPropagatesErrors(t *testing.T) {
	for _, mode := range []string{"inactive", "missing-user", "missing-job", "missing-sender", "wrong-sender", "missing-permission", "negative-quota", "user-error", "permission-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			uid, tenant := uuid.New(), uuid.New()
			a := authz.Actor{Type: authz.PrincipalUser, ID: uid, TenantID: tenant}
			j := &models.OutboundJob{TenantID: tenant, SenderUserID: &uid}
			r := &enqueueQuotaReader{FakeStore: testutil.NewFakeStore(), wantUser: uid, user: &models.User{ID: uid, TenantID: tenant, Role: models.RoleUser, IsActive: true}, permission: &models.EffectivePermission{DailySendQuota: 3}}
			failure := errors.New("quota reader failed")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "inactive":
				r.user.IsActive = false
			case "missing-user":
				r.user = nil
			case "missing-job":
				j = nil
			case "missing-sender":
				j.SenderUserID = nil
			case "wrong-sender":
				id := uuid.New()
				j.SenderUserID = &id
			case "missing-permission":
				r.permission = nil
			case "negative-quota":
				r.permission.DailySendQuota = -1
			case "user-error":
				r.userErr = failure
			case "permission-error":
				r.permissionErr = failure
			case "cancelled":
				cancel()
			}
			captured := store.OutboundQuotaReservation{UserDaily: &store.OutboundUserDailyQuota{UserID: &uid, Limit: 9}}
			got, e := CurrentEnqueueQuota(ctx, r, &a, j, captured)
			if e == nil {
				t.Fatal("unavailable quota policy accepted")
			}
			if mode == "user-error" || mode == "permission-error" {
				if !errors.Is(e, failure) {
					t.Fatalf("reader error lost: %v", e)
				}
			} else if mode == "cancelled" {
				if !errors.Is(e, context.Canceled) {
					t.Fatalf("context error lost: %v", e)
				}
			} else if !authz.IsAuthzError(e) {
				t.Fatalf("wrong unavailable policy rejection: %v", e)
			}
			if !reflect.DeepEqual(got, captured) || captured.UserDaily.Limit != 9 || got.CurrentUserPolicy {
				t.Fatal("failed quota refresh altered captured reservation")
			}
		})
	}
}
