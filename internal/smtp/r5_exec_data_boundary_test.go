package smtp

import (
	"bufio"
	"bytes"
	"fmt"
	"net/textproto"
	"strings"
	"testing"
)

func execDATAOriginal(t *testing.T, size int, dots bool) []byte {
	t.Helper()
	prefix := "Subject: DATA size boundary\r\n\r\n"
	if dots {
		prefix += ".\r\n..leading dots\r\n"
	}
	if size < len(prefix)+2 {
		t.Fatal("fixture size cannot contain the message")
	}
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-2) + "\r\n")
}

func execDATAWire(t *testing.T, raw []byte) string {
	t.Helper()
	var wire bytes.Buffer
	dot := textproto.NewWriter(bufio.NewWriter(&wire)).DotWriter()
	if _, err := dot.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := dot.Close(); err != nil {
		t.Fatal(err)
	}
	return wire.String()
}

// SIZE is an optional declaration. The actual decoded original must obey the
// same inclusive limit with or without it, regardless of dot-stuffing overhead.
func TestExecDATAInclusiveLimitPreservesOriginalAndCommands(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, limit := range []int{128, 1024} {
			for _, dots := range []bool{false, true} {
				for _, declared := range []bool{false, true} {
					for _, delta := range []int{-1, 0, 1} {
						name := fmt.Sprintf("durable=%t/limit=%d/dots=%t/declared=%t/delta=%+d", durable, limit, dots, declared, delta)
						t.Run(name, func(t *testing.T) {
							f := newExecFramingFixture(t, durable, int64(limit))
							if declared {
								f.expect(t, "RSET\r\n", 250)
								f.expect(t, fmt.Sprintf("MAIL FROM:<sender@example.test> SIZE=%d\r\n", limit), 250)
								f.expect(t, "RCPT TO:<reader@framing.test>\r\n", 250)
							}
							raw := execDATAOriginal(t, limit+delta, dots)
							f.expect(t, "DATA\r\n", 354)
							// The next command shares the socket write with the terminator.
							code, response := f.reply(t, execDATAWire(t, raw)+"NOOP\r\n")
							wantCode := 250
							if delta > 0 {
								wantCode = 552
							}
							if code != wantCode {
								t.Errorf("DATA accepted %d-byte original against %d-byte limit with %q, want %d", len(raw), limit, response, wantCode)
							}
							f.expect(t, "", 250)
							f.begin(t)
							next := []byte("Subject: next DATA transaction\r\n\r\nsecond original\r\n")
							f.expect(t, "DATA\r\n", 354)
							f.expect(t, execDATAWire(t, next)+"NOOP\r\n", 250)
							f.expect(t, "", 250)
							f.finish(t)
							if delta > 0 {
								f.originals(t, next)
							} else {
								f.originals(t, raw, next)
							}
						})
					}
				}
			}
		}
	}
}

func TestExecDATATruncationAtLimitIsNotAnOversizedOrCompleteMessage(t *testing.T) {
	for _, durable := range []bool{false, true} {
		for _, tail := range []string{"", ".", ".\r"} {
			name := fmt.Sprintf("durable=%t/tail=%q", durable, tail)
			t.Run(name, func(t *testing.T) {
				const limit = 128
				f := newExecFramingFixture(t, durable, limit)
				f.expect(t, "DATA\r\n", 354)
				// The original is exactly N bytes, but the terminator is incomplete.
				if _, err := fmt.Fprintf(f.conn, "%s%s", execDATAOriginal(t, limit, false), tail); err != nil {
					t.Fatal(err)
				}
				if err := f.conn.CloseWrite(); err != nil {
					t.Fatal(err)
				}
				code, response := f.reply(t, "")
				if code != 554 {
					t.Errorf("incomplete DATA at inclusive limit returned %q, want transaction failure 554", response)
				}
				f.finish(t)
				f.originals(t)
			})
		}
	}
}
