package s3obj_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"tabmail/internal/config"
	"tabmail/internal/store/s3obj"
)

func startupS3Config(server *httptest.Server) config.S3 {
	return config.S3{
		Endpoint: strings.TrimPrefix(server.URL, "http://"), Bucket: "startup-bucket",
		AccessKey: "owned-startup-access", SecretKey: "owned-startup-secret",
		Region: "us-east-1", ForcePathStyle: true,
	}
}

func TestR5S3StartupAlreadyCancelled(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "cancelled"
		if expired {
			name = "expired-deadline"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			store, err := s3obj.NewWithContext(ctx, startupS3Config(server))
			if store != nil || !errors.Is(err, ctx.Err()) {
				t.Errorf("cancelled initialization returned store=%v err=%v, want %v", store != nil, err, ctx.Err())
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("cancelled startup sent %d HTTP requests", n)
			}
		})
	}
}

func TestR5S3StartupCancelsBlockedBucketCheck(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			peerDone := make(chan bool, 1)
			release := make(chan struct{})
			var releaseOnce, enterOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodHead || r.URL.Path != "/startup-bucket/" {
					t.Errorf("unexpected initialization request %s %s", r.Method, r.URL.Path)
				}
				enterOnce.Do(func() { close(entered) })
				select {
				case <-r.Context().Done():
					peerDone <- true
				case <-release:
					w.WriteHeader(http.StatusOK)
					peerDone <- false
				}
			}))
			defer server.Close()
			defer unblock()
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
			}
			defer cancel()
			type result struct {
				store *s3obj.Store
				err   error
			}
			done := make(chan result, 1)
			go func() {
				store, err := s3obj.NewWithContext(ctx, startupS3Config(server))
				done <- result{store, err}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				unblock()
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("constructor did not join after fixture release")
				}
				t.Fatal("constructor never reached the real MinIO HEAD request")
			}
			if !deadline {
				cancel()
			}
			<-ctx.Done()
			var got result
			select {
			case got = <-done:
			case <-time.After(time.Second):
				t.Error("startup did not stop after its caller context ended")
				// Baseline still owns a live request. Release and join it rather
				// than abandoning a goroutine or waiting for the SDK's minute timeout.
				unblock()
				select {
				case got = <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("constructor did not join after fixture release")
				}
			}
			if got.store != nil || !errors.Is(got.err, ctx.Err()) {
				t.Errorf("in-flight cancellation returned store=%v err=%v, want %v", got.store != nil, got.err, ctx.Err())
			}
			select {
			case cancelled := <-peerDone:
				if !cancelled {
					t.Error("HTTP request outlived caller cancellation and required fixture release")
				}
			case <-time.After(time.Second):
				t.Error("HTTP peer did not observe request cancellation")
			}
			if n := requests.Load(); n != 1 {
				t.Errorf("expected exactly one owned HEAD request, got %d", n)
			}
		})
	}
}

func TestR5S3StartupCompatibilityAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		status     int
		legacy     bool
	}{
		{name: "existing-bucket", status: http.StatusOK},
		{name: "legacy-constructor", status: http.StatusOK, legacy: true},
		{name: "missing-bucket", code: "NoSuchBucket", status: http.StatusNotFound},
		{name: "access-denied", code: "AccessDenied", status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodHead || r.URL.Path != "/startup-bucket/" {
					t.Errorf("unexpected initialization request %s %s", r.Method, r.URL.Path)
				}
				if tc.code != "" {
					w.Header().Set("X-Minio-Error-Code", tc.code)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var store *s3obj.Store
			var err error
			if tc.legacy {
				store, err = s3obj.New(startupS3Config(server))
			} else {
				store, err = s3obj.NewWithContext(ctx, startupS3Config(server))
			}
			switch tc.code {
			case "":
				if store == nil || err != nil {
					t.Errorf("existing bucket unavailable: %v", err)
				}
			case "NoSuchBucket":
				if store != nil || err == nil || !strings.Contains(err.Error(), "does not exist") {
					t.Errorf("missing bucket must fail initialization: %v", err)
				}
			case "AccessDenied":
				var cause minio.ErrorResponse
				if store != nil || !errors.As(err, &cause) || cause.Code != tc.code {
					t.Errorf("bucket denial lost SDK cause: %v", err)
				}
			}
			if n := requests.Load(); n != 1 {
				t.Errorf("expected exactly one HEAD request, got %d", n)
			}
		})
	}
}
