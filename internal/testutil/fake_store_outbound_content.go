package testutil

import (
	"context"
	"github.com/google/uuid"
	"time"

	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// CanReadOutboundContent is an atomic in-memory fixture projection, not an
// implementation of PostgreSQL expiry, snapshot locks or physical GC. Separate
// archive facts must exist; arbitrary unpersisted job structs are not authority.
func (s *FakeStore) CanReadOutboundContent(ctx context.Context, a authz.Actor, j *models.OutboundJob) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canReadOutboundContentLocked(a, j)
}

func (s *FakeStore) canReadOutboundContentLocked(a authz.Actor, j *models.OutboundJob) (bool, error) {
	if j == nil || j.TenantID != a.TenantID || j.SenderMailboxID == nil || !a.Permission.AllowsZone(j.ZoneID) {
		return false, nil
	}
	asset := s.outboundContent[j.ID]
	if asset == nil || asset.TenantID != j.TenantID || asset.ZoneID != j.ZoneID || asset.SenderMailboxID == nil || *asset.SenderMailboxID != *j.SenderMailboxID {
		return false, nil
	}
	uid := a.EffectiveUserID()
	if uid == nil {
		return false, nil
	}
	u := s.users[*uid]
	switch a.Type {
	case authz.PrincipalUser:
		var ok bool
		a, ok = authz.RefreshMemberActor(a, a.TenantID, u)
		if !ok {
			return false, nil
		}
	case authz.PrincipalAPIKey:
		key := s.apiKeys[a.ID]
		if u == nil || !u.IsActive || u.TenantID != a.TenantID || !authz.OutboundContentKeyMatches(a, key, j) || key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
			return false, nil
		}
		a = authz.Actor{Type: authz.PrincipalUser, ID: *uid, TenantID: a.TenantID, Permission: a.Permission}
	default:
		return false, nil
	}
	mb := s.mailboxes[*j.SenderMailboxID]
	if mb == nil || mb.ZoneID != j.ZoneID {
		return false, nil
	}
	grant := s.mailboxGrants[[2]uuid.UUID{mb.ID, *uid}]
	return authz.EvaluateMailboxAccess(a, mb, grant).CanRead, nil
}
