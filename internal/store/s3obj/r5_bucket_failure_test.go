package s3obj_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"tabmail/internal/config"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/s3obj"
)

// Exercise the shipping MinIO client over HTTP, including its header fallback
// for HEAD errors. The bucket exists at startup; object requests then observe
// the configured result, as they would after a bucket is removed or access lost.
func bucketBoundaryStore(t *testing.T, code string, status int) *s3obj.Store {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && r.URL.Path == "/mail-bucket/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/mail-bucket/original.eml" || (r.Method != http.MethodHead && r.Method != http.MethodDelete) {
			t.Errorf("unexpected S3 request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if code == "" {
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.Header().Set("Last-Modified", time.Unix(1, 0).UTC().Format(http.TimeFormat))
			w.Header().Set("Content-Length", "0")
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(status)
			}
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("X-Minio-Error-Code", code)
		w.Header().Set("X-Amz-Request-Id", "owned-loopback-request")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>storage boundary failure</Message><BucketName>mail-bucket</BucketName><Key>original.eml</Key></Error>", code)
		}
	}))
	t.Cleanup(server.Close)
	st, err := s3obj.New(config.S3{
		Endpoint: strings.TrimPrefix(server.URL, "http://"), Bucket: "mail-bucket",
		AccessKey: "owned-test-access", SecretKey: "owned-test-secret",
		Region: "us-east-1", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("initialize loopback S3 store: %v", err)
	}
	return st
}

func TestR5S3MissingBucketIsStorageFailure(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		status     int
		missing    bool
	}{
		{name: "existing-object", status: http.StatusOK},
		{name: "missing-key", code: "NoSuchKey", status: http.StatusNotFound, missing: true},
		{name: "missing-object", code: "NoSuchObject", status: http.StatusNotFound, missing: true},
		{name: "missing-bucket", code: "NoSuchBucket", status: http.StatusNotFound},
		{name: "access-denied", code: "AccessDenied", status: http.StatusForbidden},
		{name: "invalid-request", code: "InvalidRequest", status: http.StatusBadRequest},
	} {
		for _, op := range []string{"exists", "delete"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				st := bucketBoundaryStore(t, tc.code, tc.status)
				var err error
				if op == "exists" {
					var exists bool
					exists, err = st.Exists(context.Background(), "original.eml")
					if exists != (tc.code == "") {
						t.Errorf("exists = %v for %s", exists, tc.name)
					}
				} else {
					err = st.Delete(context.Background(), "original.eml")
				}
				if tc.code == "" || tc.missing {
					if err != nil {
						t.Fatalf("expected successful/idempotent object operation: %v", err)
					}
					return
				}
				var cause minio.ErrorResponse
				if !errors.As(err, &cause) || cause.Code != tc.code {
					t.Fatalf("storage failure must retain %s cause; got %v", tc.code, err)
				}
			})
		}
	}
}

type unreferencedObject struct{}

func (unreferencedObject) ReleaseRawObjectIfUnreferenced(ctx context.Context, _ string, del func(context.Context) error) (bool, error) {
	if err := del(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func TestR5S3ReleaseReportsMissingBucketFailure(t *testing.T) {
	for _, code := range []string{"NoSuchBucket", "NoSuchKey"} {
		t.Run(code, func(t *testing.T) {
			st := bucketBoundaryStore(t, code, http.StatusNotFound)
			outcome, err := rawobject.Release(context.Background(), unreferencedObject{}, st, "original.eml")
			if code == "NoSuchKey" {
				if outcome != rawobject.Deleted || err != nil {
					t.Fatalf("already absent key remains an idempotent deletion: %v, %v", outcome, err)
				}
				return
			}
			var cause minio.ErrorResponse
			if outcome != rawobject.DeleteFailed || !errors.As(err, &cause) || cause.Code != code {
				t.Fatalf("reaper must retain retryable storage failure, got %v, %v", outcome, err)
			}
		})
	}
}

func TestR5S3CancelledOperationKeepsCancellation(t *testing.T) {
	for _, op := range []string{"exists", "delete"} {
		t.Run(op, func(t *testing.T) {
			st := bucketBoundaryStore(t, "", http.StatusOK)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var err error
			if op == "exists" {
				_, err = st.Exists(ctx, "original.eml")
			} else {
				err = st.Delete(ctx, "original.eml")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation must remain identifiable: %v", err)
			}
		})
	}
}
