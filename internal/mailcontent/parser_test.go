package mailcontent

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

type fixtureObjects struct {
	raw   []byte
	reads atomic.Int32
}

func (o *fixtureObjects) Get(context.Context, string) (io.ReadCloser, error) {
	o.reads.Add(1)
	return io.NopCloser(bytes.NewReader(o.raw)), nil
}

const multipartFixture = "From: Client <client@test>\r\nTo: staff@test\r\nSubject: parser fixture\r\nMessage-ID: <first@test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=boundary\r\n\r\n--boundary\r\nContent-Type: text/plain\r\n\r\nBody text\r\n--boundary\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=part.txt\r\n\r\nAttachment bytes\r\n--boundary--\r\n"

func TestDocumentIDsBindMessageSourceAndPart(t *testing.T) {
	obj := &fixtureObjects{raw: []byte(multipartFixture)}
	p := New(obj)
	id := uuid.New()
	ctx := context.Background()
	d, e := p.Document(ctx, id, "immutable")
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Parts) != 1 || len(d.Parts[0].ID) != 64 {
		t.Fatalf("parts %+v", d.Parts)
	}
	same, e := p.Document(ctx, id, "immutable")
	if e != nil || same.Parts[0].ID != d.Parts[0].ID {
		t.Fatal("unstable IDs", e)
	}
	other, e := p.Document(ctx, uuid.New(), "immutable")
	if e != nil || other.Parts[0].ID == d.Parts[0].ID {
		t.Fatal("message boundary not bound")
	}
	_, raw, e := p.Attachment(ctx, id, "immutable", d.Parts[0].ID)
	if e != nil || !bytes.Contains(raw, []byte("Attachment bytes")) {
		t.Fatal("download mismatch", e)
	}
	raw[0] = 'x'
	_, again, e := p.Attachment(ctx, id, "immutable", d.Parts[0].ID)
	if e != nil || again[0] == 'x' {
		t.Fatal("cached part was mutable")
	}
	if _, _, e = p.Attachment(ctx, uuid.New(), "immutable", d.Parts[0].ID); e == nil {
		t.Fatal("ID accepted from another message")
	}
	if obj.reads.Load() != 1 {
		t.Fatal("reparsed immutable object", obj.reads.Load())
	}
}
func TestConcurrentParseHasBoundedSharedCache(t *testing.T) {
	obj := &fixtureObjects{raw: []byte(multipartFixture)}
	p := New(obj)
	var wg sync.WaitGroup
	id := uuid.New()
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := p.Document(context.Background(), id, "one"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if p.bytes > cacheBudget || p.lru.Len() != 1 {
		t.Fatal("unbounded parser cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Document(ctx, id, "new"); e == nil {
		t.Fatal("cancelled waiter blocked or succeeded")
	}
}
func TestParserRejectsOversizedSources(t *testing.T) {
	obj := &fixtureObjects{raw: bytes.Repeat([]byte("x"), int(MaxBytes)+1)}
	if _, e := New(obj).Document(context.Background(), uuid.New(), "large"); e == nil {
		t.Fatal("unbounded input accepted")
	}
}
