package s3obj_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/s3obj"
)

// The shipping MinIO client talks to an owned HTTP object namespace. Request
// paths are inspected after one URL decode, as an S3 endpoint sees them; the
// handler does not clean paths or otherwise normalize object identities.
type r5S3KeyFixture struct {
	store    *s3obj.Store
	mu       sync.Mutex
	objects  map[string][]byte
	requests []string
}

func r5S3KeyStore(t *testing.T) *r5S3KeyFixture {
	t.Helper()
	f := &r5S3KeyFixture{objects: map[string][]byte{
		"original.eml":        []byte("referenced original"),
		"nested/original.eml": []byte("second referenced original"),
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && r.URL.Path == "/mail-bucket/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.EscapedPath())
		if !strings.HasPrefix(r.URL.Path, "/mail-bucket/") || r.URL.RawQuery != "" {
			t.Errorf("unexpected bucket or query: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/mail-bucket/")
		if r.Method == http.MethodPut {
			var reader io.Reader = r.Body
			// HTTP S3 uses AWS signed chunks inside its request body. Decoding
			// those chunks here preserves a real client/HTTP/byte round trip.
			if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-AWS4-HMAC-SHA256-PAYLOAD") {
				reader = httputil.NewChunkedReader(reader)
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("read owned S3 upload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.objects[key] = body
			w.Header().Set("ETag", `"owned-synthetic-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		body, exists := f.objects[key]
		if r.Method == http.MethodDelete {
			delete(f.objects, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !exists {
			w.Header().Set("X-Minio-Error-Code", "NoSuchKey")
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code><Message>owned key absent</Message></Error>")
			}
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Last-Modified", time.Unix(1, 0).UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"owned-synthetic-etag"`)
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			_, _ = w.Write(body)
		default:
			t.Errorf("unexpected object operation: %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	var err error
	f.store, err = s3obj.New(config.S3{
		Endpoint: strings.TrimPrefix(server.URL, "http://"), Bucket: "mail-bucket",
		AccessKey: "owned-test-access", SecretKey: "owned-test-secret",
		Region: "us-east-1", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("initialize owned S3 store: %v", err)
	}
	return f
}

func (f *r5S3KeyFixture) snapshot() (map[string][]byte, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	objects := make(map[string][]byte, len(f.objects))
	for key, body := range f.objects {
		objects[key] = bytes.Clone(body)
	}
	return objects, append([]string(nil), f.requests...)
}

func r5S3KeyOperation(ctx context.Context, st *s3obj.Store, op, key string) error {
	switch op {
	case "put":
		return st.Put(ctx, key, strings.NewReader("replacement"), 11)
	case "get":
		reader, err := st.Get(ctx, key)
		if reader != nil {
			_, readErr := io.ReadAll(reader)
			err = errors.Join(err, readErr, reader.Close())
		}
		return err
	case "exists":
		_, err := st.Exists(ctx, key)
		return err
	case "delete":
		return st.Delete(ctx, key)
	default:
		panic("unknown test operation")
	}
}

func TestR5S3ObjectKeyRejectsAmbiguousIdentity(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{"leading-slash", "/original.eml"},
		{"leading-space", " original.eml"},
		{"trailing-space", "original.eml "},
		{"unicode-space", "\u00a0original.eml"},
		{"parent-segment", "nested/../original.eml"},
		{"dot-segment", "./original.eml"},
		{"repeated-slash", "nested//original.eml"},
		{"trailing-slash", "original.eml/"},
		{"parent", "../original.eml"},
		{"backslash", `nested\original.eml`},
		{"nul", "original.eml\x00"},
		{"empty", ""},
		{"dot", "."},
	} {
		for _, op := range []string{"put", "get", "exists", "delete"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				f := r5S3KeyStore(t)
				before, _ := f.snapshot()
				if err := r5S3KeyOperation(context.Background(), f.store, op, tc.key); err == nil {
					t.Error("ambiguous key was accepted as an object identity")
				}
				after, requests := f.snapshot()
				if len(requests) != 0 {
					t.Errorf("ambiguous key reached the object backend: %v", requests)
				}
				if !reflect.DeepEqual(before, after) {
					t.Error("rejected key changed the object namespace or a referenced original")
				}
			})
		}
	}
}

func TestR5S3ObjectKeyPreservesCanonicalBytes(t *testing.T) {
	for _, key := range []string{
		rawobject.Key([]byte("synthetic raw mail")),
		"ingress-00000000-0000-4000-8000-000000000001.eml",
		"attachment-00000000-0000-4000-8000-000000000001",
		"legacy/nested/original.eml",
		"legacy/internal space.eml",
		"legacy/邮件.eml",
		"legacy/literal%2Fquestion?fragment#.eml",
		"legacy/%2e%2e/original.eml",
	} {
		t.Run(key, func(t *testing.T) {
			f := r5S3KeyStore(t)
			ctx := context.Background()
			body := []byte("owned round-trip content")
			if err := f.store.Put(ctx, key, bytes.NewReader(body), int64(len(body))); err != nil {
				t.Fatalf("put canonical key: %v", err)
			}
			objects, _ := f.snapshot()
			if !bytes.Equal(objects[key], body) {
				t.Fatal("upload did not preserve the exact key and bytes")
			}
			if exists, err := f.store.Exists(ctx, key); err != nil || !exists {
				t.Fatalf("existing canonical key: exists=%v err=%v", exists, err)
			}
			reader, err := f.store.Get(ctx, key)
			if err != nil {
				t.Fatalf("get canonical key: %v", err)
			}
			got, readErr := io.ReadAll(reader)
			if err = errors.Join(readErr, reader.Close()); err != nil || !bytes.Equal(got, body) {
				t.Fatalf("canonical read changed bytes: %q, %v", got, err)
			}
			if err = f.store.Delete(ctx, key); err != nil {
				t.Fatalf("delete canonical key: %v", err)
			}
			objects, _ = f.snapshot()
			if _, exists := objects[key]; exists || string(objects["original.eml"]) != "referenced original" {
				t.Fatal("canonical deletion removed a different object")
			}
		})
	}
}

func TestR5S3ObjectKeyPreservesMissingAndCancellation(t *testing.T) {
	for _, op := range []string{"put", "get", "exists", "delete"} {
		t.Run("cancelled/"+op, func(t *testing.T) {
			f := r5S3KeyStore(t)
			before, _ := f.snapshot()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := r5S3KeyOperation(ctx, f.store, op, "original.eml"); !errors.Is(err, context.Canceled) {
				t.Errorf("operation lost cancellation cause: %v", err)
			}
			after, requests := f.snapshot()
			if len(requests) != 0 || !reflect.DeepEqual(before, after) {
				t.Error("cancelled operation reached or modified backend")
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		f := r5S3KeyStore(t)
		ctx := context.Background()
		if exists, err := f.store.Exists(ctx, "absent.eml"); err != nil || exists {
			t.Fatalf("absent key must not be a storage failure: exists=%v err=%v", exists, err)
		}
		if err := f.store.Delete(ctx, "absent.eml"); err != nil {
			t.Fatalf("absent key delete remains idempotent: %v", err)
		}
		reader, err := f.store.Get(ctx, "absent.eml")
		if err == nil || reader != nil {
			t.Fatalf("absent read unexpectedly succeeded: %v", err)
		}
	})
}
