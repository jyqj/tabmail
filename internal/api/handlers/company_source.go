package handlers

import (
	"context"
	"io"
	"net/http"
)

// Prime one bounded chunk before committing successful download headers. The
// application source's first-read guard can still reject authorization here.
// After headers/bytes are written, abort the transport on failure rather than
// returning a truncated successful EML or appending a JSON error to mail bytes.
func (h *CompanyMailHandler) streamMailSource(w http.ResponseWriter, r *http.Request, source io.Reader) {
	reader := &mailSourceReader{ctx: r.Context(), source: source}
	buffer := make([]byte, 32*1024)
	n, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		h.result(w, nil, err)
		return
	}
	downloadHeaders(w, "message.eml", "message/rfc822")
	if n > 0 {
		written, writeErr := w.Write(buffer[:n])
		if writeErr != nil || written != n {
			panic(http.ErrAbortHandler)
		}
	}
	if err == io.EOF {
		return
	}
	if _, err = io.CopyBuffer(w, reader, buffer); err != nil {
		// Chi's Recoverer intentionally re-panics this sentinel so net/http aborts
		// HTTP/1 framing or resets the HTTP/2 stream. Do not log raw object errors.
		panic(http.ErrAbortHandler)
	}
}

// Keep deferred readers cancellable and bound repeated (0,nil) results during
// both preflight and streaming. No WriterTo promotion may bypass Read.
type mailSourceReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *mailSourceReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.source.Read(p)
		if cancelled := r.ctx.Err(); cancelled != nil {
			clear(p)
			return 0, cancelled
		}
		if n < 0 || n > len(p) {
			clear(p)
			return 0, io.ErrUnexpectedEOF
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
