package testutil

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func (s *FakeStore) CreateOutboundJob(_ context.Context, job *models.OutboundJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createOutboundJobLocked(job)
	return nil
}

func (s *FakeStore) CreateOutboundJobWithQuota(_ context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q := quota.UserDaily; q != nil && q.Limit > 0 {
		if s.countOutboundSinceLocked(job.TenantID, q.UserID, q.Since) >= q.Limit {
			return store.ErrOutboundDailyQuotaExceeded
		}
	}
	if q := quota.SendAsDaily; q != nil && q.Limit > 0 {
		if s.countOutboundByIdentitySinceLocked(job.TenantID, q.PrincipalType, q.PrincipalID, q.IdentityID, q.Since) >= q.Limit {
			return store.ErrSendAsDailyQuotaExceeded
		}
	}
	s.createOutboundJobLocked(job)
	return nil
}

func (s *FakeStore) createOutboundJobLocked(job *models.OutboundJob) {
	cp := cloneOutboundJob(job)
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := time.Now()
	if cp.State == "" {
		cp.State = models.OutboundPending
	}
	if cp.NextAttemptAt.IsZero() {
		cp.NextAttemptAt = now
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = cp.CreatedAt
	}
	s.outboundJobs[cp.ID] = cp
	job.ID = cp.ID
	job.State = cp.State
	job.NextAttemptAt = cp.NextAttemptAt
	job.CreatedAt = cp.CreatedAt
	job.UpdatedAt = cp.UpdatedAt
}

func (s *FakeStore) GetOutboundJob(_ context.Context, id uuid.UUID) (*models.OutboundJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.outboundJobs[id]
	if job == nil {
		return nil, nil
	}
	return cloneOutboundJob(job), nil
}

func (s *FakeStore) ListOutboundJobs(_ context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listOutboundJobsLocked(pg, func(job *models.OutboundJob) bool {
		return job.TenantID == tenantID
	})
}

func (s *FakeStore) ClaimOutboundJobs(_ context.Context, _ time.Time, _ int) ([]*models.OutboundJob, error) {
	return nil, nil
}

func (s *FakeStore) MarkOutboundJobSent(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ int, _, _ string) error {
	return nil
}

func (s *FakeStore) MarkOutboundJobRetry(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ time.Time) error {
	return nil
}

func (s *FakeStore) MarkOutboundJobFailed(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ bool) error {
	return nil
}

func (s *FakeStore) CountOutboundSince(_ context.Context, tenantID uuid.UUID, userID *uuid.UUID, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countOutboundSinceLocked(tenantID, userID, since), nil
}

func (s *FakeStore) CountOutboundByIdentitySince(_ context.Context, tenantID uuid.UUID, principalType string, principalID uuid.UUID, identityID uuid.UUID, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countOutboundByIdentitySinceLocked(tenantID, principalType, principalID, identityID, since), nil
}

func (s *FakeStore) ListOutboundJobsByUser(_ context.Context, tenantID uuid.UUID, userID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listOutboundJobsLocked(pg, func(job *models.OutboundJob) bool {
		return job.TenantID == tenantID && job.UserID != nil && *job.UserID == userID
	})
}

func (s *FakeStore) ListOutboundJobsByAPIKey(_ context.Context, tenantID uuid.UUID, apiKeyID uuid.UUID, pg models.Page) ([]*models.OutboundJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listOutboundJobsLocked(pg, func(job *models.OutboundJob) bool {
		return job.TenantID == tenantID && job.APIKeyID != nil && *job.APIKeyID == apiKeyID
	})
}

func (s *FakeStore) RequeueOutboundJob(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.outboundJobs[id]
	if job == nil || (job.State != models.OutboundDead && job.State != models.OutboundFailed) {
		return nil
	}
	now := time.Now()
	job.State = models.OutboundPending
	job.LastError = ""
	job.NextAttemptAt = now
	job.ClaimedAt = nil
	job.LeaseUntil = nil
	job.DeliveryToken = nil
	job.UpdatedAt = now
	return nil
}

func (s *FakeStore) CreateOutboundAttempt(_ context.Context, a *models.OutboundAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *a
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	s.outboundAttempts[cp.ID] = &cp
	a.ID = cp.ID
	return nil
}

func (s *FakeStore) ListOutboundAttempts(_ context.Context, jobID uuid.UUID) ([]*models.OutboundAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.OutboundAttempt
	for _, a := range s.outboundAttempts {
		if a.JobID == jobID {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Attempt < out[j].Attempt })
	return out, nil
}

func (s *FakeStore) CreateSendIdentity(_ context.Context, si *models.SendIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *si
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	// Check uniqueness: (tenant_id, address, identity_type)
	for _, existing := range s.sendIdentities {
		if existing.TenantID == cp.TenantID && existing.Address == cp.Address && existing.IdentityType == cp.IdentityType {
			return errors.New("duplicate send identity")
		}
	}
	s.sendIdentities[cp.ID] = &cp
	si.ID = cp.ID
	si.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) GetSendIdentity(_ context.Context, id uuid.UUID) (*models.SendIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if si, ok := s.sendIdentities[id]; ok {
		cp := *si
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListSendIdentities(_ context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.SendIdentity
	for _, si := range s.sendIdentities {
		if si.TenantID == tenantID {
			out = append(out, si)
		}
	}
	return out, nil
}

func (s *FakeStore) ListSendIdentitiesByZone(_ context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.SendIdentity
	for _, si := range s.sendIdentities {
		if si.ZoneID == zoneID {
			cp := *si
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *FakeStore) FindSendIdentityForAddress(_ context.Context, tenantID uuid.UUID, address string) (*models.SendIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Try exact match first.
	for _, si := range s.sendIdentities {
		if si.TenantID == tenantID && si.Address == address && si.IdentityType == models.SendIdentityExact {
			cp := *si
			return &cp, nil
		}
	}
	// Try domain_wildcard.
	idx := strings.LastIndex(address, "@")
	if idx < 0 {
		return nil, nil
	}
	domain := address[idx+1:]
	wildcardAddr := "*@" + domain
	for _, si := range s.sendIdentities {
		if si.TenantID == tenantID && si.Address == wildcardAddr && si.IdentityType == models.SendIdentityDomainWildcard {
			cp := *si
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *FakeStore) UpdateSendIdentitiesVerifiedByZone(_ context.Context, zoneID uuid.UUID, verified bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, si := range s.sendIdentities {
		if si.ZoneID == zoneID {
			si.Verified = verified
		}
	}
	return nil
}

func (s *FakeStore) DeleteSendIdentity(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sendIdentities, id)
	return nil
}

func (s *FakeStore) AddSuppression(_ context.Context, e *models.SuppressionEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	cp.Address = strings.ToLower(cp.Address)
	for _, existing := range s.suppressions {
		if existing.TenantID == cp.TenantID && existing.Address == cp.Address {
			e.ID = existing.ID
			return nil
		}
	}
	s.suppressions[cp.ID] = &cp
	e.ID = cp.ID
	e.CreatedAt = cp.CreatedAt
	return nil
}

func (s *FakeStore) IsSuppressed(_ context.Context, tenantID uuid.UUID, address string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address = strings.ToLower(address)
	for _, e := range s.suppressions {
		if e.TenantID == tenantID && e.Address == address {
			return true, nil
		}
	}
	return false, nil
}

func (s *FakeStore) ListSuppressions(_ context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.SuppressionEntry, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pg = pg.Normalize()
	var items []*models.SuppressionEntry
	for _, e := range s.suppressions {
		if e.TenantID == tenantID {
			cp := *e
			if e.SourceJobID != nil {
				id := *e.SourceJobID
				cp.SourceJobID = &id
			}
			items = append(items, &cp)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	total := len(items)
	start := pg.Offset()
	if start >= len(items) {
		return []*models.SuppressionEntry{}, total, nil
	}
	end := start + pg.PerPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func (s *FakeStore) DeleteSuppression(_ context.Context, tenantID uuid.UUID, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.suppressions[id]; e != nil && e.TenantID == tenantID {
		delete(s.suppressions, id)
	}
	return nil
}

func cloneOutboundJob(job *models.OutboundJob) *models.OutboundJob {
	if job == nil {
		return nil
	}
	cp := *job
	if job.UserID != nil {
		id := *job.UserID
		cp.UserID = &id
	}
	if job.APIKeyID != nil {
		id := *job.APIKeyID
		cp.APIKeyID = &id
	}
	if job.RcptTo != nil {
		cp.RcptTo = append([]string(nil), job.RcptTo...)
	}
	if job.HeadersJSON != nil {
		cp.HeadersJSON = append([]byte(nil), job.HeadersJSON...)
	}
	if job.RawMIME != nil {
		cp.RawMIME = append([]byte(nil), job.RawMIME...)
	}
	if job.ClaimedAt != nil {
		v := *job.ClaimedAt
		cp.ClaimedAt = &v
	}
	if job.LeaseUntil != nil {
		v := *job.LeaseUntil
		cp.LeaseUntil = &v
	}
	if job.SMTPCode != nil {
		v := *job.SMTPCode
		cp.SMTPCode = &v
	}
	if job.DeliveryToken != nil {
		id := *job.DeliveryToken
		cp.DeliveryToken = &id
	}
	return &cp
}

func (s *FakeStore) countOutboundSinceLocked(tenantID uuid.UUID, userID *uuid.UUID, since time.Time) int {
	count := 0
	for _, job := range s.outboundJobs {
		if job.TenantID != tenantID || job.CreatedAt.Before(since) {
			continue
		}
		if userID != nil {
			if job.UserID == nil || *job.UserID != *userID {
				continue
			}
		}
		count++
	}
	return count
}

func (s *FakeStore) countOutboundByIdentitySinceLocked(tenantID uuid.UUID, principalType string, principalID uuid.UUID, identityID uuid.UUID, since time.Time) int {
	identity := s.sendIdentities[identityID]
	if identity == nil {
		return 0
	}
	count := 0
	for _, job := range s.outboundJobs {
		if job.TenantID != tenantID || job.CreatedAt.Before(since) || !sendIdentityMatchesAddress(identity, job.MailFrom) {
			continue
		}
		if principalType == "user" {
			if job.UserID == nil || *job.UserID != principalID {
				continue
			}
		} else {
			if job.APIKeyID == nil || *job.APIKeyID != principalID {
				continue
			}
		}
		count++
	}
	return count
}

func sendIdentityMatchesAddress(identity *models.SendIdentity, address string) bool {
	if identity.IdentityType == models.SendIdentityExact {
		return address == identity.Address
	}
	if identity.IdentityType == models.SendIdentityDomainWildcard && strings.HasPrefix(identity.Address, "*@") {
		idx := strings.LastIndex(address, "@")
		return idx >= 0 && address[idx+1:] == strings.TrimPrefix(identity.Address, "*@")
	}
	return false
}

func (s *FakeStore) listOutboundJobsLocked(pg models.Page, keep func(*models.OutboundJob) bool) ([]*models.OutboundJob, int, error) {
	pg = pg.Normalize()
	items := make([]*models.OutboundJob, 0, len(s.outboundJobs))
	for _, job := range s.outboundJobs {
		if keep(job) {
			items = append(items, cloneOutboundJob(job))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	start := pg.Offset()
	if start >= len(items) {
		return []*models.OutboundJob{}, total, nil
	}
	end := start + pg.PerPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}
