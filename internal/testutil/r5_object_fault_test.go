package testutil_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"tabmail/internal/testutil"
	"testing"
)

func TestR5ObjectFaultRealPortAndPrivacy(t *testing.T) {
	base := testutil.NewMemoryObjectStore()
	f := testutil.NewR5ObjectFault(base)
	ctx := context.Background()
	key := "test-only-private-object-key"
	if err := f.Put(ctx, key, strings.NewReader("hello world"), 11); err != nil {
		t.Fatal(err)
	}
	if err := f.FailNext("get", testutil.ErrR5ObjectFault); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Get(ctx, key); !errors.Is(err, testutil.ErrR5ObjectFault) {
		t.Fatal("get fault not observed")
	}
	if err := f.ShortNextGet(3); err != nil {
		t.Fatal(err)
	}
	reader, err := f.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != "hel" || base.Count() != 1 {
		t.Fatal("short read altered source object")
	}
	if err = f.FailNext("delete", testutil.ErrR5ObjectFault); err != nil {
		t.Fatal(err)
	}
	if err = f.Delete(ctx, key); !errors.Is(err, testutil.ErrR5ObjectFault) || base.Count() != 1 {
		t.Fatal("failed delete changed source")
	}
	if err = f.Delete(ctx, key); err != nil || base.Count() != 0 {
		t.Fatal("single-shot fault did not restore real delete")
	}
	for _, call := range f.Calls() {
		if len(call.KeySHA256) != 64 || call.KeySHA256 == key {
			t.Fatal("object call leaked raw key")
		}
	}
}
func TestR5ObjectFaultCancellationAndInvalidConfiguration(t *testing.T) {
	base := testutil.NewMemoryObjectStore()
	f := testutil.NewR5ObjectFault(base)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Put(ctx, "canceled", strings.NewReader("ignored"), 7); !errors.Is(err, context.Canceled) || base.Count() != 0 {
		t.Fatal("canceled write reached port")
	}
	if err := f.FailNext("unknown", errors.New("failure")); err == nil {
		t.Fatal("unknown operation accepted")
	}
	if err := f.FailNext("put", nil); err == nil {
		t.Fatal("nil fault accepted")
	}
	if err := f.ShortNextGet(-1); err == nil {
		t.Fatal("negative limit accepted")
	}
	if _, err := testutil.NewR5ObjectFault(nil).Exists(context.Background(), "missing"); err == nil {
		t.Fatal("missing real object port accepted")
	}
}
