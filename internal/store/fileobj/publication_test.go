package fileobj

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type objectCallbackReader struct {
	reader      io.Reader
	callback    func()
	eofWithData bool
}

func (r *objectCallbackReader) Read(b []byte) (int, error) {
	if r.callback != nil {
		f := r.callback
		r.callback = nil
		f()
	}
	n, err := r.reader.Read(b)
	if r.eofWithData && n > 0 {
		err = io.EOF
	}
	return n, err
}
func TestFileObjectParentSwapCannotRedirectPublication(t *testing.T) {
	s, root, outside := objectFixture(t)
	ctx := context.Background()
	if err := s.Put(ctx, "nested/mail.eml", strings.NewReader("original"), 8); err != nil {
		t.Fatal(err)
	}
	var decoy string
	input := &objectCallbackReader{reader: strings.NewReader("replacement"), callback: func() {
		// Replace only this test's directory while its writer is inside Read.
		moved := filepath.Join(root, "moved")
		if err := os.Rename(filepath.Join(root, "nested"), moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(moved)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".ingress-") {
				decoy = filepath.Join(outside, entry.Name())
				if err := os.WriteFile(decoy, []byte("outside temporary fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if decoy == "" {
			t.Fatal("writer did not create its temporary object before reading")
		}
	}}
	if err := s.Put(ctx, "nested/mail.eml", input, 11); err == nil {
		t.Error("parent swap published through escaping link")
	}
	requireOutsideUnchanged(t, outside)
	b, err := os.ReadFile(filepath.Join(root, "moved/mail.eml"))
	if err != nil || string(b) != "original" {
		t.Error("original object changed")
	}
	entries, err := os.ReadDir(filepath.Join(root, "moved"))
	if err != nil || len(entries) != 1 {
		t.Error("temporary object leaked in moved parent")
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Error("store touched outside temporary fixture")
	}
}
func TestFileObjectPublicationPreservesEOFAndCancellation(t *testing.T) {
	for _, mode := range []string{"eof-with-data", "cancel-at-eof", "nil-source"} {
		t.Run(mode, func(t *testing.T) {
			s, root, _ := objectFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Put(ctx, "mail.eml", strings.NewReader("original"), 8); err != nil {
				t.Fatal(err)
			}
			var input io.Reader = &objectCallbackReader{reader: strings.NewReader("new"), eofWithData: true}
			if mode == "cancel-at-eof" {
				input = &objectCallbackReader{reader: strings.NewReader("new"), eofWithData: true, callback: cancel}
			}
			if mode == "nil-source" {
				input = nil
			}
			err := func() (err error) {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Error("adapter panicked for invalid input")
						err = errors.New("invalid-input panic")
					}
				}()
				return s.Put(ctx, "mail.eml", input, 3)
			}()
			want := "original"
			if mode == "eof-with-data" {
				want = "new"
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Error("invalid write succeeded")
			}
			if mode == "cancel-at-eof" && !errors.Is(err, context.Canceled) {
				t.Error("cancel cause lost")
			}
			b, e := os.ReadFile(filepath.Join(root, "mail.eml"))
			if e != nil || string(b) != want {
				t.Error("publication changed wrong bytes")
			}
			entries, e := os.ReadDir(root)
			if e != nil || len(entries) != 1 {
				t.Error("temporary file leaked")
			}
		})
	}
}

type countingObjectReader struct{ consumed int64 }

func (r *countingObjectReader) Read(b []byte) (int, error) {
	// Bound the baseline fixture even when the old adapter ignores declared size.
	if r.consumed >= 8192 {
		return 0, io.EOF
	}
	if int64(len(b)) > 8192-r.consumed {
		b = b[:8192-r.consumed]
	}
	for i := range b {
		b[i] = 'x'
	}
	r.consumed += int64(len(b))
	return len(b), nil
}
func TestFileObjectDeclaredLengthBoundsConsumption(t *testing.T) {
	s, _, _ := objectFixture(t)
	r := &countingObjectReader{}
	if err := s.Put(context.Background(), "mail.eml", r, 3); err == nil {
		t.Error("unbounded source accepted")
	}
	if r.consumed != 4 {
		t.Errorf("consumed %d bytes, want declared size plus one", r.consumed)
	}
}
func TestFileObjectCopyCannotBypassCancelledReader(t *testing.T) {
	s, _, _ := objectFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Put(ctx, "mail.eml", strings.NewReader("original"), 8); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, "mail.eml")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cancel()
	var out bytes.Buffer
	n, err := io.Copy(&out, r)
	if n != 0 || out.Len() != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("copy bypassed context: bytes=%d err=%v", n, err)
	}
}
func TestFileObjectInternalParentLinkRemainsSupported(t *testing.T) {
	s, root, _ := objectFixture(t)
	if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Put(ctx, "alias/mail.eml", strings.NewReader("inside"), 6); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, "real/mail.eml")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "inside" {
		t.Error("internal path changed")
	}
	if err := s.Delete(ctx, "alias/mail.eml"); err != nil {
		t.Fatal(err)
	}
}
