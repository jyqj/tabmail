package mailindex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
)

const r5IndexRaw = "From: sender@example.test\r\nSubject: index fixture\r\nContent-Type: text/plain\r\n\r\nindex body"
const r5IndexBadMIME = "Subject broken no colon\r\n\r\nbody"

type r5IndexOutcome struct {
	job      company.MailIndexJob
	document company.ParsedMessage
	reason   string
}

type r5IndexRepository struct {
	claim       func(context.Context, int) ([]company.MailIndexJob, error)
	complete    func(context.Context) error
	fail        func(context.Context) error
	claims      int
	completions []r5IndexOutcome
	failures    []r5IndexOutcome
}

func (r *r5IndexRepository) ClaimMailIndexJobs(ctx context.Context, limit int) ([]company.MailIndexJob, error) {
	r.claims++
	return r.claim(ctx, limit)
}
func (r *r5IndexRepository) CompleteMailIndexJob(ctx context.Context, job company.MailIndexJob, document company.ParsedMessage) error {
	r.completions = append(r.completions, r5IndexOutcome{job: job, document: document})
	if r.complete != nil {
		return r.complete(ctx)
	}
	return nil
}
func (r *r5IndexRepository) FailMailIndexJob(ctx context.Context, job company.MailIndexJob, reason string) error {
	r.failures = append(r.failures, r5IndexOutcome{job: job, reason: reason})
	if r.fail != nil {
		return r.fail(ctx)
	}
	return nil
}

type r5IndexObjects struct {
	get   func(context.Context, string) (io.ReadCloser, error)
	opens atomic.Int32
}

func (o *r5IndexObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	o.opens.Add(1)
	return o.get(ctx, key)
}

func r5IndexJob() company.MailIndexJob {
	return company.MailIndexJob{TenantID: uuid.New(), MessageID: uuid.New(), SourceKey: "synthetic/raw.eml", Token: uuid.New(), LeaseUntil: time.Now().Add(time.Minute)}
}

func r5IndexRepo(t *testing.T, job company.MailIndexJob) *r5IndexRepository {
	t.Helper()
	return &r5IndexRepository{claim: func(_ context.Context, limit int) ([]company.MailIndexJob, error) {
		if limit != 1 {
			t.Errorf("index claim widened its existing single-job budget: %d", limit)
		}
		return []company.MailIndexJob{job}, nil
	}}
}

func r5IndexSource(raw string) *r5IndexObjects {
	return &r5IndexObjects{get: func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(raw)), nil
	}}
}

func TestR5MailIndexCancellationBeforeClaim(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"cancelled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}},
		{"deadline exceeded", func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Unix(0, 0))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			repo := r5IndexRepo(t, r5IndexJob())
			objects := r5IndexSource(r5IndexRaw)
			n, err := New(repo, objects, zerolog.Nop()).Batch(ctx)
			if n != 0 || !errors.Is(err, ctx.Err()) || repo.claims != 0 || len(repo.failures) != 0 || len(repo.completions) != 0 || objects.opens.Load() != 0 {
				t.Fatalf("cancelled entry performed work: n=%d err=%v claims=%d failed=%d completed=%d opens=%d", n, err, repo.claims, len(repo.failures), len(repo.completions), objects.opens.Load())
			}
		})
	}
}

func TestR5MailIndexCancellationAfterClaim(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			job := r5IndexJob()
			repo := &r5IndexRepository{claim: func(context.Context, int) ([]company.MailIndexJob, error) {
				cancel()
				if empty {
					return nil, nil
				}
				return []company.MailIndexJob{job}, nil
			}}
			objects := r5IndexSource(r5IndexRaw)
			n, err := New(repo, objects, zerolog.Nop()).Batch(ctx)
			if n != 0 || !errors.Is(err, context.Canceled) || repo.claims != 1 || len(repo.failures) != 0 || len(repo.completions) != 0 || objects.opens.Load() != 0 {
				t.Fatalf("cancelled claim admitted a later operation: n=%d err=%v claims=%d failed=%d completed=%d opens=%d", n, err, repo.claims, len(repo.failures), len(repo.completions), objects.opens.Load())
			}
		})
	}
}

type r5IndexHeldReader struct {
	reader      *strings.Reader
	entered     chan struct{}
	release     chan struct{}
	closed      chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
	closeOnce   sync.Once
}

func (r *r5IndexHeldReader) Read(p []byte) (int, error) {
	r.enterOnce.Do(func() { close(r.entered) })
	<-r.release
	return r.reader.Read(p)
}
func (r *r5IndexHeldReader) unblock() { r.releaseOnce.Do(func() { close(r.release) }) }
func (r *r5IndexHeldReader) Close() error {
	r.unblock()
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func r5IndexWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("index cancellation barrier did not complete")
	}
}

func TestR5MailIndexCancellationDuringSourceReadLeavesLeaseForRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := r5IndexJob()
	repo := r5IndexRepo(t, job)
	held := &r5IndexHeldReader{reader: strings.NewReader(r5IndexRaw), entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	t.Cleanup(func() { held.unblock(); r5IndexWait(t, held.closed) })
	objects := &r5IndexObjects{get: func(context.Context, string) (io.ReadCloser, error) { return held, nil }}
	service := New(repo, objects, zerolog.Nop())
	var count int
	var batchErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		count, batchErr = service.Batch(ctx)
	}()
	r5IndexWait(t, held.entered)
	cancel()
	r5IndexWait(t, done)
	if count != 0 || !errors.Is(batchErr, context.Canceled) || len(repo.failures) != 0 || len(repo.completions) != 0 {
		t.Fatalf("worker cancellation became a durable outcome: n=%d err=%v failed=%d completed=%d", count, batchErr, len(repo.failures), len(repo.completions))
	}
	// Shared parser work keeps its existing independent resource budget. Release
	// this synthetic source before reclaiming; the worker does not own that policy.
	held.unblock()
	r5IndexWait(t, held.closed)
	reclaimed := job
	reclaimed.Token = uuid.New()
	reclaimed.LeaseUntil = job.LeaseUntil.Add(time.Minute)
	repo.claim = func(context.Context, int) ([]company.MailIndexJob, error) {
		return []company.MailIndexJob{reclaimed}, nil
	}
	n, err := service.Batch(context.Background())
	if n != 1 || err != nil || repo.claims != 2 || len(repo.failures) != 0 || len(repo.completions) != 1 {
		t.Fatalf("reclaimed source could not complete: n=%d err=%v claims=%d failed=%d completed=%d", n, err, repo.claims, len(repo.failures), len(repo.completions))
	}
	completed := repo.completions[0]
	if completed.job != reclaimed || completed.document.MessageID != job.MessageID || completed.document.SourceKey != job.SourceKey || completed.document.SourceSHA256 != mailcontent.Hash([]byte(r5IndexRaw)) {
		t.Fatal("recovery completion lost the newly claimed lease or immutable source identity")
	}
}

func TestR5MailIndexCancellationKeepsAcknowledgedOutcome(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%v", failed), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := r5IndexRepo(t, r5IndexJob())
			raw := r5IndexRaw
			finish := func(context.Context) error { cancel(); return nil }
			if failed {
				raw = r5IndexBadMIME
				repo.fail = finish
			} else {
				repo.complete = finish
			}
			service := New(repo, r5IndexSource(raw), zerolog.Nop())
			if n, err := service.Batch(ctx); n != 1 || err != nil {
				t.Fatalf("cancellation pretended to undo an acknowledged outcome: n=%d err=%v", n, err)
			}
			if len(repo.completions)+len(repo.failures) != 1 {
				t.Fatal("acknowledged outcome was lost or duplicated")
			}
			if n, err := service.Batch(ctx); n != 0 || !errors.Is(err, context.Canceled) || repo.claims != 1 || len(repo.completions)+len(repo.failures) != 1 {
				t.Fatalf("a later cancelled batch admitted more work: n=%d err=%v claims=%d", n, err, repo.claims)
			}
		})
	}
}

func TestR5MailIndexNormalOutcomesPreserved(t *testing.T) {
	for _, kind := range []string{"valid source", "malformed MIME", "object read failure"} {
		t.Run(kind, func(t *testing.T) {
			job := r5IndexJob()
			repo := r5IndexRepo(t, job)
			objects := r5IndexSource(r5IndexRaw)
			if kind == "malformed MIME" {
				objects = r5IndexSource(r5IndexBadMIME)
			} else if kind == "object read failure" {
				objects.get = func(context.Context, string) (io.ReadCloser, error) {
					return nil, errors.New("synthetic object failure")
				}
			}
			n, err := New(repo, objects, zerolog.Nop()).Batch(context.Background())
			if n != 1 || err != nil || repo.claims != 1 || objects.opens.Load() != 1 {
				t.Fatalf("ordinary index outcome changed: n=%d err=%v claims=%d opens=%d", n, err, repo.claims, objects.opens.Load())
			}
			if kind == "valid source" {
				if len(repo.failures) != 0 || len(repo.completions) != 1 || repo.completions[0].job != job || repo.completions[0].document.TextBody != "index body" {
					t.Fatal("valid source was not completed with its original lease and parsed body")
				}
			} else if len(repo.completions) != 0 || len(repo.failures) != 1 || repo.failures[0].job != job || repo.failures[0].reason != "parse_failed" {
				t.Fatal("real source failure did not retain the existing durable failure route")
			}
		})
	}
}

func TestR5MailIndexStoreFailuresRemainVisible(t *testing.T) {
	for _, stage := range []string{"claim", "claim with cancellation", "complete", "fail"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := r5IndexRepo(t, r5IndexJob())
			objects := r5IndexSource(r5IndexRaw)
			failure := errors.New("synthetic durable storage failure")
			wrapped := fmt.Errorf("storage operation: %w", failure)
			switch stage {
			case "claim", "claim with cancellation":
				repo.claim = func(context.Context, int) ([]company.MailIndexJob, error) {
					if stage == "claim with cancellation" {
						cancel()
					}
					return nil, wrapped
				}
			case "complete":
				repo.complete = func(context.Context) error { return wrapped }
			case "fail":
				objects = r5IndexSource(r5IndexBadMIME)
				repo.fail = func(context.Context) error { return wrapped }
			}
			n, err := New(repo, objects, zerolog.Nop()).Batch(ctx)
			if n != 0 || !errors.Is(err, failure) {
				t.Fatalf("durable storage error disappeared: n=%d err=%v", n, err)
			}
		})
	}
}

func TestR5MailIndexRunStopsAfterClaimCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := r5IndexJob()
	repo := &r5IndexRepository{claim: func(context.Context, int) ([]company.MailIndexJob, error) {
		cancel()
		return []company.MailIndexJob{job}, nil
	}}
	objects := r5IndexSource(r5IndexRaw)
	done := make(chan struct{})
	go func() { defer close(done); New(repo, objects, zerolog.Nop()).Run(ctx) }()
	r5IndexWait(t, done)
	if repo.claims != 1 || len(repo.failures) != 0 || len(repo.completions) != 0 || objects.opens.Load() != 0 {
		t.Fatal("cancelled run did not stop before the next side effect")
	}
}
