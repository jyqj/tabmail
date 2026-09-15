package mailboxapp

import (
 "context"
 "github.com/google/uuid"
 "tabmail/internal/models"
)

// The existing mailbox lookup tests deliberately have no user records. Their
// cross-tenant rejection happens before owner resolution; full owner lifecycle
// behaviour is covered by the stateful FakeStore router and PostgreSQL tests.
func (s *mailboxTestStore) GetUser(context.Context, uuid.UUID) (*models.User, error) {
 return nil, nil
}
