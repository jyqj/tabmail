package outbound

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type queuedReadObjects struct {
	store.ObjectStore
	get   func(context.Context, string) (io.ReadCloser, error)
	calls atomic.Int32
}

func (s *queuedReadObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	s.calls.Add(1)
	return s.get(ctx, key)
}

type queuedReadCloser struct {
	read      func([]byte) (int, error)
	close     func() error
	reads     atomic.Int32
	closes    atomic.Int32
	bytesRead atomic.Int64
}

func (r *queuedReadCloser) Read(p []byte) (int, error) {
	r.reads.Add(1)
	n, err := r.read(p)
	if n > 0 && n <= len(p) {
		r.bytesRead.Add(int64(n))
	}
	return n, err
}

func (r *queuedReadCloser) Close() error {
	r.closes.Add(1)
	if r.close != nil {
		return r.close()
	}
	return nil
}

type queuedReadResult struct {
	wire  []byte
	err   error
	panic any
}

// Keep malformed-reader baseline failures inside their subtest so every
// independent boundary still executes. Production is required not to panic.
func observeQueuedRead(svc *Service, ctx context.Context, job *models.OutboundJob) (result queuedReadResult) {
	defer func() { result.panic = recover() }()
	result.wire, result.err = svc.buildQueuedMIME(ctx, job)
	return result
}

func queuedReadFixture(data string) (*Service, *queuedAttachmentRepository, *queuedReadObjects, *models.OutboundJob, *queuedReadCloser) {
	a, _, _ := queuedAttachmentFixture()
	a.Size, a.SHA256 = int64(len(data)), company.Hash(data)
	repo := &queuedAttachmentRepository{rows: []company.Attachment{a}}
	r := &queuedReadCloser{read: strings.NewReader(data).Read}
	objects := &queuedReadObjects{get: func(context.Context, string) (io.ReadCloser, error) { return r, nil }}
	svc := &Service{store: repo, objects: objects}
	job := &models.OutboundJob{ID: uuid.New(), AttachmentIDs: []uuid.UUID{a.ID}, MailFrom: "sender@example.test", To: []string{"recipient@example.test"}, TextBody: "message"}
	return svc, repo, objects, job, r
}

func requireQueuedReadFailure(t *testing.T, result queuedReadResult, causes ...error) {
	t.Helper()
	if result.panic != nil {
		t.Fatalf("queued attachment reader panicked: %v", result.panic)
	}
	if result.err == nil || result.wire != nil {
		t.Errorf("failed attachment returned MIME: err=%v bytes=%d", result.err, len(result.wire))
	}
	for _, cause := range causes {
		if !errors.Is(result.err, cause) {
			t.Errorf("lost error cause %v: %v", cause, result.err)
		}
	}
}

type queuedMIMEAttachment struct {
	size int64
	sha  string
}

func queuedMIMEAttachments(t *testing.T, result queuedReadResult) map[string]queuedMIMEAttachment {
	t.Helper()
	if result.panic != nil || result.err != nil || len(result.wire) == 0 {
		t.Fatalf("valid attachment did not produce MIME: panic=%v err=%v bytes=%d", result.panic, result.err, len(result.wire))
	}
	message, err := mail.ReadMessage(bytes.NewReader(result.wire))
	if err != nil {
		t.Fatal(err)
	}
	kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/mixed" {
		t.Fatalf("expected attachment MIME: kind=%q err=%v", kind, err)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	actual := map[string]queuedMIMEAttachment{}
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := part.FileName()
		if name == "" {
			continue
		}
		if _, duplicate := actual[name]; duplicate {
			t.Fatalf("duplicate MIME attachment %q", name)
		}
		hash := sha256.New()
		n, err := io.Copy(hash, base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatal(err)
		}
		actual[name] = queuedMIMEAttachment{size: n, sha: hex.EncodeToString(hash.Sum(nil))}
	}
	return actual
}

func TestR5QueuedAttachmentReadPrecancelDoesNotAcquireResources(t *testing.T) {
	for _, attachments := range []bool{false, true} {
		t.Run(fmt.Sprintf("attachments=%v", attachments), func(t *testing.T) {
			svc, repo, objects, job, reader := queuedReadFixture("x")
			if !attachments {
				job.AttachmentIDs = nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			requireQueuedReadFailure(t, observeQueuedRead(svc, ctx, job), context.Canceled)
			if repo.calls != 0 || objects.calls.Load() != 0 || reader.closes.Load() != 0 {
				t.Fatalf("cancelled request acquired resources: repo=%d Get=%d Close=%d", repo.calls, objects.calls.Load(), reader.closes.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadOwnsReaderAndPreservesCauses(t *testing.T) {
	getErr, readErr, closeErr := errors.New("controlled Get failure"), errors.New("controlled Read failure"), errors.New("controlled Close failure")
	for _, tc := range []struct {
		name                   string
		get, read, close       error
		nilReader, partialRead bool
	}{
		{name: "Get_nil_reader_error", get: getErr, nilReader: true},
		{name: "Get_reader_error", get: getErr},
		{name: "Get_reader_and_Close_errors", get: getErr, close: closeErr},
		{name: "Read_error", read: readErr},
		{name: "Read_partial_error", read: readErr, partialRead: true},
		{name: "Read_and_Close_errors", read: readErr, close: closeErr},
		{name: "Close_error", close: closeErr},
		{name: "nil_reader_without_error", nilReader: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, objects, job, reader := queuedReadFixture("x")
			reader.close = func() error { return tc.close }
			if tc.read != nil {
				reader.read = func(p []byte) (int, error) {
					if tc.partialRead {
						return copy(p, "x"), tc.read
					}
					return 0, tc.read
				}
			}
			objects.get = func(context.Context, string) (io.ReadCloser, error) {
				if tc.nilReader {
					return nil, tc.get
				}
				return reader, tc.get
			}
			causes := []error{}
			for _, cause := range []error{tc.get, tc.read, tc.close} {
				if cause != nil {
					causes = append(causes, cause)
				}
			}
			requireQueuedReadFailure(t, observeQueuedRead(svc, context.Background(), job), causes...)
			wantClose := int32(1)
			if tc.nilReader {
				wantClose = 0
			}
			if reader.closes.Load() != wantClose {
				t.Errorf("owned reader Close count=%d want=%d", reader.closes.Load(), wantClose)
			}
			if objects.calls.Load() != 1 || (tc.get != nil && reader.reads.Load() != 0) {
				t.Errorf("unexpected failed Get ownership: Get=%d Read=%d", objects.calls.Load(), reader.reads.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadRechecksCancellationAtBoundaries(t *testing.T) {
	for _, stage := range []string{"Get_return", "Get_nil_return", "Read_EOF", "Close_return"} {
		t.Run(stage, func(t *testing.T) {
			svc, _, objects, job, reader := queuedReadFixture("x")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantClose := int32(1)
			switch stage {
			case "Get_return":
				objects.get = func(context.Context, string) (io.ReadCloser, error) { cancel(); return reader, nil }
			case "Get_nil_return":
				objects.get = func(context.Context, string) (io.ReadCloser, error) { cancel(); return nil, nil }
				wantClose = 0
			case "Read_EOF":
				reader.read = func(p []byte) (int, error) { cancel(); return copy(p, "x"), io.EOF }
			case "Close_return":
				reader.close = func() error { cancel(); return nil }
			}
			requireQueuedReadFailure(t, observeQueuedRead(svc, ctx, job), context.Canceled)
			if reader.closes.Load() != wantClose || (strings.HasPrefix(stage, "Get_") && reader.reads.Load() != 0) {
				t.Errorf("cancelled reader ownership violated: Read=%d Close=%d", reader.reads.Load(), reader.closes.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadCancellationClosesBlockedReader(t *testing.T) {
	svc, _, _, job, reader := queuedReadFixture("x")
	ctx, cancel := context.WithCancel(context.Background())
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	reader.read = func([]byte) (int, error) {
		enterOnce.Do(func() { close(entered) })
		<-release
		return 0, errors.New("owned reader released")
	}
	reader.close = func() error { unblock(); return nil }
	done := make(chan queuedReadResult, 1)
	go func() {
		defer close(joined)
		done <- observeQueuedRead(svc, ctx, job)
	}()
	t.Cleanup(func() {
		cancel()
		unblock() // finite baseline escape; never rely on an unbounded broken Read
		select {
		case <-joined:
		case <-time.After(2 * time.Second):
			t.Error("test-owned queued MIME worker did not join")
		}
	})
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("queued MIME builder did not enter the controlled reader")
	}
	cancel()
	select {
	case result := <-done:
		requireQueuedReadFailure(t, result, context.Canceled)
		if reader.closes.Load() != 1 {
			t.Errorf("cancelled reader Close count=%d want=1", reader.closes.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close and join the blocked reader")
	}
}

func TestR5QueuedAttachmentReadRejectsConsecutiveNoProgress(t *testing.T) {
	svc, _, _, job, reader := queuedReadFixture("x")
	body := strings.NewReader("x")
	empty := 0
	reader.read = func(p []byte) (int, error) {
		if empty < 101 {
			empty++
			return 0, nil
		}
		// A broken baseline eventually completes; it cannot hang the suite.
		return body.Read(p)
	}
	requireQueuedReadFailure(t, observeQueuedRead(svc, context.Background(), job), io.ErrNoProgress)
	if reader.reads.Load() != 100 || reader.closes.Load() != 1 {
		t.Fatalf("no-progress bound or cleanup changed: Read=%d Close=%d", reader.reads.Load(), reader.closes.Load())
	}
}

func TestR5QueuedAttachmentReadTransientEmptiesResetAfterProgress(t *testing.T) {
	for _, empties := range []int{1, 99} {
		t.Run(fmt.Sprintf("empties_per_byte=%d", empties), func(t *testing.T) {
			svc, repo, _, job, reader := queuedReadFixture("ab")
			remaining, sent := empties, 0
			reader.read = func(p []byte) (int, error) {
				if remaining > 0 {
					remaining--
					return 0, nil
				}
				if sent == 2 {
					return 0, io.EOF
				}
				p[0] = "ab"[sent]
				sent++
				remaining = empties
				if sent == 2 {
					return 1, io.EOF
				}
				return 1, nil
			}
			actual := queuedMIMEAttachments(t, observeQueuedRead(svc, context.Background(), job))
			a := repo.rows[0]
			if len(actual) != 1 || actual[a.Filename] != (queuedMIMEAttachment{size: a.Size, sha: a.SHA256}) || reader.closes.Load() != 1 {
				t.Fatalf("transient empty reads changed attachment/ownership: actual=%v Close=%d", actual, reader.closes.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadRejectsInvalidReaderCounts(t *testing.T) {
	for _, negative := range []bool{false, true} {
		t.Run(fmt.Sprintf("negative=%v", negative), func(t *testing.T) {
			svc, _, _, job, reader := queuedReadFixture("x")
			reader.read = func(p []byte) (int, error) {
				if negative {
					return -1, nil
				}
				return len(p) + 1, nil
			}
			requireQueuedReadFailure(t, observeQueuedRead(svc, context.Background(), job))
			if reader.closes.Load() != 1 || reader.reads.Load() != 1 {
				t.Fatalf("malformed reader was retried or not closed: Read=%d Close=%d", reader.reads.Load(), reader.closes.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadRetainsSizeAndHashBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, data, expected string
		valid                bool
	}{
		{name: "exact", data: "abc", expected: "abc", valid: true},
		{name: "empty", data: "", expected: "", valid: true},
		{name: "short", data: "ab", expected: "abc"},
		{name: "long", data: "abc" + strings.Repeat("x", 8192), expected: "abc"},
		{name: "SHA_mismatch", data: "abd", expected: "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, job, reader := queuedReadFixture(tc.expected)
			reader.read = strings.NewReader(tc.data).Read
			result := observeQueuedRead(svc, context.Background(), job)
			if !tc.valid {
				requireQueuedReadFailure(t, result)
			} else {
				actual := queuedMIMEAttachments(t, result)
				a := repo.rows[0]
				if len(actual) != 1 || actual[a.Filename] != (queuedMIMEAttachment{size: a.Size, sha: a.SHA256}) {
					t.Fatalf("wrong decoded MIME: %v", actual)
				}
			}
			if reader.closes.Load() != 1 || reader.bytesRead.Load() > int64(len(tc.expected))+1 {
				t.Fatalf("size sentinel/ownership changed: bytes=%d size=%d Close=%d", reader.bytesRead.Load(), len(tc.expected), reader.closes.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadPreflightsAllMetadataBeforeObjects(t *testing.T) {
	for _, fault := range []string{"not_ready", "negative_size", "individual_over_20MiB", "total_over_20MiB", "count_over_10", "empty_name", "long_name", "slash_name", "backslash_name", "CRLF_name", "NUL_name"} {
		t.Run(fault, func(t *testing.T) {
			svc, repo, objects, job, _ := queuedReadFixture("x")
			a, b := repo.rows[0], repo.rows[0]
			b.ID, b.ObjectKey, b.Filename = uuid.New(), "other", "other.txt"
			repo.rows = []company.Attachment{a, b}
			objects.get = func(context.Context, string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("x")), nil
			}
			switch fault {
			case "not_ready":
				repo.rows[1].State = "uploading"
			case "negative_size":
				repo.rows[1].Size = -1
			case "individual_over_20MiB":
				repo.rows[1].Size = 20*1024*1024 + 1
			case "total_over_20MiB":
				repo.rows[0].Size = 20 * 1024 * 1024
			case "count_over_10":
				for len(repo.rows) < 11 {
					row := a
					row.ID, row.Filename = uuid.New(), fmt.Sprintf("part-%d.txt", len(repo.rows))
					repo.rows = append(repo.rows, row)
				}
			case "empty_name":
				repo.rows[1].Filename = ""
			case "long_name":
				repo.rows[1].Filename = strings.Repeat("x", 201)
			case "slash_name":
				repo.rows[1].Filename = "a/b"
			case "backslash_name":
				repo.rows[1].Filename = "a\\b"
			case "CRLF_name":
				repo.rows[1].Filename = "a\r\nb"
			case "NUL_name":
				repo.rows[1].Filename = "a\x00b"
			}
			job.AttachmentIDs = nil
			for _, row := range repo.rows {
				job.AttachmentIDs = append(job.AttachmentIDs, row.ID)
			}
			requireQueuedReadFailure(t, observeQueuedRead(svc, context.Background(), job))
			if objects.calls.Load() != 0 {
				t.Fatalf("invalid later attachment metadata opened %d object(s)", objects.calls.Load())
			}
		})
	}
}

func TestR5QueuedAttachmentReadAcceptsOriginalBudgetAndCountLimits(t *testing.T) {
	t.Run("exact_20MiB", func(t *testing.T) {
		svc, repo, objects, job, reader := queuedReadFixture(strings.Repeat("x", 20*1024*1024))
		actual := queuedMIMEAttachments(t, observeQueuedRead(svc, context.Background(), job))
		a := repo.rows[0]
		if len(actual) != 1 || actual[a.Filename] != (queuedMIMEAttachment{size: a.Size, sha: a.SHA256}) || objects.calls.Load() != 1 || reader.closes.Load() != 1 {
			t.Fatalf("original exact size limit or ownership changed: actual=%v Get=%d Close=%d", actual, objects.calls.Load(), reader.closes.Load())
		}
	})
	t.Run("exact_10_and_200_byte_filename", func(t *testing.T) {
		svc, repo, objects, job, _ := queuedReadFixture("x")
		row := repo.rows[0]
		repo.rows, job.AttachmentIDs = nil, nil
		for i := 0; i < 10; i++ {
			a := row
			a.ID, a.Filename = uuid.New(), fmt.Sprintf("part-%d.txt", i)
			if i == 0 {
				a.Filename = strings.Repeat("x", 200)
			}
			repo.rows = append(repo.rows, a)
			job.AttachmentIDs = append(job.AttachmentIDs, a.ID)
		}
		var readers []*queuedReadCloser
		objects.get = func(context.Context, string) (io.ReadCloser, error) {
			r := &queuedReadCloser{read: strings.NewReader("x").Read}
			readers = append(readers, r)
			return r, nil
		}
		actual := queuedMIMEAttachments(t, observeQueuedRead(svc, context.Background(), job))
		if len(actual) != 10 || objects.calls.Load() != 10 {
			t.Fatalf("original attachment-count limit changed: parts=%d Get=%d", len(actual), objects.calls.Load())
		}
		for _, a := range repo.rows {
			if actual[a.Filename] != (queuedMIMEAttachment{size: a.Size, sha: a.SHA256}) {
				t.Errorf("wrong MIME attachment %q: %v", a.Filename, actual[a.Filename])
			}
		}
		for _, r := range readers {
			if r.closes.Load() != 1 {
				t.Errorf("reader Close count=%d want=1", r.closes.Load())
			}
		}
	})
}
