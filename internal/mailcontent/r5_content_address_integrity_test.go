package mailcontent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
)

func TestProgressParserRejectsCorruptedAddressedSource(t *testing.T) {
	for _, entry := range []string{"envelope", "document", "attachment"} {
		for _, damage := range []string{"same size", "shorter", "longer"} {
			t.Run(entry+"/"+damage, func(t *testing.T) {
				ctx := context.Background()
				root := t.TempDir()
				objects, err := fileobj.New(root)
				if err != nil {
					t.Fatal(err)
				}
				raw := []byte(multipartFixture)
				key, message := rawobject.Key(raw), uuid.New()
				if err = objects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))); err != nil {
					t.Fatal(err)
				}
				healthy, err := New(objects).Document(ctx, message, key)
				if err != nil || len(healthy.Parts) != 1 {
					t.Fatalf("healthy addressed fixture: %v", err)
				}
				corrupt := bytes.Replace(raw, []byte("Body text"), []byte("Evil text"), 1)
				switch damage {
				case "shorter":
					corrupt = bytes.Replace(raw, []byte("Body text"), []byte("Body"), 1)
				case "longer":
					corrupt = bytes.Replace(raw, []byte("Body text"), []byte("Unexpected expanded body"), 1)
				}
				// Fault injection changes the actual filesystem after a successful
				// canonical write. FileStore.Put's publication policy is unchanged.
				if err = os.WriteFile(filepath.Join(root, key), corrupt, 0o600); err != nil {
					t.Fatal(err)
				}
				parser := New(objects)
				call := func() (bool, error) {
					switch entry {
					case "envelope":
						env, err := parser.Envelope(ctx, key)
						return env != nil, err
					case "document":
						doc, err := parser.Document(ctx, message, key)
						return doc != nil, err
					default:
						part, data, err := parser.Attachment(ctx, message, key, healthy.Parts[0].ID)
						return part != nil || data != nil, err
					}
				}
				if value, err := call(); value || err == nil || errors.Is(err, ErrAttachmentNotFound) {
					t.Errorf("corrupt source was published or classified as absent attachment: value=%v err=%v", value, err)
				}
				if parser.lru.Len() != 0 || parser.bytes != 0 || len(parser.parses) != 0 {
					t.Error("corrupt source retained a parsed value or admission slot")
				}
				if err = os.WriteFile(filepath.Join(root, key), raw, 0o600); err != nil {
					t.Fatal(err)
				}
				if value, err := call(); !value || err != nil {
					t.Fatalf("healthy repair could not be read: value=%v err=%v", value, err)
				}
				doc, err := parser.Document(ctx, message, key)
				if err != nil || doc.TextBody != healthy.TextBody || doc.SourceSHA256 != healthy.SourceSHA256 || doc.Parts[0].ID != healthy.Parts[0].ID {
					t.Fatalf("repair reused corrupt derived content: doc=%+v err=%v", doc, err)
				}
			})
		}
	}
}

func TestProgressParserAddressedReadKeepsOriginalErrorChain(t *testing.T) {
	for _, failure := range []string{"read", "close", "no progress"} {
		t.Run(failure, func(t *testing.T) {
			cause := errors.New("controlled storage failure")
			r := &r5MIMECloseReader{read: bytes.NewReader([]byte(multipartFixture)).Read, close: func() error { return nil }}
			switch failure {
			case "read":
				r.read = func(b []byte) (int, error) { return copy(b, "damaged bytes"), cause }
			case "close":
				r.close = func() error { return cause }
			case "no progress":
				r.read = func([]byte) (int, error) { return 0, nil }
				cause = io.ErrNoProgress
			}
			p := New(resourceObjectFunc(func(context.Context, string) (io.ReadCloser, error) { return r, nil }))
			doc, err := p.Document(context.Background(), uuid.New(), rawobject.Key([]byte(multipartFixture)))
			if doc != nil || !errors.Is(err, cause) || r.closes.Load() != 1 || p.lru.Len() != 0 {
				t.Fatalf("source identity validation replaced an I/O failure: doc=%v err=%v closes=%d", doc != nil, err, r.closes.Load())
			}
		})
	}
}
