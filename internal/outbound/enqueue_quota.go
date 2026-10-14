package outbound

import (
	"context"
	"time"

	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// CurrentEnqueueQuota runs after the shared requester/sender validation, using
// the SAME fenced reader. Neither effective values captured by middleware nor
// a nil early reservation can disable a newly finite current user limit.
func CurrentEnqueueQuota(ctx context.Context, reader JobAuthorizationReader, principal *authz.Actor, job *models.OutboundJob, reservation store.OutboundQuotaReservation) (store.OutboundQuotaReservation, error) {
	if principal == nil {
		return reservation, nil // Explicit trusted internal reservation.
	}
	uid := principal.EffectiveUserID()
	if uid == nil {
		return reservation, nil // Ownerless integrations have no user quota.
	}
	user, err := reader.GetUser(ctx, *uid)
	if err != nil {
		return reservation, err
	}
	if user == nil || !user.IsActive || job == nil || job.SenderUserID == nil || *job.SenderUserID != *uid {
		return reservation, authz.ErrForbidden("current quota principal unavailable")
	}
	limit := 0
	// Interactive current admins keep the existing unlimited policy; an owned
	// API key remains a key even when its owner is an administrator.
	if principal.Type != authz.PrincipalUser || user.Role != models.RoleAdmin && user.Role != models.RoleSuperAdmin {
		permission, err := reader.EffectivePermission(ctx, *uid)
		if err != nil {
			return reservation, err
		}
		if permission == nil || permission.DailySendQuota < 0 {
			return reservation, authz.ErrForbidden("current quota policy unavailable")
		}
		limit = permission.DailySendQuota
	}
	reservation.UserDaily = &store.OutboundUserDailyQuota{UserID: uid, Limit: limit, Since: time.Now().UTC().Truncate(24 * time.Hour)}
	reservation.CurrentUserPolicy = true
	return reservation, nil
}
