package postgres_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"tabmail/internal/app/companymail"
	"tabmail/internal/app/mailindex"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
)

func persistenceDerivedMIME(contentType, body string) []byte {
	return []byte("From: client@fixture.test\r\nSubject: Persisted derived message\r\nMessage-ID: <persistence@fixture.test>\r\nMIME-Version: 1.0\r\nContent-Type: " + contentType + "; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte(body)) + "\r\n")
}

func TestR5DerivedTextPersistence(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("derived MIME persistence acceptance requires TABMAIL_TEST_DB_DSN; no skip")
	}
	for _, entry := range []string{"employee-read", "index-worker"} {
		for _, tc := range []struct{ name, contentType, body, want string }{
			{"nul-text", "text/plain", "alpha\x00omega", "alpha\ufffdomega"},
			{"invalid-utf8-text", "text/plain", "alpha\xffomega", "alpha\ufffdomega"},
			{"nul-html", "text/html", "<p>alpha\x00omega</p><script>unsafe()</script>", "alpha\ufffdomega"},
			{"unicode-control", "text/plain", "你好🙂omega", "你好🙂omega"},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				f := seedCompany(t)
				raw := persistenceDerivedMIME(tc.contentType, tc.body)
				key := rawobject.Key(raw)
				objects, err := fileobj.New(t.TempDir())
				must(t, err)
				must(t, objects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw))))
				message := &models.Message{TenantID: f.tenant.ID, ZoneID: f.zone.ID, MailboxID: f.personal.ID, Sender: "client@fixture.test", Recipients: []string{f.personal.FullAddress}, Subject: "Persisted derived message", Size: int64(len(raw)), RawObjectKey: key}
				must(t, f.st.CreateMessage(ctx, message))
				service := companymail.NewService(f.st, objects)
				if entry == "employee-read" {
					view, err := service.Message(ctx, f.u, f.personal.ID, message.ID)
					must(t, err)
					if view == nil || !strings.Contains(view.TextBody+view.HTMLBody, tc.want) {
						t.Fatalf("employee content missing canonical derived text: %+v", view)
					}
				} else {
					processed, err := mailindex.New(f.st, objects, zerolog.Nop()).Batch(ctx)
					must(t, err)
					if processed != 1 {
						t.Fatalf("index processed=%d, want 1", processed)
					}
					var state, diagnostic string
					must(t, f.pool.QueryRow(ctx, `SELECT state,last_error FROM mail_index_jobs WHERE tenant_id=$1 AND message_id=$2`, f.tenant.ID, message.ID).Scan(&state, &diagnostic))
					if state != "ready" || diagnostic != "" {
						t.Fatalf("valid MIME left index unresolved: state=%q diagnostic=%q", state, diagnostic)
					}
				}
				stored, err := f.st.GetParsedMessage(ctx, f.u, f.personal.ID, message.ID)
				must(t, err)
				if stored == nil || stored.SourceKey != key || stored.SourceSHA256 != mailcontent.Hash(raw) || !strings.Contains(stored.TextBody+stored.HTMLBody, tc.want) {
					t.Fatalf("persisted derived document disagrees with verified source: %+v", stored)
				}
				if strings.ContainsRune(stored.TextBody+stored.HTMLBody, 0) || !utf8.ValidString(stored.TextBody+stored.HTMLBody) || strings.Contains(stored.HTMLBody, "unsafe()") {
					t.Fatal("stored document contains invalid text or unsafe HTML")
				}
				matches, count, err := f.st.ListWorkMessages(ctx, f.u, f.personal.ID, "inbox", "omega", models.Page{})
				must(t, err)
				if count != 1 || len(matches) != 1 || matches[0].ID != message.ID {
					t.Fatal("body-derived search index failed to find persisted content")
				}
				source, err := service.Source(ctx, f.u, f.personal.ID, message.ID)
				must(t, err)
				original, err := io.ReadAll(source)
				must(t, err)
				must(t, source.Close())
				if !bytes.Equal(original, raw) {
					t.Fatal("derived text repair modified the downloadable raw source")
				}
			})
		}
	}
}
