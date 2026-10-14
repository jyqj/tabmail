package testutil

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

var errFakeDeliveryToken = store.ErrDeliveryTokenMismatch

func (s *FakeStore) ClaimOutboundJobs(_ context.Context, _ time.Time, _ int) ([]*models.OutboundJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	var chosen *models.OutboundJob
	for _, j := range s.outboundJobs {
		expired := j.State == models.OutboundProcessing && (j.LeaseUntil == nil || !j.LeaseUntil.After(now))
		if expired && j.InFlightDomain != "" {
			j.State = models.OutboundFailed
			j.LastError = "acceptance uncertain"
			j.DeliveryToken = nil
			j.LeaseUntil = nil
			continue
		}
		ready := (j.State == models.OutboundPending || j.State == models.OutboundRetry) && !j.NextAttemptAt.After(now)
		if j.InFlightDomain == "" && (ready || expired) && (chosen == nil || j.CreatedAt.Before(chosen.CreatedAt)) {
			chosen = j
		}
	}
	if chosen == nil {
		return nil, nil
	}
	token := uuid.New()
	lease := now.Add(5 * time.Minute)
	chosen.State = models.OutboundProcessing
	chosen.Attempts++
	chosen.DeliveryToken = &token
	chosen.LeaseUntil = &lease
	chosen.ClaimedAt = &now
	return []*models.OutboundJob{cloneOutboundJob(chosen)}, nil
}
func (s *FakeStore) validOutbound(id uuid.UUID, token *uuid.UUID) *models.OutboundJob {
	j := s.outboundJobs[id]
	if !delivery.LeaseOwned(j, token, time.Now()) {
		return nil
	}
	return j
}
func finishFakeOutbound(j *models.OutboundJob, state models.OutboundState, reason string) {
	j.State = state
	j.LastError = reason
	j.DeliveryToken = nil
	j.LeaseUntil = nil
	j.ClaimedAt = nil
	j.UpdatedAt = time.Now().UTC()
}
func (s *FakeStore) MarkOutboundJobSent(_ context.Context, id uuid.UUID, token *uuid.UUID, code int, response, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.validOutbound(id, token)
	if j == nil || j.InFlightDomain != "" {
		return errFakeDeliveryToken
	}
	j.SMTPCode = &code
	j.SMTPResponse = response
	j.MessageIDHeader = messageID
	state, err := delivery.FinalState(delivery.FinishSent, j.InFlightDomain != "")
	if err != nil {
		return err
	}
	finishFakeOutbound(j, state, "")
	return nil
}
func (s *FakeStore) MarkOutboundJobRetry(_ context.Context, id uuid.UUID, token *uuid.UUID, reason string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.validOutbound(id, token)
	if j == nil {
		return errFakeDeliveryToken
	}
	state, err := delivery.FinalState(delivery.FinishRetry, j.InFlightDomain != "")
	if err != nil {
		return err
	}
	if j.InFlightDomain != "" {
		reason = delivery.UncertainRetryPrefix + reason
	}
	j.NextAttemptAt = next
	finishFakeOutbound(j, state, reason)
	return nil
}
func (s *FakeStore) MarkOutboundJobFailed(_ context.Context, id uuid.UUID, token *uuid.UUID, reason string, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.validOutbound(id, token)
	if j == nil {
		return errFakeDeliveryToken
	}
	event := delivery.FinishFailed
	if dead {
		event = delivery.FinishDead
	}
	state, err := delivery.FinalState(event, j.InFlightDomain != "")
	if err != nil {
		return err
	}
	finishFakeOutbound(j, state, reason)
	return nil
}
