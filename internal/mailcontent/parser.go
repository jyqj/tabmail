// Package mailcontent owns bounded MIME parsing. No HTTP or SQL authorization
// lives here: callers must authorize BEFORE asking for a cached envelope.
package mailcontent

import (
	"bytes"
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
}

func New(objects ObjectReader) *Parser {
	return &Parser{objects: objects, lru: list.New(), entries: map[string]*list.Element{}}
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
func (p *Parser) load(ctx context.Context, key string) (*parsed, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" || p.objects == nil {
		return nil, errors.New("raw source unavailable")
	}
	p.mu.Lock()
	if el := p.entries[key]; el != nil {
		v := el.Value.(*parsed)
		if time.Now().Before(v.until) {
			p.lru.MoveToFront(el)
			p.mu.Unlock()
			return v, nil
		}
		p.bytes -= v.size
		p.lru.Remove(el)
		delete(p.entries, key)
	}
	p.mu.Unlock()
	ch := p.flight.DoChan(key, func() (any, error) {
		// Do not tie a shared parse to the first viewer's cancelled request. The
		// parse is bounded by its own timeout; each waiter may independently leave.
		parseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		r, e := p.objects.Get(parseCtx, key)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, errors.New("object reader unavailable")
		}
		defer r.Close()
		raw, e := io.ReadAll(io.LimitReader(r, MaxBytes+1))
		if e != nil {
			return nil, e
		}
		if int64(len(raw)) > MaxBytes {
			return nil, errors.New("message exceeds 25 MiB parser limit")
		}
		env, e := enmime.ReadEnvelope(bytes.NewReader(raw))
		if e != nil {
			return nil, errors.New("MIME parsing failed")
		}
		parts := Parts(env)
		if len(parts) > maxParts {
			return nil, errors.New("MIME part limit exceeded")
		}
		size := int64(len(raw) + len(env.Text) + len(env.HTML))
		for _, f := range parts {
			size += int64(len(f.Content))
		}
		v := &parsed{env: env, hash: Hash(raw), size: size, key: key, until: time.Now().Add(2 * time.Minute)}
		p.mu.Lock()
		defer p.mu.Unlock()
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
	return nil, nil, errors.New("attachment not found in this source")
}
