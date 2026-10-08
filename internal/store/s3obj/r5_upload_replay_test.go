package s3obj_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestR5S3UploadDoesNotReplayAConsumedCallerStream(t *testing.T) {
	f := newUploadBoundaryFixture(t)
	f.rejectFirstPut = true
	// The first stream ends at EOF. A caller may expose later data after that
	// boundary; the SDK must not treat its own no-op seek as a rewind.
	base := strings.NewReader("first")
	reader := &boundaryUploadReader{read: func(p []byte) (int, error) {
		n, err := base.Read(p)
		if err == io.EOF {
			base = strings.NewReader("other")
		}
		return n, err
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err, panicValue := putBoundaryWithoutPanic(ctx, f, reader, 5)
	if err == nil || panicValue != nil {
		t.Errorf("consumed input was replayed: err=%v panic=%v", err, panicValue)
	}
	f.unchanged(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		t.Errorf("sent %d requests for one non-rewindable source", len(f.requests))
	}
}
