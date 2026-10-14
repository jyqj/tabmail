package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
)

type progressIdentityCache struct {
	*contentRepo
	doc     *company.ParsedMessage
	saves   int
	onGet   func()
	saveErr error
}

func (r *progressIdentityCache) GetParsedMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*company.ParsedMessage, error) {
	if r.onGet != nil {
		r.onGet()
	}
	return r.doc, nil
}
func (r *progressIdentityCache) SaveParsedMessage(_ context.Context, _ authz.Actor, _ uuid.UUID, doc company.ParsedMessage) error {
	r.saves++
	if r.saveErr != nil {
		return r.saveErr
	}
	r.doc = &doc
	return nil
}

type progressIdentityFiles struct {
	*fileobj.FileStore
	opens int
}

func (f *progressIdentityFiles) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f.opens++
	return f.FileStore.Get(ctx, key)
}

func TestProgressCompanyRebuildsInvalidDerivedIdentity(t *testing.T) {
	for _, mode := range []string{"addressed empty", "addressed mismatch", "addressed nonhex", "legacy empty", "ingress empty", "legacy valid", "ingress valid", "addressed valid"} {
		t.Run(mode, func(t *testing.T) {
			_, base, original, actor, mailbox, message := contentFixture()
			root := t.TempDir()
			files, err := fileobj.New(root)
			if err != nil {
				t.Fatal(err)
			}
			objects := &progressIdentityFiles{FileStore: files}
			raw := original.raw
			key := rawobject.Key(raw)
			if strings.HasPrefix(mode, "legacy") {
				key = "legacy/original.eml"
			}
			if strings.HasPrefix(mode, "ingress") {
				key = "ingress-" + uuid.NewString() + ".eml"
			}
			base.message.RawObjectKey = key
			if err = objects.Put(context.Background(), key, bytes.NewReader(raw), int64(len(raw))); err != nil {
				t.Fatal(err)
			}
			hash := ""
			body := "damaged cached body"
			if strings.HasSuffix(mode, "mismatch") {
				hash = mailcontent.Hash([]byte("a different source"))
			} else if strings.HasSuffix(mode, "nonhex") {
				hash = strings.Repeat("z", 64)
			} else if strings.HasSuffix(mode, "valid") {
				hash, body = mailcontent.Hash(raw), "private body"
			}
			repo := &progressIdentityCache{contentRepo: base, doc: &company.ParsedMessage{MessageID: message, SourceKey: key, SourceSHA256: hash, ParserVersion: 1, TextBody: body}}
			service := NewService(repo, objects)
			for i := 0; i < 2; i++ {
				value, err := service.Message(context.Background(), actor, mailbox, message)
				if err != nil || value == nil || value.TextBody != "private body" {
					t.Errorf("invalid cache identity escaped or healthy content failed: value=%+v err=%v", value, err)
				}
			}
			want := 1
			if strings.HasSuffix(mode, "valid") {
				want = 0
			}
			if repo.saves != want || objects.opens != want || repo.doc.SourceSHA256 != mailcontent.Hash(raw) {
				t.Errorf("cache rebuild/reuse was not coherent: saves=%d opens=%d hash=%q", repo.saves, objects.opens, repo.doc.SourceSHA256)
			}
		})
	}
}

func TestProgressCompanyCorruptionDoesNotPublishDerivedContent(t *testing.T) {
	for _, entry := range []string{"message", "attachments"} {
		t.Run(entry, func(t *testing.T) {
			_, base, original, actor, mailbox, message := contentFixture()
			root := t.TempDir()
			files, err := fileobj.New(root)
			if err != nil {
				t.Fatal(err)
			}
			objects := &progressIdentityFiles{FileStore: files}
			key, raw := rawobject.Key(original.raw), original.raw
			base.message.RawObjectKey = key
			if err = files.Put(context.Background(), key, bytes.NewReader(raw), int64(len(raw))); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(root, key), bytes.Replace(raw, []byte("private body"), []byte("corrupt body"), 1), 0o600); err != nil {
				t.Fatal(err)
			}
			repo := &progressIdentityCache{contentRepo: base}
			service := NewService(repo, objects)
			call := func() (bool, error) {
				if entry == "message" {
					v, err := service.Message(context.Background(), actor, mailbox, message)
					return v != nil, err
				}
				v, err := service.InboundAttachments(context.Background(), actor, mailbox, message)
				return v != nil, err
			}
			value, err := call()
			if value || err == nil {
				t.Errorf("corrupt content was returned: value=%v err=%v", value, err)
			} else {
				expectKind(t, err, app.KindInternal)
			}
			if repo.saves != 0 || repo.doc != nil {
				t.Error("corrupt bytes reached persistent parsed-content cache")
			}
			if err = os.WriteFile(filepath.Join(root, key), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if value, err = call(); !value || err != nil {
				t.Fatalf("healthy source retry failed: value=%v err=%v", value, err)
			}
			if repo.doc == nil || repo.doc.TextBody != "private body" || repo.doc.SourceSHA256 != mailcontent.Hash(raw) || objects.opens != 2 || repo.saves != 1 {
				t.Errorf("retry reused corrupted document: doc=%+v opens=%d saves=%d", repo.doc, objects.opens, repo.saves)
			}
		})
	}
}

func TestProgressCompanyCacheRebuildKeepsFailureAndAuthority(t *testing.T) {
	for _, mode := range []string{"missing raw", "save failed", "revoked", "source replaced"} {
		t.Run(mode, func(t *testing.T) {
			_, base, original, actor, mailbox, message := contentFixture()
			key := rawobject.Key(original.raw)
			base.message.RawObjectKey = key
			files, err := fileobj.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			objects := &progressIdentityFiles{FileStore: files}
			if mode != "missing raw" {
				if err = files.Put(context.Background(), key, bytes.NewReader(original.raw), int64(len(original.raw))); err != nil {
					t.Fatal(err)
				}
			}
			repo := &progressIdentityCache{contentRepo: base, doc: &company.ParsedMessage{MessageID: message, SourceKey: key, ParserVersion: 1, TextBody: "stale private cache"}}
			failure := errors.New("controlled derived write failed")
			switch mode {
			case "save failed":
				repo.saveErr = failure
			case "revoked":
				repo.onGet = func() { base.denied = app.Forbidden("read revoked") }
			case "source replaced":
				repo.onGet = func() { base.message.RawObjectKey = "replacement-source" }
			}
			value, err := NewService(repo, objects).Message(context.Background(), actor, mailbox, message)
			if value != nil || err == nil {
				t.Errorf("unverified stale cache survived failed rebuild: value=%+v err=%v", value, err)
			}
			if mode == "save failed" && !errors.Is(err, failure) {
				t.Errorf("save cause lost: %v", err)
			}
			if mode == "missing raw" && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("missing original cause lost: %v", err)
			}
			if mode == "revoked" || mode == "source replaced" {
				if objects.opens != 0 || repo.saves != 0 {
					t.Error("denied cached read opened or rebuilt original content")
				}
			}
			if repo.doc.TextBody != "stale private cache" {
				t.Error("failure claimed a successful derived write")
			}
		})
	}
}
