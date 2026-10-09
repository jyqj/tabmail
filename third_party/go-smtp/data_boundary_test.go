package smtp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestExecDATAReaderBoundaryAndOverflow(t *testing.T) {
	raw := []byte("Subject: reader boundary\r\n\r\n.\r\nlast line\r\n")
	const wire = "Subject: reader boundary\r\n\r\n..\r\nlast line\r\n.\r\nNOOP\r\n"
	for _, bufferSize := range []int{1, 7, 512} {
		for _, delta := range []int{-1, 0, 1} {
			t.Run(fmt.Sprintf("buffer=%d/limit_delta=%+d", bufferSize, delta), func(t *testing.T) {
				limit := len(raw) + delta
				input := bufio.NewReader(strings.NewReader(wire))
				reader := &dataReader{r: input, limited: true, n: int64(limit)}
				buffer := make([]byte, bufferSize)
				var got []byte
				var terminal error
				for terminal == nil {
					n, err := reader.Read(buffer)
					if n == 0 && err == nil {
						t.Fatal("message reader made no progress")
					}
					got = append(got, buffer[:n]...)
					if len(got) > limit {
						t.Fatalf("reader exposed %d original bytes, limit %d", len(got), limit)
					}
					terminal = err
				}
				want := raw
				if limit < len(raw) {
					want = raw[:limit]
					if !errors.Is(terminal, ErrDataTooLarge) {
						t.Errorf("oversized message error = %v, want ErrDataTooLarge", terminal)
					}
					// Re-reading an oversized transaction must not turn it into a
					// successful EOF or reveal further body bytes to the backend.
					for i := 0; i < 2; i++ {
						if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, ErrDataTooLarge) {
							t.Errorf("read after limit failure = %d, %v", n, err)
						}
					}
					// The server drains a failed DATA transaction without the limit.
					reader.limited = false
					if _, err := io.Copy(io.Discard, reader); err != nil {
						t.Errorf("drain failed transaction: %v", err)
					}
				} else if terminal != io.EOF {
					t.Errorf("complete %d-byte message with %d-byte limit ended in %v, want EOF", len(raw), limit, terminal)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("original bytes = %q, want %q", got, want)
				}
				if command, err := input.ReadString('\n'); err != nil || command != "NOOP\r\n" {
					t.Errorf("next command = %q, %v", command, err)
				}
			})
		}
	}
}
