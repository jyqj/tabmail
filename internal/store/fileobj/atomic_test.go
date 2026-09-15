package fileobj

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenReader struct{ read bool }

func (r *brokenReader) Read(b []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(b, "partial"), nil
	}
	return 0, errors.New("reader interrupted")
}
func TestAtomicSpoolPreservesExistingObjectOnFailure(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "nested/mail.eml"
	if err = s.Put(ctx, key, strings.NewReader("original"), 8); err != nil {
		t.Fatal(err)
	}
	if err = s.Put(ctx, key, &brokenReader{}, 100); err == nil {
		t.Fatal("interrupted write succeeded")
	}
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "original" {
		t.Fatalf("object truncated: %q %v", b, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "nested"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %v %v", entries, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.Put(cancelled, key, strings.NewReader("replacement"), 11); err == nil {
		t.Fatal("cancelled write published")
	}
}
func TestAtomicSpoolRejectsEscapingKeys(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../escape", "/absolute", ""} {
		if err = s.Put(context.Background(), key, strings.NewReader("x"), 1); err == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
	}
}
