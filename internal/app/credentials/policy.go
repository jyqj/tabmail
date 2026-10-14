// Package credentials owns input policy shared by HTTP, application and storage
// boundaries. It has no persistence dependency: validation never grants access.
package credentials

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	MinPasswordBytes    = 12
	MaxPasswordBytes    = 72
	MinAuditReasonBytes = 8
	MaxAuditReasonBytes = 1000
	InvitationBytes     = 32
)

var (
	ErrPassword    = errors.New("password must be 12-72 bytes")
	ErrAuditReason = errors.New("audit reason must be 8-1000 bytes after trimming whitespace")
)

// ValidatePassword deliberately measures bytes, matching bcrypt's input limit.
// Passwords are never trimmed, normalized or silently truncated.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return ErrPassword
	}
	return nil
}

// AuditReason returns the exact normalized value to persist with the audit.
func AuditReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < MinAuditReasonBytes || len(reason) > MaxAuditReasonBytes {
		return "", ErrAuditReason
	}
	return reason, nil
}

// IssueInvitation returns a 256-bit opaque secret and its storage digest. Both
// invitation entry points use this issuer; their roles, expiry and consumption
// remain the responsibility of their existing transactional use cases.
func IssueInvitation() (raw, digest string, err error) {
	var buf [InvitationBytes]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf[:])
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}
