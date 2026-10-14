package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

type attachmentCloseRepo struct {
	*contentRepo
	reads int
}

func (r *attachmentCloseRepo) GetWorkAttachment(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.Attachment, error) {
	r.reads++
	return r.contentRepo.GetWorkAttachment(ctx, a, id)
}

func (r *attachmentCloseRepo) GetSubmissionAttachment(ctx context.Context, a authz.Actor, id, attachment uuid.UUID) (*company.SubmissionAttachment, error) {
	r.reads++
	return r.contentRepo.GetSubmissionAttachment(ctx, a, id, attachment)
}

func callCloseAttachment(s *Service, ctx context.Context, actor authz.Actor, attachment uuid.UUID, sent bool) (*File, error) {
	if sent {
		return s.SubmissionAttachment(ctx, actor, uuid.New(), attachment)
	}
	return s.Attachment(ctx, actor, attachment)
}

func TestR5AttachmentOwnedCloseErrors(t *testing.T) {
	for _, sent := range []bool{false, true} {
		endpoint := "attachment"
		if sent {
			endpoint = "submission-attachment"
		}
		for _, mode := range []string{"close-only", "empty-close", "read-and-close", "cancel-read-and-close", "cancel-during-close", "normal", "identity-changed-on-close", "revoked-on-close"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				_, base, _, actor, _, _ := contentFixture()
				raw := []byte("synthetic private attachment")
				if mode == "empty-close" {
					raw = nil
				}
				base.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "synthetic/attachment", Filename: "fixture.txt", State: "ready", Size: int64(len(raw)), SHA256: digest(raw)}
				repo := &attachmentCloseRepo{contentRepo: base}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				readErr, closeErr := errors.New("synthetic read failure"), errors.New("synthetic close failure")
				reader := &r5AttachmentReader{read: bytes.NewReader(raw).Read, close: func() error { return closeErr }}
				wanted := []error{closeErr}
				wantReads := 1
				switch mode {
				case "read-and-close":
					reader.read = func(p []byte) (int, error) { return copy(p, raw), readErr }
					wanted = append(wanted, readErr)
				case "cancel-read-and-close":
					reader.read = func(p []byte) (int, error) { cancel(); return copy(p, raw), readErr }
					wanted = append(wanted, readErr, context.Canceled)
				case "cancel-during-close":
					reader.close = func() error { cancel(); return closeErr }
					wanted = append(wanted, context.Canceled)
				case "normal":
					reader.close = nil
					wanted, wantReads = nil, 2
				case "identity-changed-on-close":
					reader.close = func() error { base.attachment.ObjectKey = "replacement/attachment"; return nil }
					wanted, wantReads = nil, 2
				case "revoked-on-close":
					reader.close = func() error { base.denied = app.Forbidden("attachment access revoked"); return nil }
					wanted, wantReads = nil, 2
				}
				svc := NewService(repo, &r5AttachmentReadObjects{reader: reader})
				file, err := callCloseAttachment(svc, ctx, actor, base.attachment.ID, sent)
				if mode == "normal" {
					if err != nil || file == nil || !bytes.Equal(file.Content, raw) || file.Filename != "fixture.txt" {
						t.Fatalf("normal download changed: file=%v err=%v", file, err)
					}
				} else {
					if file != nil || err == nil {
						t.Errorf("failed download returned file=%v err=%v", file, err)
					}
					for _, cause := range wanted {
						if !errors.Is(err, cause) {
							t.Errorf("missing cause %v in %v", cause, err)
						}
					}
					if mode == "identity-changed-on-close" {
						expectKind(t, err, app.KindConflict)
					}
					if mode == "revoked-on-close" {
						expectKind(t, err, app.KindForbidden)
					}
				}
				if reader.closes.Load() != 1 || repo.reads != wantReads {
					t.Errorf("close/recheck lifecycle changed: closes=%d repoReads=%d, want 1/%d", reader.closes.Load(), repo.reads, wantReads)
				}
			})
		}
	}
}

func TestR5AttachmentOwnedCloseCancellationCallback(t *testing.T) {
	for _, sent := range []bool{false, true} {
		endpoint := "attachment"
		if sent {
			endpoint = "submission-attachment"
		}
		t.Run(endpoint, func(t *testing.T) {
			_, repo, _, actor, _, _ := contentFixture()
			repo.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "synthetic/blocked", State: "ready", Size: 0, SHA256: digest(nil)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, closed := make(chan struct{}), make(chan struct{})
			var startOnce, closeOnce sync.Once
			closeErr := errors.New("synthetic cancellation close failure")
			reader := &r5AttachmentReader{
				read:  func([]byte) (int, error) { startOnce.Do(func() { close(started) }); <-closed; return 0, io.EOF },
				close: func() error { closeOnce.Do(func() { close(closed) }); return closeErr },
			}
			svc := NewService(repo, &r5AttachmentReadObjects{reader: reader})
			type result struct {
				file *File
				err  error
			}
			done := make(chan result, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				file, err := callCloseAttachment(svc, ctx, actor, repo.attachment.ID, sent)
				done <- result{file, err}
			}()
			t.Cleanup(func() {
				cancel()
				closeOnce.Do(func() { close(closed) })
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("download worker remained after fixture cleanup")
				}
			})
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("reader never began")
			}
			cancel()
			select {
			case got := <-done:
				if got.file != nil || !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, closeErr) || reader.closes.Load() != 1 {
					t.Fatalf("callback close error lost: file=%v err=%v closes=%d", got.file, got.err, reader.closes.Load())
				}
			case <-time.After(time.Second):
				closeOnce.Do(func() { close(closed) })
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("reader worker did not join after fixture release")
				}
				t.Fatal("cancellation failed to release the owned reader")
			}
		})
	}
}
