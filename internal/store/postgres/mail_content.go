package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

func (s *PgStore) GetParsedMessage(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*company.ParsedMessage, error) {
	var out *company.ParsedMessage
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		mb, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !mb.CanRead {
			return app.NotFound("message not found")
		}
		d := &company.ParsedMessage{MessageID: id}
		var parts []byte
		e = tx.QueryRow(ctx, `SELECT d.source_key,d.source_sha256,d.parser_version,d.text_body,d.html_body,d.body_access,d.parts,d.thread_key FROM mail_documents d JOIN messages m ON m.id=d.message_id AND m.tenant_id=d.tenant_id AND m.raw_object_key=d.source_key WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.id=$3 AND d.parser_version=1`, a.TenantID, mailbox, id).Scan(&d.SourceKey, &d.SourceSHA256, &d.ParserVersion, &d.TextBody, &d.HTMLBody, &d.BodyAccess, &parts, &d.ThreadKey)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if e = json.Unmarshal(parts, &d.Parts); e != nil {
			return e
		}
		out = d
		return nil
	})
	return out, e
}
func saveParsed(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, d company.ParsedMessage) error {
	if d.ParserVersion != 1 || len(d.SourceSHA256) != 64 || len(d.Parts) > 512 {
		return app.BadRequest("invalid parser document")
	}
	parts, e := json.Marshal(d.Parts)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `INSERT INTO mail_documents(tenant_id,message_id,source_key,source_sha256,parser_version,text_body,html_body,body_access,parts,thread_key,search_text)
 SELECT m.tenant_id,m.id,m.raw_object_key,$4,$5,$6,$7,$8,$9,$10,concat_ws(E'\n',m.subject,m.sender,array_to_string(m.recipients,','),$6::text)
 FROM messages m WHERE m.tenant_id=$1 AND m.id=$2 AND m.raw_object_key=$3
 ON CONFLICT(message_id) DO UPDATE SET source_key=EXCLUDED.source_key,source_sha256=EXCLUDED.source_sha256,parser_version=EXCLUDED.parser_version,text_body=EXCLUDED.text_body,html_body=EXCLUDED.html_body,body_access=EXCLUDED.body_access,parts=EXCLUDED.parts,thread_key=EXCLUDED.thread_key,search_text=EXCLUDED.search_text,indexed_at=now()`, tenant, d.MessageID, d.SourceKey, d.SourceSHA256, d.ParserVersion, d.TextBody, d.HTMLBody, d.BodyAccess, parts, d.ThreadKey)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return app.Conflict("message source changed")
	}
	return nil
}
func (s *PgStore) SaveParsedMessage(ctx context.Context, a authz.Actor, mailbox uuid.UUID, d company.ParsedMessage) error {
	return s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		mb, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !mb.CanRead {
			return app.NotFound("message not found")
		}
		var valid bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE tenant_id=$1 AND mailbox_id=$2 AND id=$3 AND raw_object_key=$4)`, a.TenantID, mailbox, d.MessageID, d.SourceKey).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return app.NotFound("message not found")
		}
		return saveParsed(ctx, tx, a.TenantID, d)
	})
}
func (s *PgStore) ContentIndexStatus(ctx context.Context, a authz.Actor, mailbox uuid.UUID) (*company.ContentIndexStatus, error) {
	out := &company.ContentIndexStatus{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		mb, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !mb.CanRead {
			return app.Forbidden("mailbox read permission required")
		}
		return tx.QueryRow(ctx, `SELECT count(*),count(d.message_id),count(*) FILTER(WHERE j.state='failed') FROM messages m LEFT JOIN mail_documents d ON d.tenant_id=m.tenant_id AND d.message_id=m.id AND d.source_key=m.raw_object_key AND d.parser_version=1 LEFT JOIN mail_index_jobs j ON j.tenant_id=m.tenant_id AND j.message_id=m.id WHERE m.tenant_id=$1 AND m.mailbox_id=$2 AND m.deleted_at IS NULL`, a.TenantID, mailbox).Scan(&out.Total, &out.Indexed, &out.Failed)
	})
	return out, e
}
func (s *PgStore) ClaimMailIndexJobs(ctx context.Context, limit int) ([]company.MailIndexJob, error) {
	if limit < 1 || limit > 20 {
		limit = 5
	}
	rows, e := s.pool.Query(ctx, `WITH picked AS(SELECT message_id FROM mail_index_jobs WHERE (state='pending' AND next_attempt_at<=now()) OR (state='processing' AND lease_until<now()) ORDER BY next_attempt_at,message_id LIMIT $1 FOR UPDATE SKIP LOCKED)
 UPDATE mail_index_jobs j SET state='processing',attempts=attempts+1,lease_token=gen_random_uuid(),lease_until=now()+interval '90 seconds' FROM picked p WHERE j.message_id=p.message_id RETURNING j.tenant_id,j.message_id,j.source_key,j.lease_token,j.lease_until`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []company.MailIndexJob{}
	for rows.Next() {
		v := company.MailIndexJob{}
		if e = rows.Scan(&v.TenantID, &v.MessageID, &v.SourceKey, &v.Token, &v.LeaseUntil); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *PgStore) CompleteMailIndexJob(ctx context.Context, j company.MailIndexJob, d company.ParsedMessage) error {
	if d.MessageID != j.MessageID || d.SourceKey != j.SourceKey {
		return app.Conflict("index provenance changed")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE mail_index_jobs SET state='ready',lease_until=NULL,lease_token=NULL,last_error='' WHERE tenant_id=$1 AND message_id=$2 AND source_key=$3 AND lease_token=$4 AND state='processing' AND lease_until>now()`, j.TenantID, j.MessageID, j.SourceKey, j.Token)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return app.Conflict("index lease lost")
	}
	if e = saveParsed(ctx, tx, j.TenantID, d); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PgStore) FailMailIndexJob(ctx context.Context, j company.MailIndexJob, reason string) error {
	// Diagnostics are deliberately coarse: a malicious MIME header must never
	// copy employee content into operational status or logs.
	_, e := s.pool.Exec(ctx, `UPDATE mail_index_jobs SET state=CASE WHEN attempts>=5 THEN 'failed' ELSE 'pending' END,next_attempt_at=now()+interval '1 minute',lease_until=NULL,lease_token=NULL,last_error='content parsing failed' WHERE tenant_id=$1 AND message_id=$2 AND lease_token=$3`, j.TenantID, j.MessageID, j.Token)
	return e
}

var _ company.ParsedContentReader = (*PgStore)(nil)
var _ company.ContentIndexer = (*PgStore)(nil)
