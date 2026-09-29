package fileobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// All paths, including outside the object root, belong to this test's TempDir.
// No real mail, host configuration or external service is accessed.
func objectFixture(t *testing.T) (*FileStore, string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "objects")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "mail.eml"), []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, root, outside
}
func objectOperation(s *FileStore, ctx context.Context, op, key string) error {
	switch op {
	case "get":
		r, err := s.Get(ctx, key)
		if r != nil {
			_ = r.Close()
		}
		return err
	case "exists":
		_, err := s.Exists(ctx, key)
		return err
	case "delete":
		return s.Delete(ctx, key)
	default:
		return s.Put(ctx, key, strings.NewReader("replacement"), 11)
	}
}
func requireOutsideUnchanged(t *testing.T, outside string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(outside, "mail.eml"))
	if err != nil || string(b) != "outside fixture" {
		t.Fatalf("outside fixture changed: %v", err)
	}
}
func TestFileObjectRejectsNonCanonicalKeys(t *testing.T) {
	for _, op := range []string{"get", "exists", "delete", "put"} {
		for _, key := range []string{"", ".", "../outside/mail.eml", "a/../mail.eml", "a//mail.eml", "a\\mail.eml"} {
			t.Run(op+"/"+key, func(t *testing.T) {
				s, _, outside := objectFixture(t)
				if err := objectOperation(s, context.Background(), op, key); err == nil {
					t.Error("invalid object key accepted")
				}
				requireOutsideUnchanged(t, outside)
			})
		}
		t.Run(op+"/absolute", func(t *testing.T) {
			s, _, outside := objectFixture(t)
			if err := objectOperation(s, context.Background(), op, filepath.Join(outside, "mail.eml")); err == nil {
				t.Error("absolute object key accepted")
			}
			requireOutsideUnchanged(t, outside)
		})
	}
}
func TestFileObjectRejectsEscapingSymlinks(t *testing.T) {
	for _, op := range []string{"get", "exists", "delete", "put"} {
		for _, kind := range []string{"parent", "leaf"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				s, root, outside := objectFixture(t)
				key, target := "alias/mail.eml", outside
				link := filepath.Join(root, "alias")
				if kind == "leaf" {
					key = "mail.eml"
					target = filepath.Join(outside, "mail.eml")
					link = filepath.Join(root, key)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				if err := objectOperation(s, context.Background(), op, key); err == nil {
					t.Error("escaping symbolic link accepted")
				}
				requireOutsideUnchanged(t, outside)
			})
		}
	}
}
func TestFileObjectRejectsDirectoryObjects(t *testing.T) {
	for _, op := range []string{"get", "exists", "delete", "put"} {
		t.Run(op, func(t *testing.T) {
			s, root, _ := objectFixture(t)
			dir := filepath.Join(root, "directory")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := objectOperation(s, context.Background(), op, "directory"); err == nil {
				t.Error("directory treated as blob")
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				t.Error("directory removed")
			}
		})
	}
}
func TestFileObjectCancelledOperationsHaveNoEffects(t *testing.T) {
	for _, op := range []string{"get", "exists", "delete", "put"} {
		t.Run(op, func(t *testing.T) {
			s, _, _ := objectFixture(t)
			ctx := context.Background()
			if err := s.Put(ctx, "mail.eml", strings.NewReader("original"), 8); err != nil {
				t.Fatal(err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err := objectOperation(s, cancelled, op, "mail.eml"); !errors.Is(err, context.Canceled) {
				t.Errorf("cancel not propagated: %v", err)
			}
			r, err := s.Get(ctx, "mail.eml")
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			if err != nil || string(b) != "original" {
				t.Error("cancelled operation changed object")
			}
		})
	}
}
func TestFileObjectReadHonorsCancellation(t *testing.T) {
	s, _, _ := objectFixture(t)
	if err := s.Put(context.Background(), "mail.eml", strings.NewReader("original"), 8); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := s.Get(ctx, "mail.eml")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cancel()
	b := make([]byte, 16)
	n, err := r.Read(b)
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled read returned %d bytes, %v", n, err)
	}
}
func TestFileObjectChecksDeclaredLengthBeforePublication(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		size       int64
	}{{"short", "abc", 4}, {"extra", "abcde", 4}, {"invalid-negative", "abc", -2}, {"false-empty", "abc", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s, root, _ := objectFixture(t)
			ctx := context.Background()
			if err := s.Put(ctx, "mail.eml", strings.NewReader("original"), 8); err != nil {
				t.Fatal(err)
			}
			if err := s.Put(ctx, "mail.eml", strings.NewReader(tc.body), tc.size); err == nil {
				t.Error("wrong-length publication succeeded")
			}
			b, err := os.ReadFile(filepath.Join(root, "mail.eml"))
			if err != nil || string(b) != "original" {
				t.Error("invalid source replaced original")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Error("temporary file leaked")
			}
		})
	}
}

type noProgressObjectReader struct{ reads int }

func (r *noProgressObjectReader) Read([]byte) (int, error) {
	r.reads++
	if r.reads > 102 {
		return 0, io.EOF
	}
	return 0, nil
}
func TestFileObjectNoProgressAbortsPublication(t *testing.T) {
	s, root, _ := objectFixture(t)
	r := &noProgressObjectReader{}
	err := s.Put(context.Background(), "mail.eml", r, -1)
	if !errors.Is(err, io.ErrNoProgress) || r.reads > 100 {
		t.Errorf("no-progress source: reads=%d err=%v", r.reads, err)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Error("failed source published or leaked temp file")
	}
}
func TestFileObjectRelativeRootIsStable(t *testing.T) {
	base := t.TempDir()
	first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
	for _, d := range []string{first, second} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(first)
	s, err := New("objects")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put(context.Background(), "mail.eml", strings.NewReader("original"), 8); err != nil {
		t.Fatal(err)
	}
	t.Chdir(second)
	r, err := s.Get(context.Background(), "mail.eml")
	if err != nil {
		t.Fatal("root changed with working directory", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "original" {
		t.Error("wrong root bytes")
	}
}
func TestFileObjectValidLifecycleAndAtomicReaders(t *testing.T) {
	s, root, _ := objectFixture(t)
	ctx := context.Background()
	key := "sha256/ab/test.eml"
	for _, tc := range []struct {
		body string
		size int64
	}{{"", 0}, {"abc", 3}, {"unknown length", -1}} {
		if err := s.Put(ctx, key, strings.NewReader(tc.body), tc.size); err != nil {
			t.Fatal(err)
		}
		r, err := s.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		b, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil || string(b) != tc.body {
			t.Fatal("round trip mismatch", e)
		}
	}
	a, b := strings.Repeat("a", 32768), strings.Repeat("b", 32768)
	if err := s.Put(ctx, key, strings.NewReader(a), int64(len(a))); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				if i%2 == 0 {
					if err := s.Put(ctx, key, strings.NewReader(b), int64(len(b))); err != nil {
						errs <- err
					}
					continue
				}
				r, err := s.Get(ctx, key)
				if err != nil {
					errs <- err
					continue
				}
				raw, e := io.ReadAll(r)
				_ = r.Close()
				if e != nil {
					errs <- e
				} else if string(raw) != a && string(raw) != b {
					errs <- fmt.Errorf("partial object observed")
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if st, err := os.Stat(filepath.Join(root, key)); err != nil || st.Mode().Perm()&0077 != 0 {
		t.Error("blob not private", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(ctx, key); err != nil || exists {
		t.Fatal("missing object not idempotent", err)
	}
}
