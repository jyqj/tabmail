package mailcontent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"unicode"

	"github.com/jhillyerd/enmime/v2"
	"github.com/jhillyerd/enmime/v2/mediatype"
)

// Root is node 1 at depth 1. Containers, body parts, attachments and inlines
// all consume nodes, not just the attachment projection. These admission
// limits intentionally reject formerly accepted pathological MIME trees.
const MaxMIMEDepth = 32
const MaxMIMENodes = 1024

var ErrMIMEDepth = errors.New("MIME depth limit exceeded")
var ErrMIMENodes = errors.New("MIME node limit exceeded")
var ErrMIMEParts = errors.New("MIME part limit exceeded")
var ErrMIMEBytes = errors.New("message exceeds 25 MiB parser limit")
var ErrMIMEParse = errors.New("MIME parsing failed")

// ParseBoundedReader takes ownership of r, including on cancellation/error.
// Close must unblock Read for prompt cancellation. No unbounded worker is
// started, and Reader's optional WriterTo cannot bypass byte/context checks.
func ParseBoundedReader(ctx context.Context, r io.ReadCloser) (*enmime.Envelope, error) {
	if r == nil {
		return nil, errors.New("object reader unavailable")
	}
	var once sync.Once
	closeReader := func() { once.Do(func() { _ = r.Close() }) }
	stop := context.AfterFunc(ctx, closeReader)
	defer func() { stop(); closeReader() }()
	raw, err := io.ReadAll(io.LimitReader(&sourceProgressReader{ctx: ctx, reader: r}, MaxBytes+1))
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, err
	}
	return parseBoundedContext(ctx, raw)
}

func parseBoundedContext(ctx context.Context, raw []byte) (*enmime.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxBytes {
		return nil, ErrMIMEBytes
	}
	b := mimeBudget{ctx: ctx}
	if err := b.walk(raw, 1); err != nil {
		return nil, err
	}
	// Only after admission may enmime allocate decoded bodies/the MIME tree.
	// The same default header/media-type interpretation is used in both passes.
	env, err := enmime.ReadEnvelope(&sourceProgressReader{ctx: ctx, reader: bytes.NewReader(raw)})
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, ErrMIMEParse
	}
	if len(Parts(env)) > maxParts {
		return nil, ErrMIMEParts
	}
	return env, nil
}

type mimeBudget struct {
	ctx          context.Context
	nodes, parts int
	multipart    bool
}

// Header warnings are replayed by the real parser, not retained twice here.
type budgetHeaderErrors struct{}

func (budgetHeaderErrors) AddError(string, string, ...any)   {}
func (budgetHeaderErrors) AddWarning(string, string, ...any) {}

func (b *mimeBudget) walk(raw []byte, depth int) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if depth > MaxMIMEDepth {
		return ErrMIMEDepth
	}
	b.nodes++
	if b.nodes > MaxMIMENodes {
		return ErrMIMENodes
	}
	reader := bytes.NewReader(raw)
	br := bufio.NewReader(&sourceProgressReader{ctx: b.ctx, reader: reader})
	header, err := enmime.ReadHeader(br, budgetHeaderErrors{})
	if cancelled := b.ctx.Err(); cancelled != nil {
		return cancelled
	}
	if err != nil {
		return ErrMIMEParse
	}
	ctype := header.Get("Content-Type")
	// setupHeaders returns immediately for a child with absent OR empty CT.
	// In particular it does not populate Disposition from an otherwise valid
	// Content-Disposition header. Count the node, but not projected parts.
	if ctype == "" && depth != 1 {
		return nil
	}
	if ctype == "" && depth == 1 {
		ctype = `text/plain; charset="us-ascii"`
	}
	mtype, params, _, err := mediatype.Parse(ctype)
	if ctype != "" && err != nil {
		return ErrMIMEParse
	}
	boundary := params["boundary"]
	if depth == 1 {
		b.multipart = strings.HasPrefix(mtype, "multipart/")
	}
	if b.multipart {
		disposition, _, _, _ := mediatype.Parse(header.Get("Content-Disposition"))
		if disposition == "attachment" || mtype == "application/octet-stream" {
			b.parts++
		}
		if disposition == "inline" && !strings.HasPrefix(mtype, "multipart/") {
			b.parts++
		}
		if b.parts > maxParts {
			return ErrMIMEParts
		}
	}
	// enmime v2.3.0 does not recurse into message/rfc822 or transfer-decode
	// containers before splitting them. Child non-text data with a boundary
	// DOES recurse; child text/plain and text/html with a boundary do not.
	recurse := b.multipart && depth == 1
	if depth != 1 {
		recurse = boundary != "" && mtype != "text/plain" && mtype != "text/html"
	}
	if !recurse {
		return nil
	}
	// Default enmime readers use a 4096-byte Peek buffer. CR candidates
	// request len("--"+boundary)+4 bytes. Larger recursive boundaries cannot
	// be modeled by the normal look-ahead path, so reject them explicitly.
	// RFC-valid boundaries (at most 70 characters) are unaffected.
	if len(boundary) > 4090 {
		return ErrMIMEParse
	}
	body := raw[len(raw)-reader.Len()-br.Buffered():]
	prefix := []byte("--" + boundary)
	final := appendBoundaryFinal(prefix)
	cursor, children := 0, 0
	for cursor < len(body) {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		line, next, newline := budgetLine(body, cursor)
		cursor = next
		if len(line) > 0 && (line[0] == '\r' || line[0] == '\n') {
			continue
		}
		if bytes.Contains(line, final) {
			return nil
		}
		if newline && budgetDelimiter(line, prefix) {
			end, err := budgetPartEnd(b.ctx, body[cursor:], prefix)
			if err != nil {
				return err
			}
			if err := b.walk(body[cursor:cursor+end], depth+1); err != nil {
				return err
			}
			cursor += end
			children++
			continue
		}
		if !newline {
			return nil
		} // unclosed multipart: retain enmime's tolerance
		if children != 0 {
			return ErrMIMEParse
		}
	}
	return nil
}

func appendBoundaryFinal(prefix []byte) []byte {
	return append(append([]byte(nil), prefix...), '-', '-')
}
func budgetDelimiter(buf, prefix []byte) bool {
	i := bytes.Index(buf, prefix)
	return i >= 0 && i+len(prefix) < len(buf) && unicode.IsSpace(rune(buf[i+len(prefix)]))
}
func budgetLine(body []byte, cursor int) ([]byte, int, bool) {
	i := bytes.IndexByte(body[cursor:], '\n')
	if i < 0 {
		return body[cursor:], len(body), false
	}
	return body[cursor : cursor+i+1], cursor + i + 1, true
}

// Match enmime v2.3.0 boundaryReader.Read's small look-ahead, including its
// tolerant delimiter/terminator matching and omission of the separator CRLF.
// This works on raw slices: no bodies, decoded content or complete tree are
// allocated. Traversal admits at most 32 depths and 1024 headers; a deeper
// candidate is rejected immediately before parsing its header.
// Initial-fill candidates can repeatedly search overlapping windows of length
// len(prefix)+padding+2; newline candidates later use the same look-ahead.
// Admission bounds are not a measured CPU or heap bound.
func budgetPartEnd(ctx context.Context, raw, prefix []byte) (int, error) {
	final := appendBoundaryFinal(prefix)
	for pos := 0; pos < len(raw); {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		padding := 0
		if raw[pos] == '\r' {
			padding = 2
		} else if raw[pos] == '\n' {
			padding = 1
		}
		peekLen := len(prefix) + padding + 2
		if len(raw)-pos >= peekLen {
			peek := raw[pos : pos+peekLen]
			repeated := bytes.HasPrefix(peek, []byte("\n\n")) || bytes.HasPrefix(peek, []byte("\n\r")) || bytes.HasPrefix(peek, []byte("\r\n\r")) || bytes.HasPrefix(peek, []byte("\r\n\n"))
			if !repeated && (budgetDelimiter(peek[padding:], prefix) || bytes.Contains(peek[padding:], final)) {
				return pos, nil
			}
		}
		// enmime wraps each new part in a default 4096-byte bufio.Reader.
		// boundaryReader keeps atPartStart true for that entire first fill,
		// not just byte zero, so even a mid-line marker can end that fill.
		// Reproduce this quirk rather than undercounting its allocated nodes.
		if pos < 4095 {
			pos++
			continue
		}
		// Subsequent fills only inspect CR/LF candidates.
		i := bytes.IndexAny(raw[pos+1:], "\r\n")
		if i < 0 {
			return len(raw), nil
		}
		pos += i + 1
	}
	return len(raw), nil
}
