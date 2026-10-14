package outbound

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"unicode/utf8"

	"tabmail/internal/company"
)

func TestBuild_SubjectWireRoundTrip(t *testing.T) {
	cases := []struct {
		name, subject string
	}{
		{"empty", ""},
		{"ascii", "Hello, world!"},
		{"chinese", "中文主题：欢迎加入公司"},
		{"emoji", "📨🙂👩🏽‍💻🇨🇳"},
		{"mixed_scripts", "通知 / Καλημέρα / مرحبا / café / 日本語 🙂"},
		{"combining_marks", strings.Repeat("e\u0301", 60)},
		{"spaces", "  leading  repeated   and trailing  "},
		{"tabs", "\tleading\t\tinside\ttrailing\t"},
		{"whitespace_only", " \t \t  "},
		{"punctuation", "(Re:) [a,b]; <x@y> \\\"quote\\\" = ? _ : !"},
		{"literal_encoded_word", "=?UTF-8?B?SGVsbG8=?="},
		{"literal_encoded_words", "=?utf-8?q?hello_world?=\t=?UTF-8?B?5Lit5paH?="},
		{"malformed_encoded_word", "prefix =?utf-8?b?not-base64!?= suffix"},
		{"crlf_stripped", "通知\r\nBcc: hidden@example.test\r\n\tend"},
	}
	for _, n := range []int{989, 990, 998} {
		cases = append(cases,
			struct{ name, subject string }{fmt.Sprintf("ascii_unspaced_%d", n), strings.Repeat("x", n)},
			struct{ name, subject string }{fmt.Sprintf("ascii_spaced_%d", n), strings.Repeat("word ", n/5) + strings.Repeat("x", n%5)},
		)
	}
	for _, prefix := range []int{39, 40, 41, 42, 43} {
		for _, char := range []string{"é", "中", "🙂"} {
			cases = append(cases, struct{ name, subject string }{
				fmt.Sprintf("character_boundary_%d_%x", prefix, []byte(char)),
				strings.Repeat("x", prefix) + char + strings.Repeat("y", 50),
			})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSubjectWireRoundTrip(t, Message{Subject: tc.subject, TextBody: "body"})
		})
	}
}

func TestBuild_SubjectRenderedTemplateAtByteLimit(t *testing.T) {
	draft := company.TemplateDraft{
		Subject:   "{{.company_name}} {{.value}}",
		TextBody:  "{{.employee_name}}",
		HTMLBody:  "<p>{{.value}}</p>",
		Variables: []company.Variable{{Name: "value", Type: "text", MaxLength: 1000}},
	}
	value := strings.Repeat("中", 329) + "abcd"
	subject, text, html, err := company.Render(draft, map[string]string{"value": value}, "张三🙂", "星河", "sender@example.test")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(subject) != 998 || subject != "星河 "+value {
		t.Fatalf("rendered subject: bytes=%d, value=%q", len(subject), subject)
	}
	assertSubjectWireRoundTrip(t, Message{Subject: subject, TextBody: text, HTMLBody: html})
}

func assertSubjectWireRoundTrip(t *testing.T, m Message) {
	t.Helper()
	m.From = "sender@example.test"
	m.To = []string{"to@example.test"}
	m.CC = []string{"cc@example.test"}
	m.BCC = []string{"secret@example.test"}
	m.MessageID = "<subject@example.test>"
	m.Headers = map[string]string{"Subject": "must-not-override", "X-Subject-Control": "control\r\nvalue"}
	wire, err := Build(m)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(string(wire), "secret@example.test") || strings.Contains(string(wire), "must-not-override") {
		t.Fatal("BCC or forbidden custom Subject leaked into MIME")
	}
	headers, _, ok := strings.Cut(string(wire), "\r\n\r\n")
	if !ok {
		t.Fatal("missing header/body separator")
	}
	var subjectLines []string
	inSubject := false
	for _, line := range strings.Split(headers, "\r\n") {
		if len(line) > 998 {
			t.Errorf("header line has %d bytes, limit 998", len(line))
		}
		for _, b := range []byte(line) {
			if b >= 128 || b == '\r' || b == '\n' {
				t.Errorf("non-ASCII or bare newline in wire header: %q", line)
				break
			}
		}
		if strings.HasPrefix(line, "Subject:") {
			inSubject = true
			subjectLines = append(subjectLines, line)
		} else if inSubject && strings.HasPrefix(line, " ") {
			subjectLines = append(subjectLines, line)
		} else {
			inSubject = false
		}
	}
	if len(subjectLines) == 0 {
		t.Fatal("missing Subject header")
	}
	for _, line := range subjectLines {
		hasEncodedWord := false
		value := strings.TrimPrefix(line, "Subject: ")
		for _, word := range strings.Fields(value) {
			if _, err := new(mime.WordDecoder).Decode(word); err != nil {
				continue // Literal text that is not a valid encoded-word.
			}
			hasEncodedWord = true
			if len(word) > 75 {
				t.Errorf("encoded word has %d bytes, limit 75", len(word))
			}
			if !strings.HasPrefix(word, "=?UTF-8?b?") || !strings.HasSuffix(word, "?=") {
				t.Errorf("non-canonical encoded word: %q", word)
				continue
			}
			encoded := strings.TrimSuffix(strings.TrimPrefix(word, "=?UTF-8?b?"), "?=")
			decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil || !utf8.Valid(decoded) || base64.StdEncoding.EncodeToString(decoded) != encoded {
				t.Errorf("invalid Base64 or split UTF-8 character in encoded word: %q, err=%v", word, err)
			}
		}
		if hasEncodedWord && len(line) > 76 {
			t.Errorf("encoded-word line has %d bytes, limit 76", len(line))
		}
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatalf("mail.ReadMessage: %v", err)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	want := strings.NewReplacer("\r", "", "\n", "").Replace(m.Subject)
	if decoded != want {
		t.Errorf("Subject round trip: got %q, want %q", decoded, want)
	}
	for key, want := range map[string]string{
		"From": "sender@example.test", "To": "to@example.test", "Cc": "cc@example.test",
		"Message-ID": "<subject@example.test>", "X-Subject-Control": "controlvalue",
	} {
		if got := parsed.Header.Get(key); got != want {
			t.Errorf("unrelated header %s changed: got %q, want %q", key, got, want)
		}
	}
}
