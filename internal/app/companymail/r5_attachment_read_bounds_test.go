package companymail

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
)

type r5AttachmentReader struct {
	read   func([]byte) (int, error)
	close  func() error
	reads  atomic.Int64
	closes atomic.Int64
}

func (r *r5AttachmentReader) Read(p []byte) (int, error) {
	r.reads.Add(1)
	return r.read(p)
}
func (r *r5AttachmentReader) Close() error {
	r.closes.Add(1)
	if r.close != nil {
		return r.close()
	}
	return nil
}

type r5AttachmentReadObjects struct {
	ObjectStore
	reader io.ReadCloser
}

func (o *r5AttachmentReadObjects) Get(context.Context, string) (io.ReadCloser, error) {
	return o.reader, nil
}

func r5EmptyAttachmentReader() *r5AttachmentReader {
	empty := 0
	return &r5AttachmentReader{read: func([]byte) (int, error) {
		empty++
		if empty <= 101 {
			return 0, nil
		}
		return 0, io.EOF
	}}
}

func TestR5AttachmentUploadRejectsNoProgress(t *testing.T) {
	svc, repo, _, actor, mailbox, _ := contentFixture()
	r := r5EmptyAttachmentReader()
	attachment, err := svc.UploadAttachment(context.Background(), actor, mailbox, "empty.txt", r)
	if attachment != nil || err == nil {
		t.Fatal("stalled upload was reserved and marked ready")
	}
	expectKind(t, err, app.KindBadRequest)
	if r.reads.Load() > 100 || len(*repo.events) != 0 {
		t.Fatalf("stalled upload exceeded read/side-effect budget: reads=%d events=%v", r.reads.Load(), *repo.events)
	}
}

func TestR5AttachmentUploadCancellationStopsBeforeReservation(t *testing.T) {
	svc, repo, _, actor, mailbox, _ := contentFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := strings.NewReader("ab")
	r := &r5AttachmentReader{read: func(p []byte) (int, error) {
		n, err := source.Read(p[:1])
		cancel()
		return n, err
	}}
	attachment, err := svc.UploadAttachment(ctx, actor, mailbox, "cancel.txt", r)
	if attachment != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upload was not preserved as cancellation: %v", err)
	}
	if r.reads.Load() != 1 || len(*repo.events) != 0 {
		t.Fatalf("continued after canceled read: reads=%d events=%v", r.reads.Load(), *repo.events)
	}
}

func TestR5AttachmentUploadAllowsProgressAndKeepsReaderOwnership(t *testing.T) {
	svc, repo, _, actor, mailbox, _ := contentFixture()
	step := 0
	r := &r5AttachmentReader{read: func(p []byte) (int, error) {
		step++
		if step == 100 || step == 200 {
			p[0] = 'x'
			return 1, nil
		}
		if step > 200 {
			return 0, io.EOF
		}
		return 0, nil
	}}
	attachment, err := svc.UploadAttachment(context.Background(), actor, mailbox, "progress.txt", r)
	if err != nil || attachment == nil || attachment.Size != 2 || attachment.SHA256 != digest([]byte("xx")) || strings.Join(*repo.events, ",") != "reserve,put,finish" {
		t.Fatalf("finite progress stopped or integrity changed: attachment=%v error=%v", attachment, err)
	}
	if r.closes.Load() != 0 {
		t.Fatal("upload closed a caller-owned reader")
	}
}

func TestR5AttachmentDownloadRejectsNoProgress(t *testing.T) {
	for _, sent := range []bool{false, true} {
		name := "draft"
		if sent {
			name = "sent"
		}
		t.Run(name, func(t *testing.T) {
			_, repo, _, actor, _, _ := contentFixture()
			repo.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "stalled", State: "ready", Size: 0, SHA256: digest(nil)}
			r := r5EmptyAttachmentReader()
			svc := NewService(repo, &r5AttachmentReadObjects{reader: r})
			var file *File
			var err error
			if sent {
				file, err = svc.SubmissionAttachment(context.Background(), actor, uuid.New(), repo.attachment.ID)
			} else {
				file, err = svc.Attachment(context.Background(), actor, repo.attachment.ID)
			}
			if file != nil || err == nil {
				t.Fatal("stalled object was returned as a valid download")
			}
			expectKind(t, err, app.KindInternal)
			if r.reads.Load() > 100 || r.closes.Load() != 1 {
				t.Fatalf("stalled object exceeded read/close budget: reads=%d closes=%d", r.reads.Load(), r.closes.Load())
			}
		})
	}
}

func TestR5AttachmentDownloadCancellationStopsFurtherReads(t *testing.T) {
	_, repo, _, actor, _, _ := contentFixture()
	repo.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "cancel", State: "ready", Size: 2, SHA256: digest([]byte("ab"))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := strings.NewReader("ab")
	r := &r5AttachmentReader{read: func(p []byte) (int, error) {
		n, err := source.Read(p[:1])
		cancel()
		return n, err
	}}
	svc := NewService(repo, &r5AttachmentReadObjects{reader: r})
	file, err := svc.Attachment(ctx, actor, repo.attachment.ID)
	if file != nil || !errors.Is(err, context.Canceled) || r.reads.Load() != 1 || r.closes.Load() != 1 {
		t.Fatalf("cancellation did not stop owned source: error=%v reads=%d closes=%d", err, r.reads.Load(), r.closes.Load())
	}
}

func TestR5AttachmentDownloadCancellationClosesBlockedSource(t *testing.T) {
	_, repo, _, actor, _, _ := contentFixture()
	repo.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "blocked", State: "ready", Size: 0, SHA256: digest(nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, released := make(chan struct{}), make(chan struct{})
	var startOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	defer release()
	r := &r5AttachmentReader{
		read:  func([]byte) (int, error) { startOnce.Do(func() { close(started) }); <-released; return 0, io.EOF },
		close: func() error { release(); return nil },
	}
	svc := NewService(repo, &r5AttachmentReadObjects{reader: r})
	done := make(chan error, 1)
	go func() { _, err := svc.Attachment(ctx, actor, repo.attachment.ID); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("synthetic source never began reading")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || r.closes.Load() != 1 {
			t.Fatalf("wrong canceled-source result: %v; closes=%d", err, r.closes.Load())
		}
	case <-time.After(time.Second):
		// Let the baseline finish before reporting failure: no stranded worker.
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("source did not finish after explicit fixture cleanup")
		}
		t.Fatal("cancellation did not close the owned blocked source")
	}
}
