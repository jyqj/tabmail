package testutil

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) GetSetting(_ context.Context, key string) (*models.SystemSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return nil, nil
	}
	ss, ok := s.settings[key]
	if !ok {
		return nil, nil
	}
	cp := *ss
	return &cp, nil
}

func (s *FakeStore) UpsertSetting(_ context.Context, key, value, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		s.settings = map[string]*models.SystemSetting{}
	}
	s.settings[key] = &models.SystemSetting{Key: key, Value: value, Description: description}
	return nil
}

func (s *FakeStore) ListSettings(_ context.Context) ([]*models.SystemSetting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return nil, nil
	}
	var out []*models.SystemSetting
	for _, ss := range s.settings {
		cp := *ss
		out = append(out, &cp)
	}
	return out, nil
}

func (s *FakeStore) GetSMTPPolicy(_ context.Context) (*models.SMTPPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.smtpPolicy == nil {
		return nil, nil
	}
	cp := *s.smtpPolicy
	cp.AcceptDomains = append([]string(nil), cp.AcceptDomains...)
	cp.RejectDomains = append([]string(nil), cp.RejectDomains...)
	cp.StoreDomains = append([]string(nil), cp.StoreDomains...)
	cp.DiscardDomains = append([]string(nil), cp.DiscardDomains...)
	cp.RejectOriginDomains = append([]string(nil), cp.RejectOriginDomains...)
	return &cp, nil
}

func (s *FakeStore) UpsertSMTPPolicy(_ context.Context, p *models.SMTPPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	cp.UpdatedAt = time.Now().UTC()
	cp.AcceptDomains = append([]string(nil), cp.AcceptDomains...)
	cp.RejectDomains = append([]string(nil), cp.RejectDomains...)
	cp.StoreDomains = append([]string(nil), cp.StoreDomains...)
	cp.DiscardDomains = append([]string(nil), cp.DiscardDomains...)
	cp.RejectOriginDomains = append([]string(nil), cp.RejectOriginDomains...)
	s.smtpPolicy = &cp
	p.UpdatedAt = cp.UpdatedAt
	return nil
}

func (s *FakeStore) InsertAudit(_ context.Context, e *models.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	s.audits = append(s.audits, &cp)
	return nil
}

func (s *FakeStore) ListAuditEntries(_ context.Context, limit int) ([]*models.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.audits) {
		limit = len(s.audits)
	}
	out := make([]*models.AuditEntry, 0, limit)
	for i := len(s.audits) - 1; i >= 0 && len(out) < limit; i-- {
		cp := *s.audits[i]
		out = append(out, &cp)
	}
	return out, nil
}

func (f *FakeStore) ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *FakeStore) CreateMonitorEvent(_ context.Context, e *models.MonitorEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.At.IsZero() {
		cp.At = time.Now().UTC()
	}
	s.monitor = append(s.monitor, &cp)
	return nil
}

func (s *FakeStore) ListMonitorEvents(_ context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var filtered []*models.MonitorEvent
	for _, e := range s.monitor {
		if eventType != "" && e.Type != eventType {
			continue
		}
		if mailbox != "" && !strings.Contains(strings.ToLower(e.Mailbox), strings.ToLower(mailbox)) {
			continue
		}
		if sender != "" && !strings.Contains(strings.ToLower(e.Sender), strings.ToLower(sender)) {
			continue
		}
		cp := *e
		filtered = append(filtered, &cp)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].At.After(filtered[j].At) })
	total := len(filtered)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= len(filtered) {
		return []*models.MonitorEvent{}, total, nil
	}
	end := start + pg.PerPage
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], total, nil
}

func (s *FakeStore) CreateIngestJob(_ context.Context, job *models.IngestJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *job
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := time.Now().UTC()
	if cp.NextAttemptAt.IsZero() {
		cp.NextAttemptAt = now
	}
	if cp.State == "" {
		cp.State = "pending"
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.ingestJobs[cp.ID] = &cp
	job.ID = cp.ID
	return nil
}

func (s *FakeStore) ClaimIngestJobs(_ context.Context, now time.Time, limit int) ([]*models.IngestJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var jobs []*models.IngestJob
	for _, job := range s.ingestJobs {
		claimable := (job.State == "pending" || job.State == "retry") && !job.NextAttemptAt.After(now)
		expiredLease := job.State == "processing" && (job.LeaseUntil == nil || !job.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *job
			jobs = append(jobs, &cp)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	for _, job := range jobs {
		stored := s.ingestJobs[job.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.UpdatedAt = now
		job.State = stored.State
		job.Attempts = stored.Attempts
		job.ClaimedAt = stored.ClaimedAt
		job.LeaseUntil = stored.LeaseUntil
		job.UpdatedAt = stored.UpdatedAt
	}
	return jobs, nil
}

func (s *FakeStore) MarkIngestJobDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.State = "done"
		job.ClaimedAt = nil
		job.LeaseUntil = nil
		job.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) MarkIngestJobRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.State = "retry"
		if dead {
			job.State = "dead"
		}
		job.LastError = lastError
		job.NextAttemptAt = nextAttemptAt
		job.ClaimedAt = nil
		job.LeaseUntil = nil
		job.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) PurgeOldIngestJobs(_ context.Context, before time.Time, limit int) (int, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var toDelete []uuid.UUID
	var keys []string
	for id, job := range s.ingestJobs {
		if (job.State == "done" || job.State == "dead") && job.UpdatedAt.Before(before) {
			toDelete = append(toDelete, id)
			if job.RawObjectKey != "" {
				keys = append(keys, job.RawObjectKey)
			}
			if len(toDelete) >= limit {
				break
			}
		}
	}
	for _, id := range toDelete {
		delete(s.ingestJobs, id)
	}
	return len(toDelete), keys, nil
}

func (s *FakeStore) ListIngestJobs(_ context.Context, pg models.Page, state, source, recipient string) ([]*models.IngestJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.IngestJob
	for _, job := range s.ingestJobs {
		if state != "" && job.State != state {
			continue
		}
		if source != "" && job.Source != source {
			continue
		}
		if recipient != "" {
			match := false
			for _, rcpt := range job.Recipients {
				if strings.EqualFold(rcpt, recipient) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		cp := *job
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= total {
		return []*models.IngestJob{}, total, nil
	}
	end := start + pg.PerPage
	if end > total {
		end = total
	}
	return out[start:end], total, nil
}

func (s *FakeStore) CountIngestJobsByState(_ context.Context, states ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(states) == 0 {
		return len(s.ingestJobs), nil
	}
	set := make(map[string]struct{}, len(states))
	for _, state := range states {
		set[state] = struct{}{}
	}
	total := 0
	for _, job := range s.ingestJobs {
		if _, ok := set[job.State]; ok {
			total++
		}
	}
	return total, nil
}

func (s *FakeStore) ForceIngestJobUpdatedAt(id uuid.UUID, updatedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.ingestJobs[id]; ok {
		job.UpdatedAt = updatedAt
	}
}
