// Package mailindex coordinates fenced, rebuildable content indexing. It never
// changes the canonical EML, mailbox state or SMTP acceptance semantics.
package mailindex

import (
	"context"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"time"
)

type Service struct {
	repo   company.ContentIndexer
	parser *mailcontent.Parser
	logger zerolog.Logger
}

func New(repo company.ContentIndexer, objects mailcontent.ObjectReader, l zerolog.Logger) *Service {
	return &Service{repo: repo, parser: mailcontent.New(objects), logger: l.With().Str("component", "mail_index").Logger()}
}
func (s *Service) Batch(ctx context.Context) (int, error) {
	jobs, e := s.repo.ClaimMailIndexJobs(ctx, 1)
	if e != nil {
		return 0, e
	}
	for _, j := range jobs {
		d, e := s.parser.Document(ctx, j.MessageID, j.SourceKey)
		if e != nil {
			if e = s.repo.FailMailIndexJob(ctx, j, "parse_failed"); e != nil {
				return 0, e
			}
			continue
		}
		if e = s.repo.CompleteMailIndexJob(ctx, j, *d); e != nil {
			return 0, e
		}
	}
	return len(jobs), nil
}
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		n, e := s.Batch(ctx)
		if e != nil && ctx.Err() == nil {
			s.logger.Warn().Msg("content index batch incomplete; lease/retry will recover")
		}
		if n > 0 && e == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
