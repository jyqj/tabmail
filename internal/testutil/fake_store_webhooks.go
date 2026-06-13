package testutil

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) CreateOutboxEvent(_ context.Context, e *models.OutboxEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	now := time.Now().UTC()
	if cp.OccurredAt.IsZero() {
		cp.OccurredAt = now
	}
	if cp.NextAttemptAt.IsZero() {
		cp.NextAttemptAt = now
	}
	if cp.State == "" {
		cp.State = "pending"
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.outbox[cp.ID] = &cp
	e.ID = cp.ID
	return nil
}

func (s *FakeStore) ClaimOutboxEvents(_ context.Context, now time.Time, limit int) ([]*models.OutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var list []*models.OutboxEvent
	for _, e := range s.outbox {
		claimable := (e.State == "pending" || e.State == "retry") && !e.NextAttemptAt.After(now)
		expiredLease := e.State == "processing" && (e.LeaseUntil == nil || !e.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *e
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	for _, e := range list {
		stored := s.outbox[e.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.UpdatedAt = now
		e.State = stored.State
		e.Attempts = stored.Attempts
		e.ClaimedAt = stored.ClaimedAt
		e.LeaseUntil = stored.LeaseUntil
		e.UpdatedAt = stored.UpdatedAt
	}
	return list, nil
}

func (s *FakeStore) MarkOutboxEventDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.outbox[id]; ok {
		e.State = "done"
		e.ClaimedAt = nil
		e.LeaseUntil = nil
		e.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) MarkOutboxEventRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.outbox[id]; ok {
		e.State = "retry"
		e.LastError = lastError
		e.NextAttemptAt = nextAttemptAt
		e.ClaimedAt = nil
		e.LeaseUntil = nil
		e.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) CreateWebhookDeliveries(_ context.Context, event *models.OutboxEvent, urls []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, url := range urls {
		exists := false
		for _, d := range s.deliveries {
			if d.EventID == event.ID && d.URL == url {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		d := &models.WebhookDelivery{
			ID:            uuid.New(),
			EventID:       event.ID,
			URL:           url,
			EventType:     event.EventType,
			Payload:       append([]byte(nil), event.Payload...),
			State:         "pending",
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		s.deliveries[d.ID] = d
	}
	return nil
}

func (s *FakeStore) ClaimWebhookDeliveries(_ context.Context, now time.Time, limit int) ([]*models.WebhookDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	var list []*models.WebhookDelivery
	for _, d := range s.deliveries {
		claimable := (d.State == "pending" || d.State == "retry") && !d.NextAttemptAt.After(now)
		expiredLease := d.State == "processing" && (d.LeaseUntil == nil || !d.LeaseUntil.After(now))
		if claimable || expiredLease {
			cp := *d
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	for _, d := range list {
		stored := s.deliveries[d.ID]
		leaseUntil := now.Add(fakeClaimLeaseDuration)
		triedAt := now
		stored.State = "processing"
		stored.Attempts++
		stored.ClaimedAt = &now
		stored.LeaseUntil = &leaseUntil
		stored.LastTriedAt = &triedAt
		stored.UpdatedAt = now
		d.State = stored.State
		d.Attempts = stored.Attempts
		d.ClaimedAt = stored.ClaimedAt
		d.LeaseUntil = stored.LeaseUntil
		d.LastTriedAt = stored.LastTriedAt
		d.UpdatedAt = stored.UpdatedAt
	}
	return list, nil
}

func (s *FakeStore) MarkWebhookDeliveryDone(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.deliveries[id]; ok {
		now := time.Now().UTC()
		d.State = "delivered"
		d.ClaimedAt = nil
		d.LeaseUntil = nil
		d.DeliveredAt = &now
		d.UpdatedAt = now
	}
	return nil
}

func (s *FakeStore) MarkWebhookDeliveryRetry(_ context.Context, id uuid.UUID, lastError string, nextAttemptAt time.Time, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.deliveries[id]; ok {
		d.State = "retry"
		if dead {
			d.State = "dead"
		}
		d.LastError = lastError
		d.NextAttemptAt = nextAttemptAt
		d.ClaimedAt = nil
		d.LeaseUntil = nil
		d.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (s *FakeStore) ListDeadWebhookDeliveries(_ context.Context, limit int) ([]models.DeadLetter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	var out []models.DeadLetter
	for _, d := range s.deliveries {
		if d.State != "dead" {
			continue
		}
		out = append(out, models.DeadLetter{
			ID:          d.ID.String(),
			URL:         d.URL,
			EventType:   d.EventType,
			Payload:     append([]byte(nil), d.Payload...),
			Attempts:    d.Attempts,
			LastError:   d.LastError,
			CreatedAt:   d.CreatedAt,
			LastTriedAt: derefTime(d.LastTriedAt),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *FakeStore) CountDeadWebhookDeliveries(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, d := range s.deliveries {
		if d.State == "dead" {
			total++
		}
	}
	return total, nil
}

func (s *FakeStore) ListWebhookDeliveries(_ context.Context, pg models.Page, state, eventType, url string) ([]*models.WebhookDelivery, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.WebhookDelivery
	for _, d := range s.deliveries {
		if state != "" && d.State != state {
			continue
		}
		if eventType != "" && d.EventType != eventType {
			continue
		}
		if url != "" && !strings.Contains(strings.ToLower(d.URL), strings.ToLower(url)) {
			continue
		}
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= total {
		return []*models.WebhookDelivery{}, total, nil
	}
	end := start + pg.PerPage
	if end > total {
		end = total
	}
	return out[start:end], total, nil
}

func (s *FakeStore) CountWebhookDeliveriesByState(_ context.Context, states ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(states) == 0 {
		return len(s.deliveries), nil
	}
	set := make(map[string]struct{}, len(states))
	for _, state := range states {
		set[state] = struct{}{}
	}
	total := 0
	for _, d := range s.deliveries {
		if _, ok := set[d.State]; ok {
			total++
		}
	}
	return total, nil
}

func (s *FakeStore) CreateWebhookEndpoint(_ context.Context, _ *models.WebhookEndpoint) error {
	return nil
}

func (s *FakeStore) ListWebhookEndpoints(_ context.Context, _ uuid.UUID) ([]*models.WebhookEndpoint, error) {
	return nil, nil
}

func (s *FakeStore) GetWebhookEndpoint(_ context.Context, _ uuid.UUID) (*models.WebhookEndpoint, error) {
	return nil, nil
}

func (s *FakeStore) UpdateWebhookEndpoint(_ context.Context, _ *models.WebhookEndpoint) error {
	return nil
}

func (s *FakeStore) DeleteWebhookEndpoint(_ context.Context, _ uuid.UUID) error {
	return nil
}

func derefTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
