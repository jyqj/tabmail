package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/company"
)

type verificationObject struct {
	reader io.ReadCloser
	err    error
	onGet  func()
	opens  int
}

func (s *verificationObject) Get(context.Context, string) (io.ReadCloser, error) {
	s.opens++
	if s.onGet != nil {
		s.onGet()
	}
	return s.reader, s.err
}

type verificationReader struct {
	read     func([]byte) (int, error)
	onClose  func()
	closeErr error
	closes   atomic.Int32
}

func (r *verificationReader) Read(p []byte) (int, error) { return r.read(p) }
func (r *verificationReader) Close() error {
	r.closes.Add(1)
	if r.onClose != nil {
		r.onClose()
	}
	return r.closeErr
}

func originalReceipt(raw string) *company.RecoveryReceipt {
	return &company.RecoveryReceipt{RawKey: "controlled-original", RawSize: int64(len(raw)), RawHash: company.Hash(raw)}
}

func TestR5RecoveryVerifyIntegrity(t *testing.T) {
	readFailure := errors.New("controlled read failure")
	for _, tc := range []struct {
		name     string
		raw      string
		size     int64
		hash     string
		lastErr  error
		closeErr error
		want     bool
	}{
		{"exact", "original", 8, company.Hash("original"), io.EOF, nil, true},
		{"empty", "", 0, company.Hash(""), io.EOF, nil, true},
		{"short", "original", 9, company.Hash("original"), io.EOF, nil, false},
		{"extra", "original!", 8, company.Hash("original"), io.EOF, nil, false},
		{"wrong_hash", "original", 8, company.Hash("different"), io.EOF, nil, false},
		{"data_and_error", "original", 8, company.Hash("original"), readFailure, nil, false},
		{"close_error", "original", 8, company.Hash("original"), io.EOF, errors.New("controlled close failure"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.raw
			read := &verificationReader{closeErr: tc.closeErr, read: func(p []byte) (int, error) {
				n := copy(p, data)
				data = data[n:]
				if len(data) == 0 {
					return n, tc.lastErr
				}
				return n, nil
			}}
			objects := &verificationObject{reader: read}
			receipt := &company.RecoveryReceipt{RawKey: "controlled-original", RawSize: tc.size, RawHash: tc.hash}
			if got := Verify(context.Background(), objects, receipt); got != tc.want {
				t.Errorf("Verify = %v, want %v", got, tc.want)
			}
			if objects.opens != 1 || read.closes.Load() != 1 {
				t.Errorf("opens=%d closes=%d, want one owned read", objects.opens, read.closes.Load())
			}
		})
	}
}

func TestR5RecoveryVerifyRejectsMetadataBeforeOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*company.RecoveryReceipt)
	}{
		{"negative_size", func(v *company.RecoveryReceipt) { v.RawSize = -1 }},
		{"oversized", func(v *company.RecoveryReceipt) { v.RawSize = 25*1024*1024 + 1 }},
		{"overflow_size", func(v *company.RecoveryReceipt) { v.RawSize = 1<<63 - 1 }},
		{"missing_key", func(v *company.RecoveryReceipt) { v.RawKey = "" }},
		{"missing_hash", func(v *company.RecoveryReceipt) { v.RawHash = "" }},
		{"short_hash", func(v *company.RecoveryReceipt) { v.RawHash = "bad" }},
		{"non_hex_hash", func(v *company.RecoveryReceipt) { v.RawHash = strings.Repeat("z", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := originalReceipt("original")
			tc.edit(v)
			objects := &verificationObject{reader: io.NopCloser(strings.NewReader("original"))}
			if Verify(context.Background(), objects, v) || objects.opens != 0 {
				t.Fatalf("invalid metadata opened source: opens=%d", objects.opens)
			}
		})
	}
}

func TestR5RecoveryVerifyObjectFailureOwnership(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reader  bool
		failure bool
	}{
		{"nil_reader", false, false},
		{"get_error", false, true},
		{"reader_with_get_error", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := &verificationObject{}
			r := &verificationReader{read: func([]byte) (int, error) { t.Error("failed source was read"); return 0, io.EOF }}
			if tc.reader {
				objects.reader = r
			}
			if tc.failure {
				objects.err = errors.New("controlled object failure")
			}
			if Verify(context.Background(), objects, originalReceipt("")) {
				t.Error("failed object verified")
			}
			if tc.reader && r.closes.Load() != 1 {
				t.Errorf("returned reader closed %d times, want one", r.closes.Load())
			}
		})
	}
	if Verify(context.Background(), nil, originalReceipt("")) || Verify(context.Background(), &verificationObject{}, nil) {
		t.Fatal("missing dependency or receipt verified")
	}
}

func TestR5RecoveryVerifyEmptyReadsHaveBound(t *testing.T) {
	for _, emptyReads := range []int{99, 101} {
		name := "eventual_progress"
		if emptyReads > 100 {
			name = "no_progress"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			data := strings.NewReader("original")
			r := &verificationReader{read: func(p []byte) (int, error) {
				calls++
				if calls <= emptyReads {
					return 0, nil
				}
				return data.Read(p)
			}}
			got := Verify(context.Background(), &verificationObject{reader: r}, originalReceipt("original"))
			if got != (emptyReads < 100) || emptyReads > 100 && calls > 100 {
				t.Errorf("Verify=%v after %d reads, empty prefix=%d", got, calls, emptyReads)
			}
			if r.closes.Load() != 1 {
				t.Errorf("closes=%d, want one", r.closes.Load())
			}
		})
	}
}

func TestR5RecoveryVerifyRejectsInvalidReadCounts(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		name := "negative"
		if oversized {
			name = "larger_than_buffer"
		}
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("invalid reader panicked: %v", recovered)
				}
			}()
			r := &verificationReader{read: func(p []byte) (int, error) {
				if oversized {
					return len(p) + 1, io.EOF
				}
				return -1, io.EOF
			}}
			if Verify(context.Background(), &verificationObject{reader: r}, originalReceipt("original")) || r.closes.Load() != 1 {
				t.Fatal("invalid reader accepted or not closed")
			}
		})
	}
}

func TestR5RecoveryVerifyCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"before_open", "during_open", "last_read", "close"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &verificationReader{read: func(p []byte) (int, error) {
				if stage == "last_read" {
					cancel()
				}
				return copy(p, "original"), io.EOF
			}}
			objects := &verificationObject{reader: r}
			if stage == "before_open" {
				cancel()
			}
			if stage == "during_open" {
				objects.onGet = cancel
			}
			if stage == "close" {
				r.onClose = cancel
			}
			if Verify(ctx, objects, originalReceipt("original")) {
				t.Error("cancelled verification succeeded")
			}
			wantOpens := 1
			if stage == "before_open" {
				wantOpens = 0
			}
			if objects.opens != wantOpens || r.closes.Load() != int32(wantOpens) {
				t.Errorf("opens=%d closes=%d, want %d", objects.opens, r.closes.Load(), wantOpens)
			}
		})
	}
}

func TestR5RecoveryVerifyCancellationClosesBlockedReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, closed := make(chan struct{}), make(chan struct{})
	var startOnce, closeOnce sync.Once
	r := &verificationReader{
		read:    func([]byte) (int, error) { startOnce.Do(func() { close(started) }); <-closed; return 0, io.EOF },
		onClose: func() { closeOnce.Do(func() { close(closed) }) },
	}
	done := make(chan bool, 1)
	go func() { done <- Verify(ctx, &verificationObject{reader: r}, originalReceipt("")) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		_ = r.Close()
		t.Fatal("verification did not start reading")
	}
	cancel()
	select {
	case got := <-done:
		if got {
			t.Error("cancelled blocked read verified")
		}
	case <-time.After(time.Second):
		// Release the baseline safely even when production missed cancellation.
		_ = r.Close()
		<-done
		t.Error("cancellation did not close the owned reader")
	}
	if r.closes.Load() != 1 {
		t.Errorf("owned reader closed %d times, want one", r.closes.Load())
	}
}

func TestR5RecoveryVerifyMaxSizeStreamsWithinFixedBuffer(t *testing.T) {
	const size = 25 * 1024 * 1024
	block := bytes.Repeat([]byte{'x'}, 32*1024)
	hash := sha256.New()
	for total := 0; total < size; total += len(block) {
		_, _ = hash.Write(block)
	}
	remaining, maxRead := size, 0
	r := &verificationReader{read: func(p []byte) (int, error) {
		maxRead = max(maxRead, len(p))
		n := min(len(p), remaining)
		for i := 0; i < n; i++ {
			p[i] = 'x'
		}
		remaining -= n
		if remaining == 0 {
			return n, io.EOF
		}
		return n, nil
	}}
	v := &company.RecoveryReceipt{RawKey: "controlled-large-original", RawSize: size, RawHash: hex.EncodeToString(hash.Sum(nil))}
	if !Verify(context.Background(), &verificationObject{reader: r}, v) {
		t.Fatal("valid 25 MiB original rejected")
	}
	if maxRead > 32*1024 || r.closes.Load() != 1 {
		t.Fatalf("read buffer=%d closes=%d, want at most32 KiB and one Close", maxRead, r.closes.Load())
	}
}
