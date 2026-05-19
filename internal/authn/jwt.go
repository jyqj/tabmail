package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
)

// AccessClaims represents the payload of an access token.
type AccessClaims struct {
	UserID   uuid.UUID       `json:"uid"`
	TenantID uuid.UUID       `json:"tid"`
	Role     models.UserRole `json:"role"`
	Email    string          `json:"email"`
	IssuedAt int64           `json:"iat"`
	Exp      int64           `json:"exp"`
}

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// IssueAccessToken creates a signed access token for the given user.
func IssueAccessToken(secret string, user *models.User) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		UserID:   user.ID,
		TenantID: user.TenantID,
		Role:     user.Role,
		Email:    user.Email,
		IssuedAt: now.Unix(),
		Exp:      now.Add(AccessTokenTTL).Unix(),
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + signHMAC(secret, payload), nil
}

// VerifyAccessToken validates and parses an access token.
func VerifyAccessToken(secret, token string) (*AccessClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidToken
	}
	payload, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(signHMAC(secret, payload)), []byte(sig)) {
		return nil, ErrInvalidToken
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims AccessClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.Exp <= time.Now().Unix() {
		return nil, ErrTokenExpired
	}
	return &claims, nil
}

// GenerateRefreshToken creates a random refresh token and returns (raw, hash).
func GenerateRefreshToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 hex digest of a token string.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func signHMAC(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
