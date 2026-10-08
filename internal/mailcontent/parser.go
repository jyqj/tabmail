// Package mailcontent owns bounded MIME parsing. No HTTP or SQL authorization
// lives here: callers must authorize BEFORE asking for a cached envelope.
package mailcontent

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"golang.org/x/sync/singleflight"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"tabmail/internal/company"
	"tabmail/internal/sanitize"
	"time"
)

const Version = 1
const MaxBytes int64 = 25 * 1024 * 1024
const cacheBudget int64 = 64 * 1024 * 1024
const maxParts = 512

// ErrAttachmentNotFound means the source parsed successfully, but the requested
// immutable part ID is absent. Source I/O and MIME errors retain their causes.
var ErrAttachmentNotFound = errors.New("attachment not found in this source")

// Bound expensive distinct-key work per Parser, independently of the LRU's
// retained-byte budget. Same-key waiters share one slot through singleflight.
const maxConcurrentParses = 4
const parseTimeout = 30 * time.Second

type ObjectReader interface {
	Get(context.Context, string) (io.ReadCloser, error)
}
type parsed struct {
	env   *enmime.Envelope
	hash  string
	size  int64
	key   string
	until time.Time
}
type Parser struct {
	objects ObjectReader
	mu      sync.Mutex
	lru     *list.List
	entries map[string]*list.Element
	bytes   int64
	flight  singleflight.Group
	parses  chan struct{}
}

func New(objects ObjectReader) *Parser {
	return &Parser{objects: objects, lru: list.New(), entries: map[string]*list.Element{}, parses: make(chan struct{}, maxConcurrentParses)}
}
func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func SafeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." || len(name) > 180 {
		return "attachment"
	}
	return name
}
func Parts(env *enmime.Envelope) []*enmime.Part {
	return append(append([]*enmime.Part{}, env.Attachments...), env.Inlines...)
}

// ParseBounded checks bytes before parsing and structural limits at the real
// allocation/header events. Ingest calls this entry directly; Parser
// uses the same admission with its existing shared-work context.
func ParseBounded(raw []byte) (*enmime.Envelope, error) {
	return parseBoundedContext(context.Background(), raw)
}
func (p *Parser) load(ctx context.Context, key string) (*parsed, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" || p.objects == nil {
		return nil, errors.New("raw source unavailable")
	}
	if v := p.cached(key); v != nil {
		return v, nil
	}
	ch := p.flight.DoChan(key, func() (any, error) {
		// A previous flight may have populated the cache between the caller's
		// lookup and joining singleflight. Do not reopen that immutable source.
		if v := p.cached(key); v != nil {
			return v, nil
		}
		// Keep shared work independent of one waiter's cancellation, but include
		// capacity waiting in its own deadline. No object opens before admission.
		parseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), parseTimeout)
		defer cancel()
		select {
		case p.parses <- struct{}{}:
			defer func() { <-p.parses }()
		case <-parseCtx.Done():
			return nil, parseCtx.Err()
		}
		if e := parseCtx.Err(); e != nil {
			return nil, e
		}
		raw, e := p.readSource(parseCtx, key)
		if e != nil {
			return nil, e
		}
		if int64(len(raw)) > MaxBytes {
			return nil, ErrMIMEBytes
		}
		sourceHash := Hash(raw)
		if e = ValidateSourceHash(key, sourceHash); e != nil {
			return nil, e
		}
		env, e := parseBoundedContext(parseCtx, raw)
		if e != nil {
			return nil, e
		}
		if e = parseCtx.Err(); e != nil {
			return nil, e
		}
		parts := Parts(env)
		size := int64(len(raw) + len(env.Text) + len(env.HTML))
		for _, f := range parts {
			size += int64(len(f.Content))
		}
		v := &parsed{env: env, hash: sourceHash, size: size, key: key, until: time.Now().Add(2 * time.Minute)}
		p.mu.Lock()
		defer p.mu.Unlock()
		if e = parseCtx.Err(); e != nil {
			return nil, e
		}
		if size <= cacheBudget {
			if old := p.entries[key]; old != nil {
				p.bytes -= old.Value.(*parsed).size
				p.lru.Remove(old)
				delete(p.entries, key)
			}
			for p.bytes+size > cacheBudget && p.lru.Len() > 0 {
				last := p.lru.Back()
				old := last.Value.(*parsed)
				p.bytes -= old.size
				delete(p.entries, old.key)
				p.lru.Remove(last)
			}
			p.entries[key] = p.lru.PushFront(v)
			p.bytes += size
		}
		return v, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*parsed), nil
	}
}
func (p *Parser) Envelope(ctx context.Context, key string) (*enmime.Envelope, error) {
	v, e := p.load(ctx, key)
	if e != nil {
		return nil, e
	}
	return v.env, nil
}
func attachmentID(message uuid.UUID, sourceHash string, index int, hash string) string {
	return company.Hash(fmt.Sprintf("mime-v1:%s:%s:%d:%s", message, sourceHash, index, hash))
}
func (p *Parser) Document(ctx context.Context, id uuid.UUID, key string) (*company.ParsedMessage, error) {
	v, e := p.load(ctx, key)
	if e != nil {
		return nil, e
	}
	d := &company.ParsedMessage{MessageID: id, SourceKey: key, SourceSHA256: v.hash, ParserVersion: Version, TextBody: v.env.Text, Parts: []company.ParsedAttachment{}}
	if v.env.HTML != "" {
		d.HTMLBody, e = sanitize.HTML(v.env.HTML)
		if e != nil {
			d.HTMLBody = ""
			d.BodyAccess = "sanitize_failed"
		}
	}
	for i, f := range Parts(v.env) {
		h := Hash(f.Content)
		d.Parts = append(d.Parts, company.ParsedAttachment{ID: attachmentID(id, v.hash, i, h), Index: i, Filename: SafeFilename(f.FileName), Size: len(f.Content), ContentType: f.ContentType, SHA256: h})
	}
	// An RFC reference root is a hint, not a globally trusted identity. Queries
	// always scope threads to one tenant and mailbox.
	root := ""
	for _, header := range []string{"References", "In-Reply-To", "Message-Id"} {
		for _, r := range strings.Fields(v.env.GetHeader(header)) {
			if len(r) <= 254 && strings.HasPrefix(r, "<") && strings.HasSuffix(r, ">") {
				root = r
				break
			}
		}
		if root != "" {
			break
		}
	}
	if root == "" {
		root = id.String()
	}
	d.ThreadKey = company.Hash(root)
	return d, nil
}
func (p *Parser) Attachment(ctx context.Context, message uuid.UUID, key, id string) (*company.ParsedAttachment, []byte, error) {
	v, e := p.load(ctx, key)
	if e != nil {
		return nil, nil, e
	}
	for i, f := range Parts(v.env) {
		h := Hash(f.Content)
		if attachmentID(message, v.hash, i, h) == id {
			return &company.ParsedAttachment{ID: id, Index: i, Filename: SafeFilename(f.FileName), Size: len(f.Content), ContentType: f.ContentType, SHA256: h}, append([]byte(nil), f.Content...), nil
		}
	}
	return nil, nil, ErrAttachmentNotFound
}
