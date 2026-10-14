package enmime

import (
	"context"
	"errors"
	"io"
	"strings"
)

// ParseLimits bounds actual parsing events, not a separately interpreted MIME
// tree. All limits must be positive. Root is node 1 at depth 1. MaxParts counts
// the original Envelope.Attachments + Envelope.Inlines projection; a matching
// octet-stream inline consumes two entries. Byte admission is the caller's job.
type ParseLimits struct {
	MaxDepth         int
	MaxNodes         int
	MaxParts         int
	MaxBoundaryBytes int
}

var ErrParseDepthLimit = errors.New("MIME depth limit exceeded")
var ErrParseNodeLimit = errors.New("MIME node limit exceeded")
var ErrParsePartLimit = errors.New("MIME part limit exceeded")
var ErrParseBoundaryLimit = errors.New("MIME recursive boundary limit exceeded")
var ErrInvalidParseLimits = errors.New("invalid MIME parse limits")

// ReadEnvelopeBounded uses the unchanged default decoding/projection pipeline,
// with per-call guards in its actual Part allocation and header/recursion path.
// Existing ReadEnvelope/ReadParts APIs leave these guards disabled. No parse
// state, clock, context or budget is shared between bounded invocations.
func ReadEnvelopeBounded(ctx context.Context, r io.Reader, limits ParseLimits) (*Envelope, error) {
	budget, err := newParseBudget(ctx, limits)
	if err != nil {
		return nil, err
	}
	root, err := defaultParser.readParts(r, budget)
	if refused := budget.check(); refused != nil {
		return nil, refused
	}
	if err != nil {
		return nil, err
	}
	// Do not copy or reinterpret envelope projection or text conversion.
	envelope, err := defaultParser.EnvelopeFromPart(root)
	if cancelled := budget.check(); cancelled != nil {
		return nil, cancelled
	}
	return envelope, err
}

type parseBudget struct {
	ctx    context.Context
	limits ParseLimits
	nodes  int
	parts  int
	err    error
}

func newParseBudget(ctx context.Context, limits ParseLimits) (*parseBudget, error) {
	if ctx == nil || limits.MaxDepth <= 0 || limits.MaxNodes <= 0 || limits.MaxParts <= 0 || limits.MaxBoundaryBytes <= 0 {
		return nil, ErrInvalidParseLimits
	}
	return &parseBudget{ctx: ctx, limits: limits}, nil
}

// A nil guard is the legacy path: no context access, clock or additional
// header interpretation. Refusals are sticky so malformed-part recovery cannot
// erase them and continue allocating children.
func (b *parseBudget) check() error {
	if b == nil {
		return nil
	}
	if b.err != nil {
		return b.err
	}
	if err := b.ctx.Err(); err != nil {
		b.err = err
	}
	return b.err
}
func (b *parseBudget) refuse(err error) error { b.err = err; return err }
func (b *parseBudget) admitNode(depth int) error {
	if err := b.check(); err != nil || b == nil {
		return err
	}
	if depth > b.limits.MaxDepth {
		return b.refuse(ErrParseDepthLimit)
	}
	if b.nodes >= b.limits.MaxNodes {
		return b.refuse(ErrParseNodeLimit)
	}
	b.nodes++ // immediately before the real root/child &Part allocation
	return nil
}
func (b *parseBudget) admitProjection(part *Part, multipart bool) error {
	if err := b.check(); err != nil || b == nil {
		return err
	}
	count := 0
	if multipart {
		if part.Disposition == cdAttachment || part.ContentType == ctAppOctetStream {
			count++
		}
		if part.Disposition == cdInline && !strings.HasPrefix(part.ContentType, ctMultipartPrefix) {
			count++
		}
	} else if detectBinaryBody(part) {
		// Single-part binary EnvelopeFromPart chooses attachment OR inline,
		// unlike multipart matching, which can put the same part in both.
		count = 1
	}
	if count > b.limits.MaxParts-b.parts {
		return b.refuse(ErrParsePartLimit)
	}
	b.parts += count
	return nil
}
func (b *parseBudget) admitBoundary(boundary string) error {
	if err := b.check(); err != nil || b == nil {
		return err
	}
	if len(boundary) > b.limits.MaxBoundaryBytes {
		return b.refuse(ErrParseBoundaryLimit)
	}
	return nil
}
