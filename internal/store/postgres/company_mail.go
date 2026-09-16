package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
)

const workMessageSelect = `SELECT id,tenant_id,mailbox_id,zone_id,sender,recipients,subject,size,seen,raw_object_key,headers_json,received_at,expires_at,otp_code,otp_confidence,deleted_at,purge_after,archived_at FROM messages`

func scanWorkMessage(row pgx.Row) (*models.Message, error) {
	m := &models.Message{}
	e := row.Scan(&m.ID, &m.TenantID, &m.MailboxID, &m.ZoneID, &m.Sender, &m.Recipients, &m.Subject, &m.Size, &m.Seen, &m.RawObjectKey, &m.HeadersJSON, &m.ReceivedAt, &m.ExpiresAt, &m.OTPCode, &m.OTPConfidence, &m.DeletedAt, &m.PurgeAfter, &m.ArchivedAt)
	return m, e
}
func (s *PgStore) ListWorkMessages(ctx context.Context, a authz.Actor, id uuid.UUID, folder, query string, page models.Page) ([]*models.Message, int, error) {
	page = page.Normalize()
	query = strings.TrimSpace(query)
	if len(query) > 256 {
		return nil, 0, app.BadRequest("search query too long")
	}
	where := `deleted_at IS NULL AND archived_at IS NULL`
	switch folder {
	case "", "inbox":
	case "archive":
		where = `deleted_at IS NULL AND archived_at IS NOT NULL`
	case "trash":
		where = `deleted_at IS NOT NULL`
	case "all":
		where = `deleted_at IS NULL`
	default:
		return nil, 0, app.BadRequest("invalid folder")
	}
	// LIKE metacharacters are escaped: the search box accepts literal text.
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%"
	out := []*models.Message{}
	total := 0
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		v, e := s.mailboxAccessTx(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if !v.CanRead {
			return app.Forbidden("mailbox read permission required")
		}
		filter := `tenant_id=$1 AND mailbox_id=$2 AND ` + where + ` AND ($3='%%' OR subject ILIKE $3 OR sender ILIKE $3 OR array_to_string(recipients,',') ILIKE $3)`
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE `+filter, a.TenantID, id, pattern).Scan(&total); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, workMessageSelect+` WHERE `+filter+` ORDER BY received_at DESC,id DESC LIMIT $4 OFFSET $5`, a.TenantID, id, pattern, page.PerPage, page.Offset())
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
func messageMutation(ctx context.Context, tx pgx.Tx, tenant, mailbox, id uuid.UUID, action string) error {
	var sql string
	switch action {
	case "trash":
		sql = `UPDATE messages SET deleted_at=COALESCE(deleted_at,now()),purge_after=COALESCE(purge_after,now()+interval '30 days') WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3`
	case "restore":
		sql = `UPDATE messages SET deleted_at=NULL,purge_after=NULL,expires_at=NULL WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3 AND deleted_at IS NOT NULL`
	case "archive":
		sql = `UPDATE messages SET archived_at=now() WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3 AND deleted_at IS NULL`
	case "unarchive":
		sql = `UPDATE messages SET archived_at=NULL WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3 AND deleted_at IS NULL`
	case "seen":
		sql = `UPDATE messages SET seen=true WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3`
	case "unseen":
		sql = `UPDATE messages SET seen=false WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3`
	default:
		return app.BadRequest("unsupported message action")
	}
	tag, e := tx.Exec(ctx, sql, tenant, mailbox, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return app.NotFound("message unavailable for this action")
	}
	return nil
}
func messageOutbox(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, address, action string) error {
	eventType := "message." + action
	raw, e := json.Marshal(hooks.Event{Type: eventType, TenantID: tenant.String(), Mailbox: address, MessageID: id.String(), OccurredAt: time.Now().UTC()})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO outbox_events(id,event_type,payload) VALUES($1,$2,$3)`, uuid.New(), eventType, raw)
	return e
}
func (s *PgStore) MutateWorkMessage(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID, action string) error {
	return s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		v, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !v.CanRead || !v.CanOrganize {
			return app.Forbidden("mailbox organize permission required")
		}
		if e = messageMutation(ctx, tx, a.TenantID, mailbox, id, action); e != nil {
			return e
		}
		if e = companyAudit(ctx, tx, a, "message."+action, "message", id, map[string]any{"mailbox_id": mailbox}); e != nil {
			return e
		}
		return messageOutbox(ctx, tx, a.TenantID, id, v.Mailbox.FullAddress, action)
	})
}

// Legacy company-mail DELETE routes use this seam after their resource check.
// Preserve the raw object and durable ingress tombstone; audit/outbox commit
// with the trash transition, not best-effort after physical deletion.
func (s *PgStore) TrashCompanyMessages(ctx context.Context, tenant, mailbox uuid.UUID, id *uuid.UUID, actor string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockMemberTenant(ctx, tx, tenant); e != nil {
		return e
	}
	var address string
	if e = tx.QueryRow(ctx, `SELECT full_address FROM mailboxes WHERE id=$1 AND tenant_id=$2 AND (mailbox_kind<>'legacy' OR owner_user_id IS NOT NULL) FOR UPDATE`, mailbox, tenant).Scan(&address); e != nil {
		return e
	}
	if id != nil {
		e = messageMutation(ctx, tx, tenant, mailbox, *id, "trash")
	} else {
		_, e = tx.Exec(ctx, `UPDATE messages SET deleted_at=now(),purge_after=now()+interval '30 days' WHERE tenant_id=$1 AND mailbox_id=$2 AND deleted_at IS NULL`, tenant, mailbox)
	}
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(map[string]any{"mailbox_id": mailbox, "message_id": id, "recovery_days": 30})
	if _, e = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,'message.trash','mailbox',$3,$4)`, tenant, actor, mailbox, raw); e != nil {
		return e
	}
	messageID := uuid.Nil
	if id != nil {
		messageID = *id
	}
	if e = messageOutbox(ctx, tx, tenant, messageID, address, "trashed"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PgStore) ListMailboxEvents(ctx context.Context, tenant, mailbox uuid.UUID, cursor int64, limit int) ([]company.MailEvent, int64, error) {
	if cursor < 0 {
		var latest int64
		e := s.pool.QueryRow(ctx, `SELECT COALESCE(max(sequence),0) FROM mailbox_event_log WHERE tenant_id=$1 AND mailbox_id=$2`, tenant, mailbox).Scan(&latest)
		return []company.MailEvent{}, latest, e
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	out := []company.MailEvent{}
	rows, e := s.pool.Query(ctx, `SELECT sequence,event_type,message_id FROM mailbox_event_log WHERE tenant_id=$1 AND mailbox_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, tenant, mailbox, cursor, limit)
	if e != nil {
		return nil, cursor, e
	}
	defer rows.Close()
	for rows.Next() {
		v := company.MailEvent{}
		if e = rows.Scan(&v.Sequence, &v.Type, &v.MessageID); e != nil {
			return nil, cursor, e
		}
		out = append(out, v)
		cursor = v.Sequence
	}
	return out, cursor, rows.Err()
}

func (s *PgStore) ListMailDrafts(ctx context.Context, a authz.Actor) ([]company.Draft, error) {
	out := []company.Draft{}
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, `SELECT id,mailbox_id,payload,revision,updated_at FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 ORDER BY updated_at DESC LIMIT 200`, a.TenantID, a.ID)
		if e != nil {
			return e
		}
		for rows.Next() {
			v := company.Draft{}
			var raw []byte
			if e = rows.Scan(&v.ID, &v.MailboxID, &raw, &v.Revision, &v.UpdatedAt); e != nil {
				rows.Close()
				return e
			}
			if e = json.Unmarshal(raw, &v.Payload); e != nil {
				rows.Close()
				return e
			}
			out = append(out, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		filtered := []company.Draft{}
		for _, v := range out {
			rights, e := s.mailboxAccessTx(ctx, tx, a, v.MailboxID)
			if e != nil {
				var ae *app.Error
				if errors.As(e, &ae) && ae.Kind == app.KindNotFound {
					continue
				}
				return e
			}
			if rights.CanSend {
				filtered = append(filtered, v)
			}
		}
		out = filtered
		return nil
	})
	return out, e
}
func (s *PgStore) SaveMailDraft(ctx context.Context, a authz.Actor, v company.Draft) (*company.Draft, error) {
	raw, e := json.Marshal(v.Payload)
	if e != nil {
		return nil, e
	}
	if len(raw) > 2*1024*1024 || len(v.Payload.AttachmentIDs) > 10 {
		return nil, app.BadRequest("draft too large")
	}
	e = s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		rights, e := s.mailboxAccessTx(ctx, tx, a, v.MailboxID)
		if e != nil {
			return e
		}
		if !rights.CanSend {
			return app.Forbidden("mailbox send permission required")
		}
		if e = validateAttachmentIDs(ctx, tx, a.TenantID, a.ID, v.MailboxID, v.Payload.AttachmentIDs); e != nil {
			return e
		}
		if v.ID == uuid.Nil {
			if v.Revision != 0 {
				return app.Conflict("new draft revision must be zero")
			}
			v.ID = uuid.New()
			return tx.QueryRow(ctx, `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload) VALUES($1,$2,$3,$4,$5) RETURNING revision,updated_at`, v.ID, a.TenantID, a.ID, v.MailboxID, raw).Scan(&v.Revision, &v.UpdatedAt)
		}
		e = tx.QueryRow(ctx, `UPDATE mail_drafts SET mailbox_id=$4,payload=$5,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND user_id=$2 AND id=$3 AND revision=$6 RETURNING revision,updated_at`, a.TenantID, a.ID, v.ID, v.MailboxID, raw, v.Revision).Scan(&v.Revision, &v.UpdatedAt)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.Conflict("draft changed in another tab")
		}
		return e
	})
	return &v, e
}
func (s *PgStore) DeleteMailDraft(ctx context.Context, a authz.Actor, id uuid.UUID, revision int) error {
	return s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		tag, e := tx.Exec(ctx, `DELETE FROM mail_drafts WHERE tenant_id=$1 AND user_id=$2 AND id=$3 AND revision=$4`, a.TenantID, a.ID, id, revision)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("draft changed or unavailable")
		}
		return nil
	})
}
func validateAttachmentIDs(ctx context.Context, tx pgx.Tx, tenant, user, mailbox uuid.UUID, ids []uuid.UUID) error {
	if len(ids) > 10 {
		return app.BadRequest("at most ten attachments")
	}
	var size int64
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return app.BadRequest("duplicate attachment")
		}
		seen[id] = true
		var n int64
		e := tx.QueryRow(ctx, `SELECT size FROM mail_attachments WHERE id=$1 AND tenant_id=$2 AND user_id=$3 AND mailbox_id=$4 AND state='ready' AND (expires_at>now() OR EXISTS(SELECT 1 FROM mail_drafts d WHERE d.tenant_id=$2 AND d.user_id=$3 AND d.payload->'attachment_ids' ? $5)) FOR SHARE`, id, tenant, user, mailbox, id.String()).Scan(&n)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.Forbidden("attachment unavailable for this sender and mailbox")
		}
		if e != nil {
			return e
		}
		size += n
	}
	if size > 20*1024*1024 {
		return app.BadRequest("total attachments exceed 20 MiB")
	}
	return nil
}
func (s *PgStore) ReserveMailAttachment(ctx context.Context, a authz.Actor, v company.Attachment) (*company.Attachment, error) {
	if len(v.Filename) < 1 || len(v.Filename) > 200 || strings.ContainsAny(v.Filename, "\r\n/\\\x00") || v.Size < 0 || v.Size > 20*1024*1024 {
		return nil, app.BadRequest("invalid attachment name or size")
	}
	v.ID = uuid.New()
	v.UserID = a.ID
	v.ObjectKey = "attachment-" + v.ID.String()
	v.State = "uploading"
	v.ContentType = "application/octet-stream"
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		rights, e := s.mailboxAccessTx(ctx, tx, a, v.MailboxID)
		if e != nil {
			return e
		}
		if !rights.CanSend {
			return app.Forbidden("mailbox send permission required")
		}
		var bytes int64
		e = tx.QueryRow(ctx, `SELECT COALESCE(sum(size),0) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2 AND created_at>now()-interval '1 day'`, a.TenantID, a.ID).Scan(&bytes)
		if e != nil {
			return e
		}
		if bytes+v.Size > 200*1024*1024 {
			return app.Conflict("daily attachment upload budget exceeded")
		}
		_, e = tx.Exec(ctx, `INSERT INTO mail_attachments(id,tenant_id,mailbox_id,user_id,object_key,filename,content_type,size) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, a.TenantID, v.MailboxID, a.ID, v.ObjectKey, v.Filename, v.ContentType, v.Size)
		return e
	})
	return &v, e
}
func (s *PgStore) FinishMailAttachment(ctx context.Context, a authz.Actor, id uuid.UUID, sha string) error {
	if len(sha) != 64 {
		return app.BadRequest("invalid checksum")
	}
	return s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		var mailbox uuid.UUID
		e := tx.QueryRow(ctx, `SELECT mailbox_id FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2 AND id=$3 AND state='uploading'`, a.TenantID, a.ID, id).Scan(&mailbox)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("upload not found")
		}
		if e != nil {
			return e
		}
		rights, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !rights.CanSend {
			return app.Forbidden("mailbox sending authority revoked during upload")
		}
		_, e = tx.Exec(ctx, `UPDATE mail_attachments SET state='ready',sha256=$4 WHERE tenant_id=$1 AND user_id=$2 AND id=$3`, a.TenantID, a.ID, id, sha)
		return e
	})
}
func (s *PgStore) GetWorkAttachment(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.Attachment, error) {
	v := &company.Attachment{}
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		e := tx.QueryRow(ctx, `SELECT id,mailbox_id,user_id,object_key,filename,content_type,size,sha256,state FROM mail_attachments WHERE tenant_id=$1 AND id=$2 AND state='ready'`, a.TenantID, id).Scan(&v.ID, &v.MailboxID, &v.UserID, &v.ObjectKey, &v.Filename, &v.ContentType, &v.Size, &v.SHA256, &v.State)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("attachment not found")
		}
		if e != nil {
			return e
		}
		rights, e := s.mailboxAccessTx(ctx, tx, a, v.MailboxID)
		if e != nil {
			return e
		}
		if v.UserID == a.ID && rights.CanSend {
			return nil
		}
		var sent bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbound_attachments WHERE tenant_id=$1 AND attachment_id=$2)`, a.TenantID, id).Scan(&sent); e != nil {
			return e
		}
		if !sent || !rights.CanRead {
			return app.Forbidden("attachment read permission required")
		}
		return nil
	})
	return v, e
}

// Worker-only attachment lookup. Queue ownership/permission is checked by the
// delivery service, and the pinned job relation keeps objects alive on retry.
func (s *PgStore) OutboundAttachments(ctx context.Context, job uuid.UUID) ([]company.Attachment, error) {
	out := []company.Attachment{}
	rows, e := s.pool.Query(ctx, `SELECT a.id,a.mailbox_id,a.user_id,a.object_key,a.filename,a.content_type,a.size,a.sha256,a.state FROM mail_attachments a JOIN outbound_attachments x ON x.attachment_id=a.id AND x.tenant_id=a.tenant_id WHERE x.job_id=$1 ORDER BY a.id`, job)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		a := company.Attachment{}
		if e = rows.Scan(&a.ID, &a.MailboxID, &a.UserID, &a.ObjectKey, &a.Filename, &a.ContentType, &a.Size, &a.SHA256, &a.State); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
