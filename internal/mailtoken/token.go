package mailtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Claims struct {
	MailboxID string `json:"mailbox_id"`
	Address   string `json:"address"`
	ExpiresAt int64  `json:"exp"`
}

func Issue(secret, mailboxID, address string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("ttl must be positive")
	}
	claims := Claims{
		MailboxID: mailboxID,
		Address:   strings.ToLower(strings.TrimSpace(address)),
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + sign(secret, payload), nil
}

func Verify(secret, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid token format")
	}
	payload, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(sign(secret, payload)), []byte(sig)) {
		return nil, errors.New("invalid token signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	var claims Claims
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, errors.New("invalid token claims")
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return nil, errors.New("token expired")
	}
	claims.Address = strings.ToLower(strings.TrimSpace(claims.Address))
	return &claims, nil
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
