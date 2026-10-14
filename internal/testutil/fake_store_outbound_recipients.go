package testutil

import (
	"context"
	"github.com/google/uuid"
	"sort"
	"strings"
	"tabmail/internal/company"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"time"
)

// Called by every enqueue path so tests exercise the production recipient loop.
func (s *FakeStore) initializeRecipientLedgerLocked(job *models.OutboundJob) {
	if s.outboundRecipients == nil {
		s.outboundRecipients = map[uuid.UUID]map[string]company.Recipient{}
	}
	rows := map[string]company.Recipient{}
	for _, address := range job.RcptTo {
		rows[address] = company.Recipient{Address: address, State: delivery.Pending, UpdatedAt: job.CreatedAt}
	}
	s.outboundRecipients[job.ID] = rows
}

func (s *FakeStore) ListOutboundRecipients(ctx context.Context, tenant, job uuid.UUID) ([]company.Recipient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []company.Recipient{}
	j := s.outboundJobs[job]
	if j == nil || j.TenantID != tenant {
		return out, nil
	}
	for _, r := range s.outboundRecipients[job] {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}

func (s *FakeStore) BeginOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.validOutbound(id, token)
	if j == nil || !j.RecipientLedger || j.InFlightDomain != "" || address == "" {
		return false, errFakeDeliveryToken
	}
	r, ok := s.outboundRecipients[id][address]
	if !ok || !delivery.CanTransitionRecipient(r.State, delivery.Uncertain) {
		return false, nil
	}
	r.State = delivery.Uncertain
	r.Attempts++
	r.Diagnostic = "SMTP attempt started; outcome pending"
	r.UpdatedAt = time.Now().UTC()
	s.outboundRecipients[id][address] = r
	j.InFlightDomain = "rcpt:" + address
	j.UpdatedAt = r.UpdatedAt
	return true, nil
}

func (s *FakeStore) CompleteOutboundRecipient(ctx context.Context, id uuid.UUID, token *uuid.UUID, address, state string, code int, diagnostic string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.validOutbound(id, token)
	if !delivery.IsCompletion(state) || j == nil || !j.RecipientLedger || j.InFlightDomain != "rcpt:"+address {
		return errFakeDeliveryToken
	}
	r, ok := s.outboundRecipients[id][address]
	if !ok || !delivery.CanTransitionRecipient(r.State, state) {
		return errFakeDeliveryToken
	}
	runes := []rune(strings.ToValidUTF8(diagnostic, "?"))
	if len(runes) > 2000 {
		runes = runes[:2000]
	}
	r.State = state
	r.SMTPCode = code
	r.Diagnostic = string(runes)
	r.UpdatedAt = time.Now().UTC()
	s.outboundRecipients[id][address] = r
	j.InFlightDomain = ""
	j.UpdatedAt = r.UpdatedAt
	return nil
}
