package outbound

import (
	"strings"
	"testing"
)

func TestBuild_TextOnly(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Hello",
		TextBody:  "Hello world",
		MessageID: "<test@example.com>",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "Content-Type: text/plain") {
		t.Error("expected text/plain content type")
	}
	if strings.Contains(s, "text/html") {
		t.Error("unexpected text/html in text-only message")
	}
	if !strings.Contains(s, "From: sender@example.com") {
		t.Error("missing From header")
	}
	if !strings.Contains(s, "To: to@example.com") {
		t.Error("missing To header")
	}
}

func TestBuild_HTMLOnly(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Hello",
		HTMLBody:  "<p>Hello world</p>",
		MessageID: "<test@example.com>",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "Content-Type: text/html") {
		t.Error("expected text/html content type")
	}
	if strings.Contains(s, "text/plain") {
		t.Error("unexpected text/plain in html-only message")
	}
}

func TestBuild_MultipartAlternative(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Hello",
		TextBody:  "Hello world",
		HTMLBody:  "<p>Hello world</p>",
		MessageID: "<test@example.com>",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("expected multipart/alternative content type")
	}
	if !strings.Contains(s, "text/plain") {
		t.Error("expected text/plain part")
	}
	if !strings.Contains(s, "text/html") {
		t.Error("expected text/html part")
	}
}

func TestBuild_BCCNotInHeaders(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		CC:        []string{"cc@example.com"},
		BCC:       []string{"bcc@example.com"},
		Subject:   "Hello",
		TextBody:  "Hello world",
		MessageID: "<test@example.com>",
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "To: to@example.com") {
		t.Error("missing To header")
	}
	if !strings.Contains(s, "Cc: cc@example.com") {
		t.Error("missing Cc header")
	}
	if strings.Contains(s, "bcc@example.com") {
		t.Error("BCC address must not appear in MIME headers")
	}
}

// TestBuild_BCCNeverLeaksWithoutMetadata pins the bug the old shallow builder
// carried: when recipient metadata was missing it dumped every RcptTo address
// into the To header, exposing BCC. With recipients typed on the Message there
// is no missing-metadata path — BCC can only ever be an envelope recipient.
func TestBuild_BCCNeverLeaksWithoutMetadata(t *testing.T) {
	msg := Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		BCC:       []string{"secret@example.com"},
		Subject:   "Hello",
		TextBody:  "Hi",
		MessageID: "<test@example.com>",
	}
	mime, err := Build(msg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if strings.Contains(string(mime), "secret@example.com") {
		t.Error("BCC recipient leaked into the rendered message")
	}
	env := msg.EnvelopeRecipients()
	if len(env) != 2 || env[0] != "to@example.com" || env[1] != "secret@example.com" {
		t.Errorf("envelope must include BCC for delivery, got %v", env)
	}
}

func TestBuild_CustomHeaders(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Hello",
		TextBody:  "Hello world",
		MessageID: "<test@example.com>",
		Headers: map[string]string{
			"X-Custom": "should-appear",
		},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(string(mime), "X-Custom: should-appear") {
		t.Error("legitimate custom header X-Custom should appear")
	}
}

func TestBuild_HeaderNameInjectionBlocked(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"CRLF injection", "Evil\r\nBcc"},
		{"LF injection", "Evil\nBcc"},
		{"CR injection", "Evil\rBcc"},
		{"space in name", "Evil Header"},
		{"colon in name", "Evil:Header"},
		{"empty name", ""},
		{"starts with hyphen", "-Evil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mime, err := Build(Message{
				From:      "sender@example.com",
				To:        []string{"to@example.com"},
				Subject:   "Hello",
				TextBody:  "Hello world",
				MessageID: "<test@example.com>",
				Headers:   map[string]string{tt.key: "injected-value"},
			})
			if err != nil {
				t.Fatalf("Build error: %v", err)
			}
			if strings.Contains(string(mime), "injected-value") {
				t.Errorf("header with invalid name %q should be blocked", tt.key)
			}
		})
	}
}

func TestBuild_ForbiddenHeadersBlocked(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Hello",
		TextBody:  "Hello world",
		MessageID: "<test@example.com>",
		Headers: map[string]string{
			"From":           "evil@attacker.com",
			"Bcc":            "hidden@attacker.com",
			"DKIM-Signature": "forged",
			"Return-Path":    "bounce@attacker.com",
			"X-Custom-Legit": "this-is-fine",
		},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)

	// Only one From header should exist (the real one).
	if strings.Count(s, "From:") != 1 {
		t.Error("forbidden From header was not blocked")
	}
	if strings.Contains(s, "evil@attacker.com") {
		t.Error("forbidden From value leaked into headers")
	}
	if strings.Contains(s, "hidden@attacker.com") {
		t.Error("forbidden Bcc header leaked")
	}
	if strings.Contains(s, "forged") {
		t.Error("forbidden DKIM-Signature header leaked")
	}
	if strings.Contains(s, "bounce@attacker.com") {
		t.Error("forbidden Return-Path header leaked")
	}
	if !strings.Contains(s, "X-Custom-Legit: this-is-fine") {
		t.Error("legitimate custom header was incorrectly blocked")
	}
}

func TestBuild_HeaderValueCRLFStripped(t *testing.T) {
	mime, err := Build(Message{
		From:      "sender@example.com",
		To:        []string{"to@example.com"},
		Subject:   "Normal\r\nBcc: evil@attacker.com",
		TextBody:  "Hello",
		MessageID: "<test@example.com>",
		Headers: map[string]string{
			"X-Custom": "value\r\nBcc: evil@attacker.com",
		},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	s := string(mime)

	// After sanitization, CR/LF are stripped so no header injection can occur.
	for _, line := range strings.Split(s, "\r\n") {
		if strings.HasPrefix(line, "Subject:") {
			if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
				t.Error("Subject header contains unstripped CR/LF")
			}
			break
		}
		if strings.HasPrefix(line, "Bcc:") {
			t.Error("CRLF injection created a Bcc header line")
		}
	}

	for _, line := range strings.Split(s, "\r\n") {
		if strings.HasPrefix(line, "X-Custom:") {
			if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
				t.Error("X-Custom header value contains unstripped CR/LF")
			}
			break
		}
	}
}

func TestIsValidHeaderName(t *testing.T) {
	valid := []string{
		"X-Custom",
		"Reply-To",
		"X123",
		"A",
		"Content-Disposition",
	}
	for _, name := range valid {
		if !isValidHeaderName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"",
		"-X-Bad",
		"Bad Name",
		"Bad:Name",
		"Bad\r\nName",
		"Bad\nName",
		strings.Repeat("A", 127), // exceeds 126 limit
	}
	for _, name := range invalid {
		if isValidHeaderName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}

	// Exactly 126 chars should be valid.
	if !isValidHeaderName(strings.Repeat("A", 126)) {
		t.Error("126-char header name should be valid")
	}
}
