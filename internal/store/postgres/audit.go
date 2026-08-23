package postgres

import (
	"context"
	"fmt"
	"time"

	"tabmail/internal/models"

	"github.com/google/uuid"
)

// ================================================================
// Audit
// ================================================================

func (s *PgStore) InsertAudit(ctx context.Context, e *models.AuditEntry) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	e.CreatedAt = time.Now()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (id,tenant_id,actor,action,resource_type,resource_id,details,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.TenantID, e.Actor, e.Action, e.ResourceType, e.ResourceID, e.Details, e.CreatedAt)
	return err
}

func (s *PgStore) ListAuditEntries(ctx context.Context, limit int) ([]*models.AuditEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,actor,action,resource_type,resource_id,details,created_at
		FROM audit_log
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.AuditEntry
	for rows.Next() {
		e := &models.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PgStore) ListAuditEntriesPaged(ctx context.Context, pg models.Page) ([]*models.AuditEntry, int, error) {
	pg = pg.Normalize()
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,tenant_id,actor,action,resource_type,resource_id,details,created_at
		FROM audit_log
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`, pg.PerPage, (pg.Page-1)*pg.PerPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.AuditEntry
	for rows.Next() {
		e := &models.AuditEntry{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.ResourceType, &e.ResourceID, &e.Details, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *PgStore) CreateMonitorEvent(ctx context.Context, e *models.MonitorEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO monitor_events (id,type,mailbox,message_id,sender,subject,size,at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.Type, e.Mailbox, e.MessageID, e.Sender, e.Subject, e.Size, e.At)
	return err
}

func (s *PgStore) ListMonitorEvents(ctx context.Context, pg models.Page, eventType, mailbox, sender string) ([]*models.MonitorEvent, int, error) {
	pg = pg.Normalize()
	filters := []any{}
	where := " WHERE 1=1"
	add := func(cond string, val any) {
		filters = append(filters, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(filters))
	}
	if eventType != "" {
		add("type =", eventType)
	}
	if mailbox != "" {
		add("mailbox ILIKE", "%"+mailbox+"%")
	}
	if sender != "" {
		add("sender ILIKE", "%"+sender+"%")
	}

	var total int
	countQuery := `SELECT count(*) FROM monitor_events` + where
	if err := s.pool.QueryRow(ctx, countQuery, filters...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(filters, pg.PerPage, pg.Offset())
	query := `SELECT id,type,mailbox,message_id,sender,subject,size,at FROM monitor_events` + where + fmt.Sprintf(" ORDER BY at DESC LIMIT $%d OFFSET $%d", len(filters)+1, len(filters)+2)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.MonitorEvent
	for rows.Next() {
		e := &models.MonitorEvent{}
		if err := rows.Scan(&e.ID, &e.Type, &e.Mailbox, &e.MessageID, &e.Sender, &e.Subject, &e.Size, &e.At); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
