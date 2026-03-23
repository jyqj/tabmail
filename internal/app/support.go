package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

type AuditStore interface {
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
}

func InsertAudit(ctx context.Context, s AuditStore, logger zerolog.Logger, entry models.AuditEntry) {
	if s == nil {
		return
	}
	if entry.Details == nil {
		entry.Details = json.RawMessage(`{}`)
	}
	if err := s.InsertAudit(ctx, &entry); err != nil {
		logger.Warn().Err(err).Msg("insert audit")
	}
}

func MustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func UUIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}
