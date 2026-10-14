package mailcontent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
)

type sourceReadFunc func([]byte) (int, error)

func (f sourceReadFunc) Read(p []byte) (int, error) { return f(p) }

func TestSourceReadRejectsCancelledContextBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(strings.NewReader(multipartFixture)), nil
	}))
	raw, err := p.readSource(ctx, "cancelled")
	if opened || raw != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("opened=%v returned=%v err=%v", opened, raw != nil, err)
	}
}

func TestSourceReadWireBoundaries(t *testing.T) {
	for _, kind := range []string{"eof", "data-eof", "data-error", "negative-count", "oversized-count", "cancelled-after-read", "progress-resets-empty-count"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("synthetic read failure")
			calls := 0
			r := &resourceReader{reader: sourceReadFunc(func(buf []byte) (int, error) {
				calls++
				switch kind {
				case "eof":
					return 0, io.EOF
				case "data-eof":
					return copy(buf, "abc"), io.EOF
				case "data-error":
					return copy(buf, "abc"), failure
				case "negative-count":
					return -1, nil
				case "oversized-count":
					return len(buf) + 1, nil
				case "cancelled-after-read":
					cancel()
					return copy(buf, "abc"), nil
				default:
					if calls == 99 || calls == 198 {
						return copy(buf, "x"), nil
					}
					if calls > 198 {
						return 0, io.EOF
					}
					return 0, nil
				}
			})}
			p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, nil }))
			raw, err := p.readSource(ctx, "wire")
			switch kind {
			case "eof":
				if err != nil || len(raw) != 0 {
					t.Errorf("empty EOF: %v", err)
				}
			case "data-eof":
				if err != nil || string(raw) != "abc" {
					t.Errorf("last bytes plus EOF lost: %v", err)
				}
			case "progress-resets-empty-count":
				if err != nil || string(raw) != "xx" {
					t.Errorf("legitimate progress rejected: %v", err)
				}
			case "data-error":
				if raw != nil || !errors.Is(err, failure) {
					t.Errorf("partial failure returned content: %v", err)
				}
			case "cancelled-after-read":
				if raw != nil || !errors.Is(err, context.Canceled) {
					t.Errorf("cancelled content returned: %v", err)
				}
			default:
				if raw != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("invalid count accepted: %v", err)
				}
			}
			if r.closes.Load() != 1 {
				t.Errorf("closed %d times", r.closes.Load())
			}
		})
	}
}

func TestParserResourceFailuresReleaseAdmission(t *testing.T) {
	for _, kind := range []string{"nil-reader", "open-error", "read-error", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls, closed := 0, 0
				p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) {
					calls++
					if calls > 8 {
						return io.NopCloser(strings.NewReader(multipartFixture)), nil
					}
					if kind == "nil-reader" {
						return nil, nil
					}
					r := &resourceReader{reader: strings.NewReader(badMIMEFixture), afterClose: func() { closed++ }}
					if kind == "open-error" {
						return r, errors.New("synthetic open")
					}
					if kind == "read-error" {
						r.reader = sourceReadFunc(func([]byte) (int, error) { return 0, errors.New("synthetic read") })
					}
					return r, nil
				}))
				for i := 0; i < 8; i++ {
					if value, err := p.Envelope(context.Background(), "same-key"); value != nil || err == nil {
						t.Error("failed object accepted")
					}
					if len(p.parses) != 0 {
						t.Error("failed flight retained capacity")
					}
				}
				if value, err := p.Envelope(context.Background(), "same-key"); value == nil || err != nil {
					t.Errorf("retry failed: %v", err)
				}
				if calls != 9 || (kind != "nil-reader" && closed != 8) {
					t.Errorf("opens=%d closed=%d", calls, closed)
				}
			})
		})
	}
}

func TestParserResourceLimitClosesAndDoesNotCache(t *testing.T) {
	var read int64
	r := &resourceReader{reader: sourceReadFunc(func(buf []byte) (int, error) {
		for i := range buf {
			buf[i] = 'x'
		}
		read += int64(len(buf))
		return len(buf), nil
	})}
	p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, nil }))
	value, err := p.Envelope(context.Background(), "oversized")
	if value != nil || err == nil || read != MaxBytes+1 {
		t.Errorf("read=%d value=%v err=%v", read, value != nil, err)
	}
	if r.closes.Load() != 1 || len(p.parses) != 0 || p.lru.Len() != 0 {
		t.Error("oversized source leaked reader, slot or cached content")
	}
}
