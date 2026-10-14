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
		if e := s.authorizeReceivedMailboxTx(ctx, tx, a, mailbox); e != nil {
			return e
		}
		var root string
		e := tx.QueryRow(ctx, `SELECT COALESCE(d.thread_key,'') FROM messages m LEFT JOIN mail_documents d ON d.tenant_id=m.tenant_id AND d.message_id=m.id AND d.source_key=m.raw_object_key AND d.parser_version=1 WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.id=$3 AND `+receivedContentEligible, a.TenantID, mailbox, message).Scan(&root)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("message not found")
		}
		if e != nil {
			return e
		}
		filter := `m.tenant_id=$1 AND m.mailbox_id=$2 AND m.deleted_at IS NULL AND ` + receivedContentEligible + ` AND (m.id=$3 OR ($4<>'' AND EXISTS(SELECT 1 FROM mail_documents d WHERE d.tenant_id=m.tenant_id AND d.message_id=m.id AND d.source_key=m.raw_object_key AND d.parser_version=1 AND d.thread_key=$4)))`
		out, total, e = readReceivedPageTx(ctx, tx, filter, "$7", "$5", "$6", "ASC", a.TenantID, mailbox, message, root, p.PerPage, p.Offset(), a.ID)
		if e != nil {
			return e
		}
		// The root is the authority for this navigation request. A member
		// query may have waited past its deadline; never return other members
		// from an unavailable root, even when those members remain eligible.
		return requireReceivedContentTx(ctx, tx, a.TenantID, mailbox, message)
	})
	if e != nil {
		return nil, 0, e
	}
	return out, total, nil
}
