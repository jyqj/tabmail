package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type AuditStore interface {
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
}

// InsertAuditRequired persists an audit entry and returns failures to the
// caller. Security-sensitive reads and transactional mutations use this path so
// an operation cannot report success without its audit obligation.
func InsertAuditRequired(ctx context.Context, s AuditStore, entry models.AuditEntry) error {
	if s == nil {
		return errors.New("audit store is required")
	}
	if entry.Details == nil {
		entry.Details = json.RawMessage(`{}`)
	}
	if err := s.InsertAudit(ctx, &entry); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

// InsertAudit is retained for best-effort operational telemetry. Asset
// mutations and break-glass reads must use InsertAuditRequired instead.
func InsertAudit(ctx context.Context, s AuditStore, logger zerolog.Logger, entry models.AuditEntry) {
	if err := InsertAuditRequired(ctx, s, entry); err != nil {
		logger.Warn().Err(err).Msg("insert audit")
	}
}

// WithinTransaction is the application-facing Unit of Work helper. It keeps
// infrastructure errors behind the stable app error envelope while preserving
// deliberate application errors returned by the mutation callback.
func WithinTransaction(ctx context.Context, tx store.Transactor, fn func(context.Context) error) error {
	if tx == nil {
		return Internal(errors.New("transaction boundary is required"))
	}
	if fn == nil {
		return Internal(errors.New("transaction callback is required"))
	}
	if err := tx.WithinTx(ctx, fn); err != nil {
		if _, ok := As(err); ok {
			return err
		}
		return Internal(err)
	}
	return nil
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

// EnsureTenantScope validates that a tenant context exists and is usable.
func EnsureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	if tenant == nil {
		return Forbidden("no tenant context")
	}
	if isAdmin && tenant.ID == uuid.Nil {
		return BadRequest("admin requests to tenant-scoped endpoints must include X-Tenant-ID")
	}
	return nil
}
