package authn

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestIssueAndVerifyAccessToken(t *testing.T) {
	secret := "test-jwt-secret"
	user := &models.User{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Email:    "user@example.com",
		Role:     models.RoleUser,
	}

	token, err := IssueAccessToken(secret, user)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if !strings.Contains(token, ".") {
		t.Fatalf("expected signed token, got %q", token)
	}

	claims, err := VerifyAccessToken(secret, token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.UserID != user.ID || claims.TenantID != user.TenantID || claims.Email != user.Email {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerifyAccessTokenRejectsTamperedSignature(t *testing.T) {
	secret := "test-jwt-secret"
	user := &models.User{ID: uuid.New(), TenantID: uuid.New(), Email: "u@example.com", Role: models.RoleUser}
	token, err := IssueAccessToken(secret, user)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(token, ".", 2)
	tampered := parts[0] + ".deadbeef"
	if _, err := VerifyAccessToken(secret, tampered); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestTokenTTLConstants(t *testing.T) {
	if AccessTokenTTL <= 0 || RefreshTokenTTL <= AccessTokenTTL {
		t.Fatalf("unexpected token TTL constants: access=%s refresh=%s", AccessTokenTTL, RefreshTokenTTL)
	}
	if time.Minute*14 > AccessTokenTTL {
		t.Fatalf("access token TTL unexpectedly short")
	}
}

func TestGenerateRefreshTokenAndHash(t *testing.T) {
	raw, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || hash == "" {
		t.Fatal("expected non-empty refresh token and hash")
	}
	if HashToken(raw) != hash {
		t.Fatal("hash mismatch")
	}
	if HashToken("other") == hash {
		t.Fatal("hash should differ for different input")
	}
}
