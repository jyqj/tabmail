package messageapp

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

var errInvalidCursor = app.BadRequest("invalid cursor")

// EncodeCursor renders a message's list position as an opaque token clients
// pass back to continue past it. The token is base64url("unixnano:uuid").
func EncodeCursor(m *models.Message) string {
	if m == nil {
		return ""
	}
	raw := strconv.FormatInt(m.ReceivedAt.UnixNano(), 10) + ":" + m.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a token produced by EncodeCursor. Any malformed input
// reports as invalid rather than being partially interpreted.
func DecodeCursor(token string) (*models.MessageCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return nil, errInvalidCursor
	}
	nanosStr, idStr, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return nil, errInvalidCursor
	}
	nanos, err := strconv.ParseInt(nanosStr, 10, 64)
	if err != nil {
		return nil, errInvalidCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, errInvalidCursor
	}
	return &models.MessageCursor{ReceivedAt: time.Unix(0, nanos), ID: id}, nil
}
