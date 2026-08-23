package mailtoken

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	token, err := Issue("secret", "mb-1", " User@Example.COM ", time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := Verify("secret", token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.MailboxID != "mb-1" {
		t.Fatalf("unexpected mailbox id %q", claims.MailboxID)
	}
	if claims.Address != "user@example.com" {
		t.Fatalf("expected normalized address, got %q", claims.Address)
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expected future expiry, got %d", claims.ExpiresAt)
	}
}

func TestIssueRejectsNonPositiveTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := Issue("secret", "mb-1", "a@b.c", ttl); err == nil {
			t.Fatalf("expected error for ttl=%s", ttl)
		}
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token, err := Issue("secret-a", "mb-1", "a@b.c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("secret-b", token); err == nil {
		t.Fatal("expected signature verification to fail with wrong secret")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	token, err := Issue("secret", "mb-1", "a@b.c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(token, ".", 2)
	// Re-sign is impossible without the secret; just flip payload bytes.
	tampered := parts[0][:len(parts[0])-2] + "xx" + "." + parts[1]
	if _, err := Verify("secret", tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestVerifyRejectsMalformedTokens(t *testing.T) {
	for _, tok := range []string{"", "onlyonepart", "a.b.c", "!!!.sig"} {
		if _, err := Verify("secret", tok); err == nil {
			t.Fatalf("expected error for token %q", tok)
		}
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	body, err := json.Marshal(Claims{
		MailboxID: "mb-1",
		Address:   "a@b.c",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	token := payload + "." + sign("secret", payload)
	if _, err := Verify("secret", token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}
