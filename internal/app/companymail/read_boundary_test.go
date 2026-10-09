package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The callback is a deterministic boundary: the repository changes before the
// object operation completes. No sleep, external database or production hook.
type boundaryRepository struct {
	Repository
	mu      sync.Mutex
	message models.Message
	denied  error
	reads   int
}

func (r *boundaryRepository) GetWorkMessage(ctx context.Context, _ authz.Actor, _, _ uuid.UUID) (*models.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.denied != nil {
		return nil, r.denied
	}
	value := r.message
	return &value, nil
}
func (r *boundaryRepository) change(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch kind {
	case "revoked":
		r.denied = app.Forbidden("mailbox read permission required")
	case "removed":
		r.denied = app.NotFound("message not found")
	case "database-error":
		r.denied = app.Internal(errors.New("synthetic repository failure"))
	case "source-replaced":
		r.message.RawObjectKey = "replacement-key"
	}
}

type boundaryObjects struct {
	raw    []byte
	onOpen func()
	onRead func()
	opens  atomic.Int64
	closes atomic.Int64
}

func (o *boundaryObjects) Get(context.Context, string) (io.ReadCloser, error) {
	o.opens.Add(1)
	if o.onOpen != nil {
		o.onOpen()
	}
	return &boundaryReader{Reader: bytes.NewReader(o.raw), owner: o}, nil
}
func (o *boundaryObjects) Put(context.Context, string, io.Reader, int64) error {
	panic("unexpected write")
}

type boundaryReader struct {
	*bytes.Reader
	owner     *boundaryObjects
	once      sync.Once
	closeOnce sync.Once
}

func (r *boundaryReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		if r.owner.onRead != nil {
			r.owner.onRead()
		}
	})
	return r.Reader.Read(p)
}
func (r *boundaryReader) Close() error {
	r.closeOnce.Do(func() { r.owner.closes.Add(1) })
	return nil
}
func boundaryFixture() (*Service, *boundaryRepository, *boundaryObjects, authz.Actor, uuid.UUID, uuid.UUID) {
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser}
	mailbox, message := uuid.New(), uuid.New()
	r := &boundaryRepository{message: models.Message{ID: message, TenantID: a.TenantID, MailboxID: mailbox, RawObjectKey: "original-key"}}
	o := &boundaryObjects{raw: []byte("From: sender@boundary.test\r\nSubject: synthetic boundary\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nprivate fixture body\r\n--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=fixture.txt\r\n\r\nfixture attachment\r\n--b--\r\n")}
	return NewService(r, o), r, o, a, mailbox, message
}

func TestCompanyReadRechecksAfterObjectOpen(t *testing.T) {
	for _, endpoint := range []string{"message", "source", "parts", "ordinal", "stable-id"} {
		for _, change := range []string{"revoked", "removed", "database-error", "source-replaced"} {
			t.Run(endpoint+"/"+change, func(t *testing.T) {
				s, repo, objects, actor, mailbox, message := boundaryFixture()
				partID := ""
				if endpoint == "stable-id" {
					doc, err := s.parser.Document(context.Background(), message, "original-key")
					if err != nil || len(doc.Parts) != 1 {
						t.Fatalf("invalid MIME fixture: %v", err)
					}
					partID = doc.Parts[0].ID
					// The request below must do a real object open, not reuse prewarming.
					s = NewService(repo, objects)
				}
				objects.onOpen = func() { repo.change(change) }
				var err error
				var exposed bool
				switch endpoint {
				case "message":
					v, e := s.Message(context.Background(), actor, mailbox, message)
					exposed, err = v != nil, e
				case "source":
					v, e := s.Source(context.Background(), actor, mailbox, message)
					exposed, err = v != nil, e
					if v != nil {
						_ = v.Close()
					}
				case "parts":
					v, e := s.InboundAttachments(context.Background(), actor, mailbox, message)
					exposed, err = v != nil, e
				case "ordinal":
					v, e := s.InboundAttachment(context.Background(), actor, mailbox, message, 0)
					exposed, err = v != nil, e
				case "stable-id":
					v, e := s.InboundAttachmentByID(context.Background(), actor, mailbox, message, partID)
					exposed, err = v != nil, e
				}
				if err == nil || exposed {
					t.Fatal("content exposed after authority/source changed during object open")
				}
				if objects.opens.Load() != objects.closes.Load() {
					t.Fatal("rejected object reader was not closed")
				}
			})
		}
	}
}

func TestCompanySourceRechecksBeforeFirstBytes(t *testing.T) {
	for _, change := range []string{"revoked", "removed", "database-error", "source-replaced"} {
		t.Run(change, func(t *testing.T) {
			s, repo, objects, actor, mailbox, message := boundaryFixture()
			objects.onRead = func() { repo.change(change) }
			reader, err := s.Source(context.Background(), actor, mailbox, message)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if n, e := reader.Read(nil); n != 0 || e != nil {
				t.Fatalf("zero-length read: %d %v", n, e)
			}
			buf := make([]byte, 128)
			n, err := reader.Read(buf)
			if err == nil || n != 0 {
				t.Fatal("source released first bytes after an in-flight read lost authority")
			}
			if !bytes.Equal(buf, make([]byte, len(buf))) {
				t.Fatal("rejected source bytes remained in caller buffer")
			}
			if objects.closes.Load() != 1 {
				t.Fatal("rejected source reader must close immediately")
			}
			if n, err = reader.Read(buf); n != 0 || err == nil {
				t.Fatal("failed stream resumed")
			}
		})
	}
}

func TestCompanySourceSuccessfulStreamDoesNotReauthorizeEachChunk(t *testing.T) {
	s, repo, objects, actor, mailbox, message := boundaryFixture()
	reader, err := s.Source(context.Background(), actor, mailbox, message)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// A zero-length read must not consume the first-byte authorization check.
	if n, err := reader.Read(nil); n != 0 || err != nil {
		t.Fatalf("zero-length read: %d %v", n, err)
	}
	buf := make([]byte, 17)
	var got []byte
	for {
		n, e := reader.Read(buf)
		got = append(got, buf[:n]...)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(got, objects.raw) {
		t.Fatal("authorized source bytes changed")
	}
	repo.mu.Lock()
	reads := repo.reads
	repo.mu.Unlock()
	if reads > 3 {
		t.Fatalf("stream performed %d repository reads", reads)
	}
}

type boundaryCache struct {
	*boundaryRepository
	doc    *company.ParsedMessage
	onGet  func()
	onSave func()
}

func (r *boundaryCache) GetParsedMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*company.ParsedMessage, error) {
	if r.onGet != nil {
		r.onGet()
	}
	return r.doc, nil
}
func (r *boundaryCache) SaveParsedMessage(context.Context, authz.Actor, uuid.UUID, company.ParsedMessage) error {
	if r.onSave != nil {
		r.onSave()
	}
	return nil
}

func TestCompanyCachedContentRechecksAuthorityAndProvenance(t *testing.T) {
	for _, change := range []string{"revoked", "removed", "source-replaced", "wrong-message", "wrong-source", "wrong-parser"} {
		t.Run(change, func(t *testing.T) {
			_, repo, objects, actor, mailbox, message := boundaryFixture()
			cache := &boundaryCache{boundaryRepository: repo, doc: &company.ParsedMessage{MessageID: message, SourceKey: "original-key", ParserVersion: 1, TextBody: "private cached body"}}
			switch change {
			case "wrong-message":
				cache.doc.MessageID = uuid.New()
			case "wrong-source":
				cache.doc.SourceKey = "other-source"
			case "wrong-parser":
				cache.doc.ParserVersion = 2
			default:
				cache.onGet = func() { repo.change(change) }
			}
			s := NewService(cache, objects)
			value, err := s.Message(context.Background(), actor, mailbox, message)
			if err == nil || value != nil {
				t.Fatal("stale or mismatched cached content escaped")
			}
			if objects.opens.Load() != 0 {
				t.Fatal("denied cache lookup fell back to raw storage")
			}
		})
	}
}

func TestCompanyContentRechecksAfterCacheWrite(t *testing.T) {
	_, repo, objects, actor, mailbox, message := boundaryFixture()
	cache := &boundaryCache{boundaryRepository: repo, onSave: func() { repo.change("revoked") }}
	value, err := NewService(cache, objects).Message(context.Background(), actor, mailbox, message)
	if err == nil || value != nil {
		t.Fatal("content escaped after cache write completed with revoked authority")
	}
}

func TestCompanyReadCancellationDoesNotReturnSource(t *testing.T) {
	s, _, objects, actor, mailbox, message := boundaryFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objects.onOpen = cancel
	reader, err := s.Source(ctx, actor, mailbox, message)
	if reader != nil {
		_ = reader.Close()
	}
	if reader != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source returned reader=%t error=%v", reader != nil, err)
	}
	if objects.closes.Load() != 1 {
		t.Fatal("cancelled open leaked its reader")
	}
}
