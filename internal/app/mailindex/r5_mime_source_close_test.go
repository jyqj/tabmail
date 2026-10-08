package mailindex

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
)

type r5MIMECloseIndexRepo struct {
	job               company.MailIndexJob
	failed, completed int
}

func (r *r5MIMECloseIndexRepo) ClaimMailIndexJobs(context.Context, int) ([]company.MailIndexJob, error) {
	return []company.MailIndexJob{r.job}, nil
}
func (r *r5MIMECloseIndexRepo) CompleteMailIndexJob(_ context.Context, _ company.MailIndexJob, doc company.ParsedMessage) error {
	r.completed++
	if doc.TextBody != "owned body" {
		return errors.New("unexpected healthy parsed content")
	}
	return nil
}
func (r *r5MIMECloseIndexRepo) FailMailIndexJob(_ context.Context, _ company.MailIndexJob, reason string) error {
	if reason != "parse_failed" {
		return errors.New("unexpected owned source failure classification")
	}
	r.failed++
	return nil
}

type r5MIMECloseIndexObjects struct{ opens, closes atomic.Int32 }
type r5MIMECloseIndexReader struct {
	io.Reader
	close func() error
}

func (r *r5MIMECloseIndexReader) Close() error { return r.close() }
func (o *r5MIMECloseIndexObjects) Get(context.Context, string) (io.ReadCloser, error) {
	attempt := o.opens.Add(1)
	return &r5MIMECloseIndexReader{Reader: strings.NewReader("Subject: owned index\r\n\r\nowned body"), close: func() error {
		o.closes.Add(1)
		if attempt == 1 {
			return errors.New("owned source close failed")
		}
		return nil
	}}, nil
}

func TestR5MIMESourceCloseDoesNotCompleteMailIndex(t *testing.T) {
	repo := &r5MIMECloseIndexRepo{job: company.MailIndexJob{MessageID: uuid.New(), SourceKey: "owned-index-source"}}
	objects := &r5MIMECloseIndexObjects{}
	service := New(repo, objects, zerolog.Nop())
	if count, err := service.Batch(context.Background()); count != 1 || err != nil {
		t.Fatalf("failed source did not follow existing index failure policy: count=%d err=%v", count, err)
	}
	if repo.failed != 1 || repo.completed != 0 {
		t.Errorf("failed object close completed derived index: failed=%d completed=%d", repo.failed, repo.completed)
	}
	if count, err := service.Batch(context.Background()); count != 1 || err != nil {
		t.Fatalf("healthy index retry failed: count=%d err=%v", count, err)
	}
	if repo.failed != 1 || repo.completed != 1 || objects.opens.Load() != 2 || objects.closes.Load() != 2 {
		t.Errorf("index retry reused failed cached source: failed=%d completed=%d opens=%d closes=%d", repo.failed, repo.completed, objects.opens.Load(), objects.closes.Load())
	}
}
