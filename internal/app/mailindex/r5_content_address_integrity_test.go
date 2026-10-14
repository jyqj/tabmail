package mailindex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
)

func TestProgressIndexRejectsCorruptedAddressedOriginal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	objects, err := fileobj.New(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("Subject: content identity\r\n\r\nowned body")
	key := rawobject.Key(raw)
	if err = objects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, key), bytes.Replace(raw, []byte("owned body"), []byte("other body"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := &r5MIMECloseIndexRepo{job: company.MailIndexJob{MessageID: uuid.New(), SourceKey: key}}
	service := New(repo, objects, zerolog.Nop())
	if count, err := service.Batch(ctx); count != 1 || err != nil {
		t.Errorf("source corruption bypassed existing index failure protocol: count=%d err=%v", count, err)
	}
	if repo.failed != 1 || repo.completed != 0 {
		t.Errorf("corrupt original reached index completion: failures=%d completed=%d", repo.failed, repo.completed)
	}
	if err = os.WriteFile(filepath.Join(root, key), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if count, err := service.Batch(ctx); count != 1 || err != nil || repo.failed != 1 || repo.completed != 1 {
		t.Fatalf("index retry reused corrupted cache: count=%d err=%v failures=%d completed=%d", count, err, repo.failed, repo.completed)
	}
}
