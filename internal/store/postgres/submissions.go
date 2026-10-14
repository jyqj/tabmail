package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The employee submission view projects outbound jobs the actor may see:
// submissions they sent (the same owner dimensions the outbound list uses)
// plus submissions sent from any mailbox they hold a current read right on.
// Administrative roles confer no bypass — the management view of delivery
// internals stays in the recovery surface.

// Read only ordinary-operation metadata. No content/address columns are even
// selected for the public company receipt scan.
const submissionSelect = `SELECT s.id,s.tenant_id,s.state,s.created_at,s.updated_at,
 s.attempts,s.next_attempt_at,s.in_flight_domain<>'',s.recipient_ledger FROM outbound_jobs s`

func scanSubmission(row pgx.Row) (*models.OutboundJob, error) {
	job := &models.OutboundJob{}
	var inFlight bool
	if err := row.Scan(&job.ID, &job.TenantID, &job.State, &job.CreatedAt, &job.UpdatedAt, &job.Attempts, &job.NextAttemptAt, &inFlight, &job.RecipientLedger); err != nil {
		return nil, err
	}
	// Only presence affects outcome; the private recipient-domain value is never
	// read or exposed on an ordinary receipt.
	if inFlight {
		job.InFlightDomain = "present"
	}
	return job, nil
}

// submissionScope builds the tenant-isolated visibility predicate for the
// actor. It mirrors ListOutboundJobsScoped's owner rule (user_id for members,
// api_key_id for keys) extended by readable-mailbox provenance, plus the
// zone allowlist. argBase names the first free placeholder so callers can
// prepend their own arguments.
func submissionScope(a authz.Actor, argBase int) (string, []any) {
	return submissionScopeFor(a, argBase, true)
}

func submissionContentScope(a authz.Actor, argBase int) (string, []any) {
	return submissionScopeFor(a, argBase, false)
}

// allowSubmitter grants an operation receipt, NEVER a body or attachment.
// Both predicates keep independent tenant and zone constraints. Missing,
// expired or no-longer-readable mailboxes fail closed for sent content.
func submissionScopeFor(a authz.Actor, argBase int, allowSubmitter bool) (string, []any) {
	// The tenant conjunct is independent and must survive every branch below:
	// replacing it (instead of appending) dropped the top-level tenant bound
	// and let rows from other tenants that reference this principal's user or
	// mailbox ids leak into the listing.
	where := []string{"s.tenant_id=$" + strconv.Itoa(argBase)}
	args := []any{a.TenantID}
	n := argBase
	uid := a.EffectiveUserID()
	if uid != nil {
		n++
		u := "$" + strconv.Itoa(n)
		args = append(args, *uid)
		readable := `s.sender_mailbox_id IN (SELECT m.id FROM mailboxes m WHERE ` + readableMailboxPredicate(argBase, n) + `)`
		if allowSubmitter {
			where = append(where, `(s.user_id=`+u+` OR s.sender_user_id=`+u+` OR `+readable+`)`)
		} else {
			where = append(where, readable)
		}
	} else if allowSubmitter && a.Type == authz.PrincipalAPIKey {
		n++
		where = append(where, `s.api_key_id=$`+strconv.Itoa(n))
		args = append(args, a.ID)
	} else {
		// Unknown principal: visible rows stay empty rather than broadening.
		where = append(where, `FALSE`)
	}
	if restricted, ids := a.Permission.ZoneScope(); restricted {
		n++
		where = append(where, `s.zone_id=ANY($`+strconv.Itoa(n)+`)`)
		args = append(args, ids)
	}
	return strings.Join(where, " AND "), args
}

func (s *PgStore) ListSubmissions(ctx context.Context, a authz.Actor, pg models.Page) ([]company.Submission, int, error) {
	pg = pg.Normalize()
	out := []company.Submission{}
	total := 0
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		where, args := submissionScope(a, 1)
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs s WHERE `+where, args...).Scan(&total); e != nil {
			return e
		}
		n := len(args)
		rows, e := tx.Query(ctx, submissionSelect+` WHERE `+where+` ORDER BY s.created_at DESC LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2),
			append(args, pg.PerPage, pg.Offset())...)
		if e != nil {
			return e
		}
		defer rows.Close()
		jobs := []*models.OutboundJob{}
		ids := []uuid.UUID{}
		for rows.Next() {
			job, err := scanSubmission(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
			ids = append(ids, job.ID)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		recips, err := loadSubmissionReceiptRecipients(ctx, tx, a.TenantID, ids)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			view := company.ProjectOutboundReceipt(job, recips[job.ID].ledgerStates, job.RecipientLedger)
			out = append(out, *view)
		}
		return nil
	})
	if e != nil {
		return nil, 0, e
	}
	return out, total, nil
}

func (s *PgStore) GetSubmission(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.Submission, error) {
	v := &company.Submission{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		where, args := submissionScope(a, 2)
		job, e := scanSubmission(tx.QueryRow(ctx, submissionSelect+` WHERE `+where+` AND s.id=$1`, append([]any{id}, args...)...))
		if e == pgx.ErrNoRows {
			return app.NotFound("submission not found")
		}
		if e != nil {
			return e
		}
		recips, e := loadSubmissionReceiptRecipients(ctx, tx, a.TenantID, []uuid.UUID{id})
		if e != nil {
			return e
		}
		*v = *company.ProjectOutboundReceipt(job, recips[id].ledgerStates, job.RecipientLedger)
		return nil
	})
	if e != nil {
		return nil, e
	}
	return v, nil
}

// Content and attachment projections require current mailbox read rights.
// Historical submitter identity only permits the separate operation receipt.
// Denial collapses to 404, including after grants or ownership are revoked.
func (s *PgStore) GetSubmissionContent(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.SubmissionContent, error) {
	var v *company.SubmissionContent
	err := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, current authz.Actor) error {
		var err error
		v, err = readSentContentTx(ctx, tx, current, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

const submissionAttachmentSelect = `SELECT a.id,a.filename,a.content_type,a.size,a.state,a.object_key,a.sha256
` + sentContentFrom + `
 JOIN sent_asset_attachments x ON x.tenant_id=s.tenant_id AND x.asset_id=s.id
 JOIN mail_attachments a ON a.tenant_id=x.tenant_id AND a.id=x.attachment_id`

func (s *PgStore) ListSubmissionAttachments(ctx context.Context, a authz.Actor, id uuid.UUID) ([]company.SubmissionAttachment, error) {
	out := []company.SubmissionAttachment{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		where, args := submissionContentScope(a, 2)
		// An invisible submission collapses to 404 like the metadata view,
		// rather than answering an empty list for a job that does not exist
		// for this actor.
		var one bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1`+sentContentFrom+` WHERE `+where+` AND s.id=$1)`, append([]any{id}, args...)...).Scan(&one)
		if e != nil {
			return e
		}
		if !one {
			return app.NotFound("submission not found")
		}
		rows, e := tx.Query(ctx, submissionAttachmentSelect+` WHERE `+where+` AND s.id=$1 ORDER BY a.id`, append([]any{id}, args...)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v := company.SubmissionAttachment{}
			if e := rows.Scan(&v.ID, &v.Filename, &v.ContentType, &v.Size, &v.State, &v.ObjectKey, &v.SHA256); e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// GetSubmissionAttachment resolves one pinned attachment of a readable
// submission for download. The attachment must be linked to that job
// (outbound_attachments) and finished ('ready'); the object key never leaves
// the store layer's return value unserialized.
func (s *PgStore) GetSubmissionAttachment(ctx context.Context, a authz.Actor, jobID, attachmentID uuid.UUID) (*company.SubmissionAttachment, error) {
	v := &company.SubmissionAttachment{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		where, args := submissionContentScope(a, 3)
		e := tx.QueryRow(ctx, submissionAttachmentSelect+` WHERE `+where+` AND s.id=$1 AND a.id=$2 AND a.state='ready'`, append([]any{jobID, attachmentID}, args...)...).
			Scan(&v.ID, &v.Filename, &v.ContentType, &v.Size, &v.State, &v.ObjectKey, &v.SHA256)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("attachment not found")
		}
		return e
	})
	if e != nil {
		return nil, e
	}
	return v, nil
}
