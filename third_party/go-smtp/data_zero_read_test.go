package smtp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type execZeroReadInput struct {
	data   *strings.Reader
	reads  int
	forbid bool
}

func (r *execZeroReadInput) Read(p []byte) (int, error) {
	r.reads++
	if r.forbid {
		return 0, errors.New("zero-length DATA read touched the input")
	}
	return r.data.Read(p)
}

func TestExecDATAZeroLengthReadsPreserveStateAndNextCommand(t *testing.T) {
	const raw = "body\r\n"
	for _, state := range []string{"remaining", "at_limit", "eof", "oversized"} {
		for _, bufferKind := range []string{"nil", "empty_with_capacity"} {
			t.Run(state+"/"+bufferKind, func(t *testing.T) {
				source := &execZeroReadInput{data: strings.NewReader(raw + ".\r\nNOOP\r\n")}
				input := bufio.NewReader(source)
				reader := &dataReader{r: input, limited: true, n: int64(len(raw) + 1)}
				var wantErr error
				switch state {
				case "at_limit":
					reader.n = int64(len(raw))
					got := make([]byte, len(raw))
					if _, err := io.ReadFull(reader, got); err != nil || string(got) != raw {
						t.Fatalf("read exact original: %q, %v", got, err)
					}
				case "eof":
					if got, err := io.ReadAll(reader); err != nil || string(got) != raw {
						t.Fatalf("read complete original: %q, %v", got, err)
					}
					wantErr = io.EOF
				case "oversized":
					reader.n = int64(len(raw) - 1)
					if got, err := io.ReadAll(reader); !errors.Is(err, ErrDataTooLarge) || !bytes.Equal(got, []byte(raw[:len(raw)-1])) {
						t.Fatalf("read oversized original: %q, %v", got, err)
					}
					wantErr = ErrDataTooLarge
				}
				var buffer []byte
				if bufferKind == "empty_with_capacity" {
					buffer = make([]byte, 0, 8)
				}
				beforeReads := source.reads
				beforeRemaining := source.data.Len() + input.Buffered()
				// Trap external reads, rather than relying on a wall-clock wait:
				// a zero-byte request must not need input that could block.
				source.forbid = true
				n, err := reader.Read(buffer)
				source.forbid = false
				if n != 0 || !errors.Is(err, wantErr) {
					t.Errorf("zero-length read = %d, %v; want 0, %v", n, err, wantErr)
				}
				if source.reads != beforeReads || source.data.Len()+input.Buffered() != beforeRemaining {
					t.Error("zero-length read performed I/O or consumed buffered bytes")
				}
				reader.limited = false
				if _, err := io.Copy(io.Discard, reader); err != nil {
					t.Errorf("drain after zero-length read: %v", err)
				}
				if command, err := input.ReadString('\n'); err != nil || command != "NOOP\r\n" {
					t.Errorf("next command after zero-length read = %q, %v", command, err)
				}
			})
		}
	}
}
