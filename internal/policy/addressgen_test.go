package policy

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestGenerateSuggestedLocalPartProducesValidStructuredValue(t *testing.T) {
	now := time.Unix(1711272600, 0).UTC()
	local, err := generateSuggestedLocalPart(now, "mail.example.com", "secret-key", bytes.NewReader(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != addressRandomBodyLength+addressTimeCodeLength {
		t.Fatalf("unexpected local length: %d", len(local))
	}
	code := obfuscatedMinuteCode(now.Unix()/60, "mailbox|mail.example.com", "secret-key", addressTimeCodeLength)
	if strings.HasPrefix(local, code) || strings.HasSuffix(local, code) {
		t.Fatalf("time block should not be placed at the edges: %q", local)
	}
	if err := ValidateLocalPart(local); err != nil {
		t.Fatalf("generated local part should validate: %v", err)
	}
}

func TestObfuscatedMinuteCodeChangesAcrossMinutes(t *testing.T) {
	a := obfuscatedMinuteCode(123456, "mailbox|mail.example.com", "secret-key", addressTimeCodeLength)
	b := obfuscatedMinuteCode(123457, "mailbox|mail.example.com", "secret-key", addressTimeCodeLength)
	if a == b {
		t.Fatalf("expected minute code to change across buckets: %q", a)
	}
	if len(a) != addressTimeCodeLength || len(b) != addressTimeCodeLength {
		t.Fatalf("unexpected code length: %q / %q", a, b)
	}
}

func TestGenerateSuggestedSubdomainAddress(t *testing.T) {
	now := time.Unix(1711272600, 0).UTC()
	label, domain, err := GenerateSuggestedSubdomainAddress(now, "mail.example.com", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(domain, ".mail.example.com") {
		t.Fatalf("unexpected generated subdomain: %q", domain)
	}
	if !strings.HasPrefix(domain, label+".") {
		t.Fatalf("expected domain to start with generated label, got %q", domain)
	}
	if err := ValidateLocalPart(label); err != nil {
		t.Fatalf("generated subdomain label should validate as local-part-safe token: %v", err)
	}
}
