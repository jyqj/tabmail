package authn

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// MailboxClaims represents the payload of a mailbox-scoped token. The format
// predates this package (formerly internal/mailtoken): base64url JSON followed
// by a hexadecimal HMAC, "payload.hexhmac", using the access-token HMAC-SHA256 scheme,
// not a standard three-part JWT.
type MailboxClaims struct {
	MailboxID string `json:"mailbox_id"`
	Address   string `json:"address"`
	ExpiresAt int64  `json:"exp"`
}

// IssueMailboxToken creates a signed mailbox token. Mailbox IDs keep their
// historical string form (legacy "mb-1" fixture IDs remain valid); no UUID
// conversion is applied.
func IssueMailboxToken(secret, mailboxID, address string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("ttl must be positive")
	}
	claims := MailboxClaims{
		MailboxID: mailboxID,
		Address:   strings.ToLower(strings.TrimSpace(address)),
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}
	return encodeSignedToken(secret, claims)
}

// VerifyMailboxToken validates and parses a mailbox token. The mailbox_id
// identity claim is mandatory, so an access token signed with the same secret
// is rejected here; VerifyAccessToken symmetrically rejects mailbox tokens
// via its user identity claim.
func VerifyMailboxToken(secret, token string) (*MailboxClaims, error) {
	body, err := decodeSignedToken(secret, token)
	if err != nil {
		return nil, err
	}
	var claims MailboxClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.MailboxID == "" {
		return nil, ErrInvalidToken
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return nil, ErrTokenExpired
	}
	claims.Address = strings.ToLower(strings.TrimSpace(claims.Address))
	return &claims, nil
}
