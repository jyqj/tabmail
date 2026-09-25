package postgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// sentContentFrom is the content authority. It never depends on the presence,
// owner or state of a delivery job. Purged items cannot be resurrected by a
// stale submission URL; trash items remain readable until their purge date.
const sentContentFrom = ` FROM sent_mail_assets s JOIN sent_mail_items i ON i.tenant_id=s.tenant_id AND i.asset_id=s.id AND i.mailbox_id=s.sender_mailbox_id AND (i.expires_at IS NULL OR i.expires_at>now()) AND (i.purge_after IS NULL OR i.purge_after>now()) `

func searchPattern(query string) (string, error) {
	query = strings.TrimSpace(query)
	if len(query) > 256 {
		return "", app.BadRequest("search query too long")
	}
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%", nil
}
func (s *PgStore) ListArchivedMail(ctx context.Context, a authz.Actor, mailbox uuid.UUID, folder, query string, page models.Page) ([]company.ArchivedMail, int, error) {
	out := []company.ArchivedMail{}
	total := 0
	page = page.Normalize()
	pattern, err := searchPattern(query)
	if err != nil {
		return nil, 0, err
	}
	filter := `i.deleted_at IS NULL AND i.archived_at IS NULL`
	switch folder {
	case "", "sent":
	case "archive":
		filter = `i.deleted_at IS NULL AND i.archived_at IS NOT NULL`
	case "trash":
		filter = `i.deleted_at IS NOT NULL`
	default:
		return nil, 0, app.BadRequest("invalid sent folder")
	}
	err = s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		access, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !access.CanRead {
			return app.Forbidden("mailbox read permission required")
		}
		where := ` WHERE s.tenant_id=$1 AND i.mailbox_id=$2 AND ` + filter + ` AND ($3='%%' OR s.search_text ILIKE $3)`
		if e = tx.QueryRow(ctx, `SELECT count(*)`+sentContentFrom+where, a.TenantID, mailbox, pattern).Scan(&total); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT s.id,i.mailbox_id,s.mail_from,s.to_addrs,s.subject,s.created_at,i.revision,
    (SELECT count(*) FROM sent_asset_attachments x WHERE x.tenant_id=s.tenant_id AND x.asset_id=s.id),
    EXISTS(SELECT 1 FROM outbound_jobs j WHERE j.tenant_id=s.tenant_id AND j.id=s.id)`+sentContentFrom+where+` ORDER BY s.created_at DESC,s.id DESC LIMIT $4 OFFSET $5`, a.TenantID, mailbox, pattern, page.PerPage, page.Offset())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v := company.ArchivedMail{}
			if e = rows.Scan(&v.ID, &v.MailboxID, &v.MailFrom, &v.To, &v.Subject, &v.CreatedAt, &v.Revision, &v.AttachmentCount, &v.DeliveryAvailable); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, total, err
}
func (s *PgStore) MutateArchivedMail(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID, revision int64, action string) error {
	if revision < 1 {
		return app.BadRequest("sent item revision required")
	}
	clause := ""
	switch action {
	case "trash":
		clause = `deleted_at=COALESCE(deleted_at,now()),purge_after=COALESCE(purge_after,now()+interval '30 days')`
	case "restore":
		clause = `deleted_at=NULL,purge_after=NULL`
	case "archive":
		clause = `archived_at=now()`
	case "unarchive":
		clause = `archived_at=NULL`
	default:
		return app.BadRequest("unsupported sent item action")
	}
	return s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		access, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !access.CanRead || !access.CanOrganize {
			return app.Forbidden("mailbox organize permission required")
		}
		tag, e := tx.Exec(ctx, `UPDATE sent_mail_items SET `+clause+`,revision=revision+1 WHERE tenant_id=$1 AND mailbox_id=$2 AND asset_id=$3 AND revision=$4 AND (expires_at IS NULL OR expires_at>now()) AND (purge_after IS NULL OR purge_after>now())`, a.TenantID, mailbox, id, revision)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("sent item changed or unavailable; reload")
		}
		if e = companyAudit(ctx, tx, a, "sent."+action, "sent_asset", id, map[string]any{"mailbox_id": mailbox, "revision": revision + 1}); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO mailbox_event_log(tenant_id,mailbox_id,event_type,message_id) VALUES($1,$2,'sent.changed',$3)`, a.TenantID, mailbox, id)
		return e
	})
}

var _ company.SentArchive = (*PgStore)(nil)
