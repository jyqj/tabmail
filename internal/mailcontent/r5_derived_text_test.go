package mailcontent

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"tabmail/internal/rawobject"
)

func derivedTextMIME(contentType string, body, attachment []byte) []byte {
	var raw strings.Builder
	raw.WriteString("From: client@fixture.test\r\nSubject: Derived text\r\nMessage-ID: <derived@fixture.test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=derived\r\n\r\n--derived\r\nContent-Type: ")
	raw.WriteString(contentType)
	raw.WriteString("; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	raw.WriteString(base64.StdEncoding.EncodeToString(body))
	raw.WriteString("\r\n--derived\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=original.bin\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	raw.WriteString(base64.StdEncoding.EncodeToString(attachment))
	raw.WriteString("\r\n--derived--\r\n")
	return []byte(raw.String())
}

func TestR5DerivedTextPreservesSourceAndAttachment(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"ascii", "alpha omega", "alpha omega"},
		{"unicode", "汉字🙂café", "汉字🙂café"},
		{"whitespace", "one\r\ntwo\tthree\n", "one\r\ntwo\tthree\n"},
		{"middle-nul", "alpha\x00omega", "alpha\ufffdomega"},
		{"leading-nul", "\x00alpha", "\ufffdalpha"},
		{"trailing-nul", "omega\x00", "omega\ufffd"},
		{"repeated-nul", "a\x00\x00b", "a\ufffd\ufffdb"},
		{"invalid-utf8", "a\xffb", "a\ufffdb"},
		{"truncated-utf8", "a\xe2\x82", "a\ufffd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attachment := []byte{0, 0xff, 0x80, 'A', '\r', '\n'}
			raw := derivedTextMIME("text/plain", []byte(tc.body), attachment)
			original := bytes.Clone(raw)
			key, message := rawobject.Key(raw), uuid.New()
			objects := &fixtureObjects{raw: raw}
			parser := New(objects)
			env, err := parser.Envelope(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			observedText := env.Text
			doc, err := parser.Document(context.Background(), message, key)
			if err != nil {
				t.Fatal(err)
			}
			if doc.TextBody != tc.want {
				t.Errorf("derived text=%q, want %q", doc.TextBody, tc.want)
			}
			if !utf8.ValidString(doc.TextBody) || strings.ContainsRune(doc.TextBody, 0) {
				t.Error("derived body is not representable as PostgreSQL UTF-8 text")
			}
			if doc.SourceKey != key || doc.SourceSHA256 != Hash(original) || env.Text != observedText || !bytes.Equal(raw, original) {
				t.Error("derived projection changed source, decoded envelope or integrity identity")
			}
			if len(doc.Parts) != 1 {
				t.Fatalf("attachment count=%d, want 1", len(doc.Parts))
			}
			part, content, err := parser.Attachment(context.Background(), message, key, doc.Parts[0].ID)
			if err != nil || part.SHA256 != Hash(attachment) || !bytes.Equal(content, attachment) {
				t.Errorf("binary attachment changed: part=%+v, err=%v", part, err)
			}
			cached, err := parser.Document(context.Background(), message, key)
			if err != nil || cached.TextBody != doc.TextBody || cached.Parts[0].ID != doc.Parts[0].ID || objects.reads.Load() != 1 {
				t.Error("cached projection changed bytes, part identity or reopened the source")
			}
		})
	}
}

func TestR5DerivedHTMLTextStillSanitizes(t *testing.T) {
	for _, tc := range []struct{ name, html, contained string }{
		{"nul", "<p>alpha\x00omega</p><script>alert(1)</script>", "alpha\ufffdomega"},
		{"invalid-utf8", "<p>alpha\xffomega</p><script>alert(1)</script>", "alpha\ufffdomega"},
		{"unicode", "<p>汉字🙂café</p><script>alert(1)</script>", "汉字🙂café"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := derivedTextMIME("text/html", []byte(tc.html), []byte{0, 0xff})
			parser := New(&fixtureObjects{raw: raw})
			env, err := parser.Envelope(context.Background(), rawobject.Key(raw))
			if err != nil {
				t.Fatal(err)
			}
			observed := env.HTML
			doc, err := parser.Document(context.Background(), uuid.New(), rawobject.Key(raw))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc.HTMLBody, tc.contained) || strings.Contains(doc.HTMLBody, "<script") || strings.Contains(doc.HTMLBody, "alert(1)") {
				t.Errorf("unsafe or altered HTML projection: %q", doc.HTMLBody)
			}
			if strings.ContainsRune(doc.HTMLBody, 0) || !utf8.ValidString(doc.HTMLBody) || strings.ContainsRune(doc.TextBody, 0) || !utf8.ValidString(doc.TextBody) {
				t.Error("derived HTML or fallback text is not database-safe UTF-8")
			}
			if env.HTML != observed || doc.SourceSHA256 != Hash(raw) || doc.BodyAccess != "" {
				t.Error("HTML repair changed immutable source or falsely reported sanitizer failure")
			}
		})
	}
}
