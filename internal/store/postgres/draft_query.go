package postgres

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func (s *PgStore) ListMailDraftPage(ctx context.Context, a authz.Actor, p models.Page) ([]company.Draft, int, error) {
	p = p.Normalize()
	out := []company.Draft{}
	total := 0
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		// Resolve only distinct mailbox IDs, never the draft payload, before ACLs.
		rows, e := tx.Query(ctx, `SELECT DISTINCT mailbox_id FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL`, a.TenantID, a.ID)
		if e != nil {
			return e
		}
		ids := []uuid.UUID{}
		for rows.Next() {
			var id uuid.UUID
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		access, e := s.mailboxAccessBatch(ctx, tx, a, ids)
		if e != nil {
			return e
		}
		allowed := []uuid.UUID{}
		for _, id := range ids {
			if r := access[id]; r != nil && r.CanSend {
				allowed = append(allowed, id)
			}
		}
		const where = `tenant_id=$1 AND user_id=$2 AND sealed_at IS NULL AND mailbox_id=ANY($3::uuid[])`
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE `+where, a.TenantID, a.ID, allowed).Scan(&total); e != nil {
			return e
		}
		rows, e = tx.Query(ctx, `SELECT id,mailbox_id,payload,revision,updated_at FROM mail_drafts WHERE `+where+` ORDER BY updated_at DESC,id DESC LIMIT $4 OFFSET $5`, a.TenantID, a.ID, allowed, p.PerPage, p.Offset())
		if e != nil {
			return e
		}
		for rows.Next() {
			var d company.Draft
			var raw []byte
			if e = rows.Scan(&d.ID, &d.MailboxID, &raw, &d.Revision, &d.UpdatedAt); e != nil {
				rows.Close()
				return e
			}
			if e = json.Unmarshal(raw, &d.Payload); e != nil {
				rows.Close()
				return e
			}
			out = append(out, d)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		return s.fillDraftTemplateVersions(ctx, tx, a, out)
	})
	return out, total, e
}
