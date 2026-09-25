// Package mailarchive exposes mailbox assets independently of delivery queues.
package mailarchive

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type Service struct{ repo company.SentArchive }

func New(repo company.SentArchive) *Service { return &Service{repo: repo} }
func (s *Service) List(ctx context.Context, a authz.Actor, mailbox uuid.UUID, folder, query string, page models.Page) ([]company.ArchivedMail, int, error) {
	return s.repo.ListArchivedMail(ctx, a, mailbox, folder, query, page)
}
func (s *Service) Change(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID, revision int64, action string) error {
	return s.repo.MutateArchivedMail(ctx, a, mailbox, id, revision, action)
}
