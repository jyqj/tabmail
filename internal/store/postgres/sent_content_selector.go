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
	"tabmail/internal/outbound"
)

// readSentContentTx is a narrow durable projection, not another read policy.
// Call only inside companyReadTx after current-member/profile fences. We first
// obtain the original mailbox ID (not a current address or queue source), fence
// its existing authorization row, then evaluate original sentContentFrom and
// submissionContentScope AFTER the potentially blocking mailbox wait. No
// historical submitter, admin role, job existence/state or snapshot marker can
// grant content. A complete snapshot never revives an expired/purged item.
func readSentContentTx(ctx context.Context, tx pgx.Tx, current authz.Actor, id uuid.UUID) (*company.SubmissionContent, error) {
	var mailbox uuid.UUID
	err := tx.QueryRow(ctx, `SELECT sender_mailbox_id FROM sent_mail_assets WHERE tenant_id=$1 AND id=$2`, current.TenantID, id).Scan(&mailbox)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, app.NotFound("submission not found")
	}
	if err != nil {
		return nil, err
	}
	if err = lockMailboxAuthorization(ctx, tx, current.TenantID, mailbox); err != nil {
		return nil, err
	}
	where, args := submissionContentScope(current, 2)
	v := &company.SubmissionContent{}
	var headers json.RawMessage
	const projection = `SELECT s.id,s.subject,s.mail_from,s.to_addrs,s.cc_addrs,s.bcc_addrs,
  s.recipient_completeness,s.headers_json,s.text_body,s.html_body,s.created_at`
	err = tx.QueryRow(ctx, projection+sentContentFrom+` WHERE `+where+` AND s.id=$1`, append([]any{id}, args...)...).Scan(
		&v.ID, &v.Subject, &v.MailFrom, &v.To, &v.CC, &v.BCC, &v.RecipientCompleteness, &headers, &v.TextBody, &v.HTMLBody, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, app.NotFound("submission not found")
	}
	if err != nil {
		return nil, err
	}
	v.To = append([]string{}, v.To...)
	v.CC = append([]string{}, v.CC...)
	if v.RecipientCompleteness == "complete" {
		v.BCC = append([]string{}, v.BCC...)
	} else {
		v.BCC = nil
	}
	v.Headers = outbound.SafeDisplayHeaders(headers)
	return v, nil
}
