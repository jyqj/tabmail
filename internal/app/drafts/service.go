// Package drafts owns revisioned personal editing, separate from delivery.
package drafts

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type Repository interface {
	company.DraftWorkspace
	company.DraftQuery
}
type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) List(ctx context.Context, a authz.Actor, p models.Page) ([]company.Draft, int, error) {
	return s.repo.ListMailDraftPage(ctx, a, p)
}
func (s *Service) Get(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.Draft, error) {
	return s.repo.GetMailDraft(ctx, a, id)
}
func (s *Service) Save(ctx context.Context, a authz.Actor, d company.Draft, create bool) (*company.Draft, error) {
	if create && d.Revision != 0 {
		return nil, app.BadRequest("new draft revision must be zero")
	}
	if !create && (d.ID == uuid.Nil || d.Revision < 1) {
		return nil, app.BadRequest("positive draft revision required")
	}
	return s.repo.SaveMailDraft(ctx, a, d)
}
func (s *Service) Delete(ctx context.Context, a authz.Actor, id uuid.UUID, revision int) error {
	if revision < 1 {
		return app.BadRequest("positive draft revision required")
	}
	return s.repo.DeleteMailDraft(ctx, a, id, revision)
}
