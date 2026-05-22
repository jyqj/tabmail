package outbound

import (
	"encoding/json"
	"strings"
	"testing"

	"tabmail/internal/models"
)

func TestBuildMIME_TextOnly(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Hello",
		TextBody:        "Hello world",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON:     mustJSON(t, map[string]any{"_to": []string{"to@example.com"}}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
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

func TestBuildMIME_HTMLOnly(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Hello",
		HTMLBody:        "<p>Hello world</p>",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON:     mustJSON(t, map[string]any{"_to": []string{"to@example.com"}}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "Content-Type: text/html") {
		t.Error("expected text/html content type")
	}
	if strings.Contains(s, "text/plain") {
		t.Error("unexpected text/plain in html-only message")
	}
}

func TestBuildMIME_MultipartAlternative(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Hello",
		TextBody:        "Hello world",
		HTMLBody:        "<p>Hello world</p>",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON:     mustJSON(t, map[string]any{"_to": []string{"to@example.com"}}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
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

func TestBuildMIME_BCCNotInHeaders(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com", "cc@example.com", "bcc@example.com"},
		Subject:         "Hello",
		TextBody:        "Hello world",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON: mustJSON(t, map[string]any{
			"_to": []string{"to@example.com"},
			"_cc": []string{"cc@example.com"},
		}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
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

func TestBuildMIME_InternalMetadataKeysNotInHeaders(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Hello",
		TextBody:        "Hello world",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON: mustJSON(t, map[string]any{
			"_to":     []string{"to@example.com"},
			"_cc":     []string{},
			"_custom": "should-not-appear",
			"X-Custom": "should-appear",
		}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
	}
	s := string(mime)

	// Internal metadata keys prefixed with _ must not appear as custom headers.
	if strings.Contains(s, "_to:") || strings.Contains(s, "_cc:") || strings.Contains(s, "_custom:") {
		t.Error("internal metadata keys (_to, _cc, _custom) must not appear as MIME headers")
	}

	if !strings.Contains(s, "X-Custom: should-appear") {
		t.Error("legitimate custom header X-Custom should appear")
	}
}

func TestBuildMIME_HeaderNameInjectionBlocked(t *testing.T) {
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
			hdrs := map[string]any{
				"_to": []string{"to@example.com"},
				tt.key: "injected-value",
			}
			job := &models.OutboundJob{
				MailFrom:        "sender@example.com",
				RcptTo:          []string{"to@example.com"},
				Subject:         "Hello",
				TextBody:        "Hello world",
				MessageIDHeader: "<test@example.com>",
				HeadersJSON:     mustJSON(t, hdrs),
			}
			mime, err := BuildMIME(job)
			if err != nil {
				t.Fatalf("BuildMIME error: %v", err)
			}
			if strings.Contains(string(mime), "injected-value") {
				t.Errorf("header with invalid name %q should be blocked", tt.key)
			}
		})
	}
}

func TestBuildMIME_ForbiddenHeadersBlocked(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Hello",
		TextBody:        "Hello world",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON: mustJSON(t, map[string]any{
			"_to":                  []string{"to@example.com"},
			"From":                 "evil@attacker.com",
			"Bcc":                  "hidden@attacker.com",
			"DKIM-Signature":       "forged",
			"Return-Path":          "bounce@attacker.com",
			"X-Custom-Legit":       "this-is-fine",
		}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
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

func TestBuildMIME_HeaderValueCRLFStripped(t *testing.T) {
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Normal\r\nBcc: evil@attacker.com",
		TextBody:        "Hello",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON: mustJSON(t, map[string]any{
			"_to":      []string{"to@example.com"},
			"X-Custom": "value\r\nBcc: evil@attacker.com",
		}),
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
	}
	s := string(mime)

	// After sanitization, CR/LF are stripped so no header injection can occur.
	// The Subject should be on a single line (no newline within the value).
	for _, line := range strings.Split(s, "\r\n") {
		if strings.HasPrefix(line, "Subject:") {
			if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
				t.Error("Subject header contains unstripped CR/LF")
			}
			// The injected Bcc must not appear as a separate header line.
			break
		}
		// Check that no standalone Bcc header was injected.
		if strings.HasPrefix(line, "Bcc:") {
			t.Error("CRLF injection created a Bcc header line")
		}
	}

	// X-Custom value must also not create a new header line.
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

func TestBuildMIME_RawMIMEIgnored(t *testing.T) {
	// RawMIME passthrough was removed for security — BuildMIME must always use
	// the structured builder so that header safety checks are enforced.
	raw := []byte("From: evil@attacker.com\r\nBcc: leak@attacker.com\r\nSubject: Raw\r\n\r\nBody")
	job := &models.OutboundJob{
		MailFrom:        "sender@example.com",
		RcptTo:          []string{"to@example.com"},
		Subject:         "Safe Subject",
		TextBody:        "Hello world",
		MessageIDHeader: "<test@example.com>",
		HeadersJSON:     mustJSON(t, map[string]any{"_to": []string{"to@example.com"}}),
		RawMIME:         raw,
	}
	mime, err := BuildMIME(job)
	if err != nil {
		t.Fatalf("BuildMIME error: %v", err)
	}
	s := string(mime)

	// The raw MIME must NOT be returned verbatim.
	if s == string(raw) {
		t.Fatal("RawMIME was returned as-is — passthrough should be disabled")
	}
	// The structured From header must be used, not the one from raw MIME.
	if !strings.Contains(s, "From: sender@example.com") {
		t.Error("expected structured From header")
	}
	if strings.Contains(s, "evil@attacker.com") {
		t.Error("raw MIME From address leaked into output")
	}
	if strings.Contains(s, "leak@attacker.com") {
		t.Error("raw MIME Bcc address leaked into output")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return b
}
