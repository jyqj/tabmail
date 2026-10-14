package authn

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestIssueAndVerifyMailboxTokenRoundTrip(t *testing.T) {
	token, err := IssueMailboxToken("secret", "mb-1", " User@Example.COM ", time.Minute)
	if err != nil {
		t.Fatalf("IssueMailboxToken: %v", err)
	}
	claims, err := VerifyMailboxToken("secret", token)
	if err != nil {
		t.Fatalf("VerifyMailboxToken: %v", err)
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

func TestIssueMailboxTokenRejectsNonPositiveTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := IssueMailboxToken("secret", "mb-1", "a@b.c", ttl); err == nil {
			t.Fatalf("expected error for ttl=%s", ttl)
		}
	}
}

func TestVerifyMailboxTokenRejectsWrongSecret(t *testing.T) {
	token, err := IssueMailboxToken("secret-a", "mb-1", "a@b.c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMailboxToken("secret-b", token); err == nil {
		t.Fatal("expected signature verification to fail with wrong secret")
	}
}

func TestVerifyMailboxTokenRejectsTamperedPayload(t *testing.T) {
	token, err := IssueMailboxToken("secret", "mb-1", "a@b.c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(token, ".", 2)
	// Re-sign is impossible without the secret; just flip payload bytes.
	tampered := parts[0][:len(parts[0])-2] + "xx" + "." + parts[1]
	if _, err := VerifyMailboxToken("secret", tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestVerifyMailboxTokenRejectsMalformedTokens(t *testing.T) {
	for _, tok := range []string{"", "onlyonepart", "a.b.c", "!!!.sig"} {
		if _, err := VerifyMailboxToken("secret", tok); err == nil {
			t.Fatalf("expected error for token %q", tok)
		}
	}
}

func TestVerifyMailboxTokenRejectsExpiredToken(t *testing.T) {
	body, err := json.Marshal(MailboxClaims{
		MailboxID: "mb-1",
		Address:   "a@b.c",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	token := payload + "." + signHMAC("secret", payload)
	if _, err := VerifyMailboxToken("secret", token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

// Tokens issued by the former internal/mailtoken package used the identical
// two-segment payload.hexhmac format with the same claim field names; they
// must keep verifying (no forced re-issue, no UUID mailbox IDs).
func TestVerifyMailboxTokenAcceptsLegacyHandcraftedFormat(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"mailbox_id": "mb-1",
		"address":    "user@example.com",
		"exp":        time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	legacy := payload + "." + signHMAC("secret", payload)
	if strings.Count(legacy, ".") != 1 {
		t.Fatalf("legacy format must be two segments, got %q", legacy)
	}
	claims, err := VerifyMailboxToken("secret", legacy)
	if err != nil {
		t.Fatalf("legacy mailbox token rejected: %v", err)
	}
	if claims.MailboxID != "mb-1" || claims.Address != "user@example.com" {
		t.Fatalf("unexpected legacy claims: %+v", claims)
	}
}

// A mailbox token signed with the same secret must not be accepted as an
// access token, and vice versa.
func TestMailboxTokenAndAccessTokenMutuallyRejected(t *testing.T) {
	secret := "shared-secret"
	mailboxToken, err := IssueMailboxToken(secret, "mb-1", "a@b.c", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAccessToken(secret, mailboxToken); err != ErrInvalidToken {
		t.Fatalf("expected mailbox token rejected as access token with ErrInvalidToken, got %v", err)
	}

	user := &models.User{ID: uuid.New(), TenantID: uuid.New(), Email: "u@example.com", Role: models.RoleUser}
	accessToken, err := IssueAccessToken(secret, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMailboxToken(secret, accessToken); err != ErrInvalidToken {
		t.Fatalf("expected access token rejected as mailbox token with ErrInvalidToken, got %v", err)
	}
}
