package testutil

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// This mutex snapshot models current identity and the existing synthetic sent
// facts. PostgreSQL snapshot/expiry/locking guarantees are verified on real PG.
func (s *FakeStore) outboundReceiptPrincipalLocked(a authz.Actor, scope string) (authz.Actor, error) {
	if scope != "send:read" && scope != "send:write" {
		return a, app.Forbidden("invalid outbound receipt scope")
	}
	switch a.Type {
	case authz.PrincipalUser:
		current, ok := authz.RefreshMemberActor(a, a.TenantID, s.users[a.ID])
		if !ok {
			return a, app.Forbidden("user unavailable")
		}
		return current, nil
	case authz.PrincipalAPIKey:
		k := s.apiKeys[a.ID]
		if !authz.OutboundKeyIdentityMatches(a, k) || !authz.OutboundKeyHasScope(k, scope) || k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()) {
			return a, app.Forbidden("key unavailable")
		}
		if k.OwnerUserID != nil {
			u := s.users[*k.OwnerUserID]
			if u == nil || !u.IsActive || u.TenantID != a.TenantID {
				return a, app.Forbidden("key owner unavailable")
			}
		}
		a.IsAdmin = false
		a.IsSuperAdmin = false
		a.Role = ""
		return a, nil
	default:
		return a, app.Forbidden("current outbound principal required")
	}
}

func (s *FakeStore) outboundReceiptLocked(a authz.Actor, j *models.OutboundJob) *store.OutboundReceipt {
	if j == nil || j.TenantID != a.TenantID || !a.Permission.AllowsZone(j.ZoneID) {
		return nil
	}
	if a.Type == authz.PrincipalAPIKey && !models.ZoneAllowed(s.apiKeys[a.ID].AllowedZoneIDs, j.ZoneID) {
		return nil
	}
	content, _ := s.canReadOutboundContentLocked(a, j)
	if !authz.CanAccessOwned(a, j.UserID, j.APIKeyID) && !content {
		return nil
	}
	// Read the real fixture ledger while the caller holds the same mutex as
	// principal/job/content facts. Neither recipient addresses nor caller job
	// fields manufacture outcome counts. A missing map or empty ledger remains
	// distinguishable from a known nonempty ledger at the production projector.
	rows, ledgerPresent := s.outboundRecipients[j.ID]
	addresses := make([]string, 0, len(rows))
	for address := range rows {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	states := make([]string, 0, len(addresses))
	for _, address := range addresses {
		states = append(states, rows[address].State)
	}
	return &store.OutboundReceipt{Job: cloneOutboundJob(j), ContentAllowed: content,
		RecipientStates: states, LedgerKnown: ledgerPresent && j.RecipientLedger}
}

func (s *FakeStore) GetOutboundReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.outboundReceiptPrincipalLocked(a, scope)
	if err != nil {
		return nil, err
	}
	return s.outboundReceiptLocked(current, s.outboundJobs[id]), nil
}

func (s *FakeStore) ListOutboundReceipts(ctx context.Context, a authz.Actor, page models.Page) ([]store.OutboundReceipt, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.outboundReceiptPrincipalLocked(a, "send:read")
	if err != nil {
		return nil, 0, err
	}
	all := []store.OutboundReceipt{}
	for _, j := range s.outboundJobs {
		if r := s.outboundReceiptLocked(current, j); r != nil {
			all = append(all, *r)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].Job, all[j].Job
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID.String() > b.ID.String()
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	total := len(all)
	page = page.Normalize()
	start := page.Offset()
	if start >= total {
		return []store.OutboundReceipt{}, total, nil
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}
