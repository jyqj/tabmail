package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// This fake supports service/HTTP regressions. Atomic SQL, fencing races and
// failure rollback are independently exercised against PostgreSQL in CI.
func (s *FakeStore) CreateIngress(_ context.Context, j *models.IngestJob, targets []store.IngressTarget, hash string, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j == nil || j.ID == uuid.Nil || len(targets) == 0 || len(hash) != 64 || size < 0 || j.RawObjectKey != "ingress-"+j.ID.String()+".eml" {
		return errors.New("invalid receipt")
	}
	if s.ingestJobs[j.ID] != nil {
		return errors.New("duplicate receipt")
	}
	for _, t := range targets {
		m := s.mailboxes[t.MailboxID]
		z := s.zones[t.ZoneID]
		if m == nil || z == nil || m.TenantID != t.TenantID || m.ZoneID != t.ZoneID || m.FullAddress != t.Address || !z.IsVerified || !z.MXVerified {
			return errors.New("destination changed")
		}
	}
	cp := *j
	now := time.Now().UTC()
	cp.State = "pending"
	cp.CreatedAt = now
	cp.UpdatedAt = now
	cp.NextAttemptAt = now
	s.ingestJobs[j.ID] = &cp
	c := &store.IngressClaim{Job: &cp, RawHash: hash, RawSize: size}
	s.ingressClaims[j.ID] = c
	ts := append([]store.IngressTarget(nil), targets...)
	for i := range ts {
		ts[i].JobID = j.ID
		ts[i].State = "pending"
	}
	s.ingressTargets[j.ID] = ts
	return nil
}
func (s *FakeStore) ClaimIngress(_ context.Context) (*store.IngressClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	var chosen *store.IngressClaim
	for _, c := range s.ingressClaims {
		j := c.Job
		ready := (j.State == "pending" || j.State == "retry") && !j.NextAttemptAt.After(now)
		expired := j.State == "processing" && (j.LeaseUntil == nil || !j.LeaseUntil.After(now))
		if (ready || expired) && (chosen == nil || j.CreatedAt.Before(chosen.Job.CreatedAt)) {
			chosen = c
		}
	}
	if chosen == nil {
		return nil, nil
	}
	j := chosen.Job
	lease := now.Add(fakeClaimLeaseDuration)
	chosen.Token = uuid.New()
	j.State = "processing"
	j.Attempts++
	j.LeaseUntil = &lease
	j.ClaimedAt = &now
	j.UpdatedAt = now
	cp := *chosen
	job := *j
	cp.Job = &job
	return &cp, nil
}
func (s *FakeStore) ListIngressTargets(_ context.Context, id uuid.UUID) ([]store.IngressTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.IngressTarget(nil), s.ingressTargets[id]...), nil
}
func (s *FakeStore) validIngress(c *store.IngressClaim) bool {
	if c == nil || c.Job == nil || c.Token == uuid.Nil {
		return false
	}
	current := s.ingressClaims[c.Job.ID]
	return current != nil && current.Token == c.Token && current.Job.State == "processing" && current.Job.LeaseUntil != nil && current.Job.LeaseUntil.After(time.Now()) && current.Job.RawObjectKey == c.Job.RawObjectKey
}
func (s *FakeStore) DeliverIngress(_ context.Context, c *store.IngressClaim, m *models.Message, maxMailbox, daily int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validIngress(c) {
		return false, store.ErrIngressClaim
	}
	ts := s.ingressTargets[c.Job.ID]
	for i := range ts {
		t := &ts[i]
		if t.MailboxID != m.MailboxID {
			continue
		}
		if t.TenantID != m.TenantID || t.ZoneID != m.ZoneID || m.RawObjectKey != c.Job.RawObjectKey {
			return false, errors.New("destination/content mismatch")
		}
		if t.State == "delivered" {
			return false, nil
		}
		if t.State != "pending" {
			return false, errors.New("target held")
		}
		mb := s.mailboxes[t.MailboxID]
		if mb == nil || mb.FullAddress != t.Address {
			return false, errors.New("destination missing")
		}
		day := t.TenantID.String() + time.Now().UTC().Format("20060102")
		if (maxMailbox > 0 && int(mb.MessageCount) >= maxMailbox) || (daily > 0 && s.ingressUsage[day] >= daily) {
			return false, store.ErrIngressQuota
		}
		m.ID = uuid.New()
		m.ReceivedAt = c.Job.CreatedAt
		cp := *m
		s.messages[m.ID] = &cp
		mb.MessageCount++
		s.ingressUsage[day]++
		id := m.ID
		t.MessageID = &id
		t.State = "delivered"
		t.Attempts++
		t.LastError = ""
		tenant := m.TenantID
		s.audits = append(s.audits, &models.AuditEntry{TenantID: &tenant, Actor: "ingest:" + c.Job.ID.String(), Action: "message.received", ResourceType: "message", ResourceID: &id})
		payload, _ := json.Marshal(map[string]string{"type": "message.received", "tenant_id": tenant.String(), "message_id": id.String(), "mailbox": t.Address})
		event := &models.OutboxEvent{ID: uuid.New(), EventType: "message.received", Payload: payload, State: "pending"}
		s.outbox[event.ID] = event
		return true, nil
	}
	return false, errors.New("target missing")
}
func (s *FakeStore) FailIngressTarget(_ context.Context, c *store.IngressClaim, mailbox uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failIngress(c, mailbox, reason, false)
}
func (s *FakeStore) HoldIngressTarget(_ context.Context, c *store.IngressClaim, mailbox uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failIngress(c, mailbox, reason, true)
}
func (s *FakeStore) failIngress(c *store.IngressClaim, mailbox uuid.UUID, reason string, held bool) error {
	if !s.validIngress(c) {
		return store.ErrIngressClaim
	}
	ts := s.ingressTargets[c.Job.ID]
	for i := range ts {
		t := &ts[i]
		if t.MailboxID == mailbox && t.State == "pending" {
			t.Attempts++
			t.LastError = reason
			if held {
				t.State = "held"
			}
		}
	}
	return nil
}
func (s *FakeStore) FinishIngress(_ context.Context, c *store.IngressClaim, maxAttempts int, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validIngress(c) {
		return store.ErrIngressClaim
	}
	ts := s.ingressTargets[c.Job.ID]
	if len(ts) == 0 {
		return fmt.Errorf("missing ledger")
	}
	remaining, pending := 0, 0
	for _, t := range ts {
		if t.State != "delivered" {
			remaining++
		}
		if t.State == "pending" {
			pending++
		}
	}
	j := s.ingestJobs[c.Job.ID]
	j.State = "done"
	j.LastError = ""
	if remaining > 0 {
		j.State = "retry"
		j.LastError = "Destinations remain undelivered; original retained"
		if pending == 0 || j.Attempts >= maxAttempts {
			j.State = "dead"
			for i := range ts {
				if ts[i].State == "pending" {
					ts[i].State = "held"
				}
			}
		}
	}
	j.NextAttemptAt = next
	j.ClaimedAt = nil
	j.LeaseUntil = nil
	j.UpdatedAt = time.Now().UTC()
	s.ingressClaims[j.ID].Token = uuid.Nil
	return nil
}
