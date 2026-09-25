package postgres

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// RFC reference chains are navigation hints only, scoped to one readable
// mailbox. A spoofed Message-ID never grants access to another mailbox.
func (s *PgStore) ListMessageConversation(ctx context.Context, a authz.Actor, mailbox, message uuid.UUID, p models.Page) ([]*models.Message, int, error) {
	p = p.Normalize()
	out := []*models.Message{}
	total := 0
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		rights, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !rights.CanRead {
			return app.Forbidden("mailbox read permission required")
		}
		var root string
		e = tx.QueryRow(ctx, `SELECT COALESCE(d.thread_key,'') FROM messages m LEFT JOIN mail_documents d ON d.tenant_id=m.tenant_id AND d.message_id=m.id AND d.source_key=m.raw_object_key AND d.parser_version=1 WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.id=$3`, a.TenantID, mailbox, message).Scan(&root)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("message not found")
		}
		if e != nil {
			return e
		}
		const filter = `m.tenant_id=$1 AND m.mailbox_id=$2 AND m.deleted_at IS NULL AND (m.id=$3 OR ($4<>'' AND EXISTS(SELECT 1 FROM mail_documents d WHERE d.tenant_id=m.tenant_id AND d.message_id=m.id AND d.source_key=m.raw_object_key AND d.parser_version=1 AND d.thread_key=$4)))`
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM messages m WHERE `+filter, a.TenantID, mailbox, message, root).Scan(&total); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, workMessageSelect("$7")+` WHERE `+filter+` ORDER BY m.received_at,m.id LIMIT $5 OFFSET $6`, a.TenantID, mailbox, message, root, p.PerPage, p.Offset(), a.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			m, e := scanWorkMessage(rows)
			if e != nil {
				return e
			}
			m.RawObjectKey = ""
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, total, e
}
