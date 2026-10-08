package s3obj_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/store/s3obj"
)

// The real pinned MinIO client sends single and multipart uploads to this
// task-owned S3 HTTP namespace. Incomplete request bodies and uncompleted
// multipart parts never replace the visible object.
type uploadBoundaryFixture struct {
	store          *s3obj.Store
	mu             sync.Mutex
	object         []byte
	parts          map[int][]byte
	requests       []string
	published      int
	aborted        int
	rejectFirstPut bool
}

func newUploadBoundaryFixture(t *testing.T) *uploadBoundaryFixture {
	t.Helper()
	f := &uploadBoundaryFixture{object: []byte("original referenced message"), parts: make(map[int][]byte)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && r.URL.Path == "/mail-bucket/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		if r.URL.Path != "/mail-bucket/original.eml" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		query := r.URL.Query()
		if r.Method == http.MethodPost && query.Has("uploads") {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>mail-bucket</Bucket><Key>original.eml</Key><UploadId>owned-boundary-upload</UploadId></InitiateMultipartUploadResult>`)
			return
		}
		if r.Method == http.MethodDelete && query.Get("uploadId") == "owned-boundary-upload" {
			f.mu.Lock()
			clear(f.parts)
			f.aborted++
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && query.Get("uploadId") == "owned-boundary-upload" {
			var completion struct {
				Parts []struct {
					PartNumber int
					ETag       string
				} `xml:"Part"`
			}
			if err := xml.NewDecoder(r.Body).Decode(&completion); err != nil || len(completion.Parts) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			var content []byte
			for index, part := range completion.Parts {
				body, ok := f.parts[part.PartNumber]
				if !ok || part.PartNumber != index+1 || strings.Trim(part.ETag, `"`) != fmt.Sprintf("owned-part-%d", part.PartNumber) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				content = append(content, body...)
			}
			f.object = content
			f.published++
			clear(f.parts)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>mail-bucket</Bucket><Key>original.eml</Key><ETag>owned-complete</ETag></CompleteMultipartUploadResult>`)
			return
		}
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var reader io.Reader = r.Body
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-AWS4-HMAC-SHA256-PAYLOAD") {
			reader = httputil.NewChunkedReader(reader)
		}
		body, readErr := io.ReadAll(reader)
		length := r.ContentLength
		var lengthErr error
		if header := r.Header.Get("X-Amz-Decoded-Content-Length"); header != "" {
			length, lengthErr = strconv.ParseInt(header, 10, 64)
		}
		if readErr != nil || lengthErr != nil || int64(len(body)) != length {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.rejectFirstPut {
			f.rejectFirstPut = false
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `<Error><Code>InternalError</Code><Message>owned failure before publication</Message></Error>`)
			return
		}
		if query.Get("uploadId") == "owned-boundary-upload" {
			part, err := strconv.Atoi(query.Get("partNumber"))
			if err != nil || part < 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.parts[part] = body
			w.Header().Set("ETag", fmt.Sprintf(`"owned-part-%d"`, part))
		} else if r.URL.RawQuery == "" {
			f.object = body
			f.published++
			w.Header().Set("ETag", `"owned-single"`)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	var err error
	f.store, err = s3obj.New(config.S3{
		Endpoint: strings.TrimPrefix(server.URL, "http://"), Bucket: "mail-bucket",
		AccessKey: "owned-test-access", SecretKey: "owned-test-secret",
		Region: "us-east-1", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *uploadBoundaryFixture) unchanged(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if string(f.object) != "original referenced message" || f.published != 0 {
		t.Errorf("invalid upload published %d times; stored bytes=%d", f.published, len(f.object))
	}
}

type boundaryUploadReader struct {
	read   func([]byte) (int, error)
	reads  atomic.Int32
	closes atomic.Int32
}

func (r *boundaryUploadReader) Read(p []byte) (int, error) { r.reads.Add(1); return r.read(p) }
func (r *boundaryUploadReader) Close() error               { r.closes.Add(1); return nil }

func putBoundaryWithoutPanic(ctx context.Context, f *uploadBoundaryFixture, reader io.Reader, size int64) (err error, panicValue any) {
	defer func() { panicValue = recover() }()
	err = f.store.Put(ctx, "original.eml", reader, size)
	return
}

func TestR5S3UploadRejectsInvalidDeclarationBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader io.Reader
		size   int64
	}{
		{"nil_empty", nil, 0},
		{"negative_size", strings.NewReader("abc"), -2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUploadBoundaryFixture(t)
			err, panicValue := putBoundaryWithoutPanic(context.Background(), f, tc.reader, tc.size)
			if err == nil || panicValue != nil {
				t.Errorf("invalid declaration: err=%v panic=%v", err, panicValue)
			}
			f.unchanged(t)
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.requests) != 0 {
				t.Errorf("invalid input sent %d object requests", len(f.requests))
			}
		})
	}
}

func TestR5S3UploadExactLengthAndReaderFailures(t *testing.T) {
	sourceFailure := errors.New("owned source failure")
	for _, tc := range []struct {
		name     string
		size     int64
		data     string
		terminal error
		want     error
	}{
		{"seekable_excess", 3, "abcd", nil, nil},
		{"zero_excess", 0, "a", nil, nil},
		{"truncated", 4, "abc", nil, io.ErrUnexpectedEOF},
		{"terminal_error_with_last_bytes", 3, "abc", sourceFailure, sourceFailure},
		{"terminal_unexpected_eof_with_last_bytes", 3, "abc", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
		{"empty_source_error", 3, "", sourceFailure, sourceFailure},
		{"unknown_unexpected_eof", -1, "abc", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
		{"unknown_empty_unexpected_eof", -1, "", io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUploadBoundaryFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			base := strings.NewReader(tc.data)
			reader := &boundaryUploadReader{read: func(p []byte) (int, error) {
				n, err := base.Read(p)
				if base.Len() == 0 && tc.terminal != nil {
					return n, tc.terminal
				}
				return n, err
			}}
			var source io.Reader = reader
			if tc.name == "seekable_excess" {
				source = base
			}
			err, panicValue := putBoundaryWithoutPanic(ctx, f, source, tc.size)
			if err == nil || panicValue != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Errorf("upload failure lost: err=%v want=%v panic=%v", err, tc.want, panicValue)
			}
			if reader.closes.Load() != 0 {
				t.Errorf("closed caller reader %d times", reader.closes.Load())
			}
			f.unchanged(t)
		})
	}
}

func TestR5S3UploadNoProgressAndInvalidCounts(t *testing.T) {
	for _, mode := range []string{"initial_stall", "end_stall", "negative_count", "oversized_count"} {
		t.Run(mode, func(t *testing.T) {
			f := newUploadBoundaryFixture(t)
			calls := 0
			size := int64(1)
			want := io.ErrNoProgress
			if strings.HasSuffix(mode, "count") {
				size = 16*1024*1024 + 1
				want = io.ErrUnexpectedEOF
			}
			reader := &boundaryUploadReader{read: func(p []byte) (int, error) {
				calls++
				switch mode {
				case "negative_count":
					return -1, nil
				case "oversized_count":
					return len(p) + 1, nil
				case "end_stall":
					if calls == 1 {
						p[0] = 'x'
						return 1, nil
					}
				}
				// A finite fallback keeps the unchanged baseline reproducible;
				// accepted behavior must stop at 100 empty reads before it.
				if calls > 102 {
					return 0, io.EOF
				}
				return 0, nil
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err, panicValue := putBoundaryWithoutPanic(ctx, f, reader, size)
			if panicValue != nil || !errors.Is(err, want) {
				t.Errorf("bad source: err=%v want=%v panic=%v", err, want, panicValue)
			}
			if strings.HasSuffix(mode, "stall") && reader.reads.Load() > 101 {
				t.Errorf("unbounded empty reads: %d", reader.reads.Load())
			}
			if reader.closes.Load() != 0 {
				t.Errorf("closed caller reader %d times", reader.closes.Load())
			}
			f.unchanged(t)
		})
	}
}

func TestR5S3UploadCancellationAndOwnership(t *testing.T) {
	for _, mode := range []string{"cancelled_before", "cancelled_during_read", "successful", "empty"} {
		t.Run(mode, func(t *testing.T) {
			f := newUploadBoundaryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data := "abc"
			if mode == "empty" {
				data = ""
			}
			base := strings.NewReader(data)
			reader := &boundaryUploadReader{read: func(p []byte) (int, error) {
				n, err := base.Read(p)
				if mode == "cancelled_during_read" {
					cancel()
				}
				return n, err
			}}
			if mode == "cancelled_before" {
				cancel()
			}
			err, panicValue := putBoundaryWithoutPanic(ctx, f, reader, int64(len(data)))
			if panicValue != nil {
				t.Errorf("upload panicked: %v", panicValue)
			}
			if strings.HasPrefix(mode, "cancelled") {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("cancellation lost: %v", err)
				}
				f.unchanged(t)
			} else {
				if err != nil {
					t.Errorf("legal upload rejected: %v", err)
				}
				f.mu.Lock()
				if string(f.object) != data || f.published != 1 {
					t.Errorf("legal bytes not published exactly once: size=%d published=%d", len(f.object), f.published)
				}
				f.mu.Unlock()
			}
			if reader.closes.Load() != 0 {
				t.Errorf("closed caller reader %d times", reader.closes.Load())
			}
			if mode == "cancelled_before" && reader.reads.Load() != 0 {
				t.Errorf("read cancelled caller input")
			}
		})
	}
}

func TestR5S3UploadMultipartPublication(t *testing.T) {
	for _, mode := range []string{"known_exact", "known_excess", "known_truncated", "unknown_exact"} {
		t.Run(mode, func(t *testing.T) {
			f := newUploadBoundaryFixture(t)
			length := 16*1024*1024 + 2
			if mode == "unknown_exact" {
				length = 17
			}
			body := bytes.Repeat([]byte("x"), length)
			size := int64(length)
			if mode == "known_excess" {
				size--
			}
			if mode == "known_truncated" {
				size++
			}
			if mode == "unknown_exact" {
				size = -1
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err, panicValue := putBoundaryWithoutPanic(ctx, f, bytes.NewReader(body), size)
			if panicValue != nil {
				t.Errorf("multipart panic: %v", panicValue)
			}
			if mode == "known_excess" || mode == "known_truncated" {
				if err == nil {
					t.Errorf("invalid length succeeded")
				}
				f.unchanged(t)
			} else {
				if err != nil {
					t.Errorf("legal multipart rejected: %v", err)
				}
				f.mu.Lock()
				if !bytes.Equal(f.object, body) || f.published != 1 {
					t.Errorf("multipart bytes/publication mismatch: size=%d published=%d", len(f.object), f.published)
				}
				f.mu.Unlock()
			}
		})
	}
}
