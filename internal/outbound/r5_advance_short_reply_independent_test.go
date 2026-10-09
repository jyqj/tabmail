package outbound

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"testing"
)

type advanceIndependentReadEnd struct {
	value string
	cause error
}

func (r *advanceIndependentReadEnd) Read(p []byte) (int, error) {
	n := copy(p, r.value)
	r.value = r.value[n:]
	if r.value == "" {
		return n, r.cause
	}
	return n, nil
}

// Observe successive parsed replies, not the final raw receive suffix. The
// first complete response must remain authoritative even when its read-ahead
// buffers a second unterminated response and a terminal error in the same read.
func TestAdvanceSMTPIndependentConsumedBoundary(t *testing.T) {
	for _, code := range []int{250, 451, 550} {
		for _, terminal := range []error{io.EOF, io.ErrUnexpectedEOF, io.ErrNoProgress} {
			for _, ending := range []string{"", "\r", "\n"} {
				t.Run(fmt.Sprintf("%d/%s/%q", code, terminal, ending), func(t *testing.T) {
					payload := "250 first completed\r\n" + fmt.Sprintf("%d second%s", code, ending)
					reader, classify := guardSMTPReplyReader(bufio.NewReader(&advanceIndependentReadEnd{value: payload, cause: terminal}))
					parser := textproto.NewReader(reader)
					_, _, err := parser.ReadResponse(250)
					if err := classify(err); err != nil {
						t.Fatalf("complete first response lost to read-ahead: %v", err)
					}
					_, _, err = parser.ReadResponse(250)
					err = classify(err)
					var protocol *textproto.Error
					if ending != "\n" {
						if !errors.Is(err, terminal) || errors.As(err, &protocol) {
							t.Fatalf("second fragment promoted to status or lost cause: %v", err)
						}
					} else if code == 250 {
						if err != nil {
							t.Fatalf("complete LF acceptance changed: %v", err)
						}
					} else if !errors.As(err, &protocol) || protocol.Code != code || errors.Is(err, terminal) {
						t.Fatalf("complete LF rejection changed: %v", err)
					}
				})
			}
		}
	}
}

func TestAdvanceSMTPIndependentRingBoundary(t *testing.T) {
	for _, length := range []int{4095, 4096, 4097, 8193} {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", length, complete), func(t *testing.T) {
				payload := "550 " + strings.Repeat("x", length)
				if complete {
					payload += "\r\n"
				}
				reader, classify := guardSMTPReplyReader(bufio.NewReader(&advanceIndependentReadEnd{value: payload, cause: io.ErrUnexpectedEOF}))
				_, _, err := textproto.NewReader(reader).ReadResponse(250)
				err = classify(err)
				var protocol *textproto.Error
				if complete {
					if !errors.As(err, &protocol) || protocol.Code != 550 {
						t.Fatalf("complete ring response lost: %v", err)
					}
				} else if !errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &protocol) {
					t.Fatalf("ring fragment promoted to rejection: %v", err)
				}
			})
		}
	}
}
