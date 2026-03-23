package handlers

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

type auditStore interface {
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
}

func insertAudit(ctx context.Context, s auditStore, logger zerolog.Logger, entry models.AuditEntry) {
	if entry.Details == nil {
		entry.Details = json.RawMessage(`{}`)
	}
	if err := s.InsertAudit(ctx, &entry); err != nil {
		logger.Warn().Err(err).Msg("insert audit")
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}
