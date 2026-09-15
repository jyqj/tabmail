package testutil

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) GetMailboxGrant(_ context.Context, tenant, mailbox, user uuid.UUID) (*models.MailboxGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.mailboxGrants[[2]uuid.UUID{mailbox, user}]; g != nil && g.TenantID == tenant {
		cp := *g
		return &cp, nil
	}
	return nil, nil
}
func (s *FakeStore) SetMailboxGrant(_ context.Context, g *models.MailboxGrant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g == nil || (g.CanOrganize && !g.CanRead) || (g.TemplateOnly && !g.CanSend) {
		return errors.New("invalid grant")
	}
	mb := s.mailboxes[g.MailboxID]
	u := s.users[g.UserID]
	if mb == nil || u == nil || mb.TenantID != g.TenantID || u.TenantID != g.TenantID {
		return errors.New("cross-tenant grant")
	}
	cp := *g
	s.mailboxGrants[[2]uuid.UUID{g.MailboxID, g.UserID}] = &cp
	return nil
}
