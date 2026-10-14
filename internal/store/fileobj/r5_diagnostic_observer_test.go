//go:build r5benchmark

package fileobj

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fileDiagnosticEvents struct {
	mu        sync.Mutex
	stages    []string
	failed    []bool
	callbacks int
}

func (v *fileDiagnosticEvents) ObserveFileOperation(_ context.Context, stage string, start, end time.Time, failed bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.stages = append(v.stages, stage)
	v.failed = append(v.failed, failed)
	v.callbacks++
}
func TestR5FileDiagnosticExactOperations(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if beginFileDiagnostic(context.Background(), "file_sync") != nil {
		t.Fatal("tag alone enabled observer")
	}
	v := &fileDiagnosticEvents{}
	ctx := WithR5DiagnosticObserver(context.Background(), v)
	raw := "From: synthetic@test\r\n\r\nbody"
	if e = s.Put(ctx, "sha256/aa/fixture.eml", strings.NewReader(raw), int64(len(raw))); e != nil {
		t.Fatal(e)
	}
	want := []string{"copy_and_declared_length_validation", "file_sync", "file_close", "rename", "dir_sync_leaf", "dir_close", "dir_sync_parent", "dir_close", "dir_sync_root", "dir_close"}
	if len(v.stages) != len(want) {
		t.Fatalf("filesystem operation count changed: %v", v.stages)
	}
	for i, name := range want {
		if v.stages[i] != name || v.failed[i] {
			t.Fatalf("operation%d changed: %v", i, v.stages)
		}
	}
	r, e := s.Get(context.Background(), "sha256/aa/fixture.eml")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(r)
	closeErr := r.Close()
	if e != nil || closeErr != nil || string(b) != raw {
		t.Fatal("observer changed published bytes")
	}
}

type fileDiagnosticBrokenReader struct{ err error }

func (r fileDiagnosticBrokenReader) Read([]byte) (int, error) { return 0, r.err }
func TestR5FileDiagnosticFailureCause(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	v := &fileDiagnosticEvents{}
	injected := errors.New("injected reader failure")
	e = s.Put(WithR5DiagnosticObserver(context.Background(), v), "sha256/aa/broken.eml", fileDiagnosticBrokenReader{injected}, 16)
	if !errors.Is(e, injected) || len(v.stages) != 2 || v.stages[0] != "copy_and_declared_length_validation" || !v.failed[0] || v.stages[1] != "file_close_failure_cleanup" {
		t.Fatalf("observer changed failure/publication sequence: err=%v stages=%v", e, v.stages)
	}
	exists, e := s.Exists(context.Background(), "sha256/aa/broken.eml")
	if e != nil || exists {
		t.Fatal("failed observed write published")
	}
}
func TestR5FileDiagnosticConcurrentObservers(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	v := &fileDiagnosticEvents{}
	ctx := WithR5DiagnosticObserver(context.Background(), v)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Put(ctx, "sha256/aa/fixture-"+string(rune('a'+n))+".eml", strings.NewReader("body"), 4)
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if v.callbacks != 40 {
		t.Fatalf("concurrent observe count=%d", v.callbacks)
	}
}
