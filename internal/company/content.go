package company

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"time"
)

// ParsedAttachment IDs bind immutable source bytes, part position and digest.
// Index remains for old clients; new clients must use ID, never the position.
type ParsedAttachment struct {
	ID          string `json:"id"`
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	Size        int    `json:"size"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"-"`
}
type ParsedMessage struct {
	MessageID     uuid.UUID          `json:"-"`
	SourceKey     string             `json:"-"`
	SourceSHA256  string             `json:"-"`
	ParserVersion int                `json:"parser_version"`
	TextBody      string             `json:"text_body"`
	HTMLBody      string             `json:"html_body"`
	BodyAccess    string             `json:"body_access"`
	Parts         []ParsedAttachment `json:"parts"`
	ThreadKey     string             `json:"-"`
}
type ContentIndexStatus struct {
	Total   int `json:"total"`
	Indexed int `json:"indexed"`
	Failed  int `json:"failed"`
}

// ParsedContentReader checks CURRENT rights and exact raw-source identity even
// on cache hits. It never accepts a storage key from an HTTP caller.
type ParsedContentReader interface {
	GetParsedMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*ParsedMessage, error)
	SaveParsedMessage(context.Context, authz.Actor, uuid.UUID, ParsedMessage) error
}

// MailIndexJob is an internal worker lease, not an employee DTO.
type MailIndexJob struct {
	TenantID   uuid.UUID
	MessageID  uuid.UUID
	SourceKey  string
	Token      uuid.UUID
	LeaseUntil time.Time
}
type ContentIndexer interface {
	ClaimMailIndexJobs(context.Context, int) ([]MailIndexJob, error)
	CompleteMailIndexJob(context.Context, MailIndexJob, ParsedMessage) error
	FailMailIndexJob(context.Context, MailIndexJob, string) error
}
