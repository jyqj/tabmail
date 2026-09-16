package postgres

import (
	"context"
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

const submissionSelect = `SELECT s.id,s.sender_mailbox_id,s.mail_from,s.subject,
 COALESCE(s.template_version_id::TEXT,''),s.draft_id IS NOT NULL,
 COALESCE(array_length(s.attachment_ids,1),0),s.created_at,
 s.state,s.in_flight_domain<>'' FROM outbound_jobs s`

func scanSubmission(row pgx.Row) (*company.Submission, models.OutboundState, bool, error) {
	v := &company.Submission{}
	var state models.OutboundState
	var inFlight bool
	var templateVersion string
	if e := row.Scan(&v.ID, &v.MailboxID, &v.MailFrom, &v.Subject, &templateVersion, &v.DraftConsumed, &v.AttachmentCount, &v.CreatedAt, &state, &inFlight); e != nil {
		return nil, "", false, e
	}
	if templateVersion != "" {
		id, e := uuid.Parse(templateVersion)
		if e != nil {
			return nil, "", false, e
		}
		v.TemplateVersionID = &id
	}
	return v, state, inFlight, nil
}

// applySubmissionOutcome derives the user-facing status and uncertainty flag
// from the job state plus the recipient ledger. In-flight ambiguity is fed to
// the mapper as an "uncertain" ledger entry so it surfaces as needs_attention.
func applySubmissionOutcome(v *company.Submission, state models.OutboundState, inFlight bool) {
	states := make([]string, 0, len(v.Recipients)+1)
	for _, r := range v.Recipients {
		states = append(states, r.State)
	}
	if inFlight && state != models.OutboundProcessing {
		states = append(states, "uncertain")
		v.DeliveryUncertain = true
	}
	v.Status = company.DeriveSubmissionStatus(state, states)
}

func loadSubmissionRecipients(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]company.SubmissionRecipient, error) {
	out := map[uuid.UUID][]company.SubmissionRecipient{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, e := tx.Query(ctx, `SELECT job_id,address,state FROM outbound_recipients WHERE tenant_id=$1 AND job_id=ANY($2) ORDER BY job_id,address`, tenant, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var job uuid.UUID
		r := company.SubmissionRecipient{}
		if e := rows.Scan(&job, &r.Address, &r.State); e != nil {
			return nil, e
		}
		out[job] = append(out[job], r)
	}
	return out, rows.Err()
}

// submissionScope builds the tenant-isolated visibility predicate for the
// actor. It mirrors ListOutboundJobsScoped's owner rule (user_id for members,
// api_key_id for keys) extended by readable-mailbox provenance, plus the
// zone allowlist. argBase names the first free placeholder so callers can
// prepend their own arguments.
func submissionScope(a authz.Actor, argBase int) (string, []any) {
	where := []string{"s.tenant_id=$" + strconv.Itoa(argBase)}
	args := []any{a.TenantID}
	n := argBase
	uid := a.EffectiveUserID()
	if uid != nil {
		n++
		u := "$" + strconv.Itoa(n)
		args = append(args, *uid)
		where[len(where)-1] = `(s.user_id=` + u + ` OR s.sender_user_id=` + u + ` OR s.sender_mailbox_id IN (
			SELECT m.id FROM mailboxes m WHERE m.tenant_id=$` + strconv.Itoa(argBase) + `
			 AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())
			 AND (m.owner_user_id=` + u + ` OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=` + u + ` AND g.can_read))))`
	} else if a.Type == authz.PrincipalAPIKey {
		n++
		where[len(where)-1] = `s.api_key_id=$` + strconv.Itoa(n)
		args = append(args, a.ID)
	} else {
		// Unknown principal: visible rows stay empty rather than broadening.
		where[len(where)-1] = `FALSE`
	}
	if a.Permission != nil && len(a.Permission.AllowedZoneIDs) > 0 {
		n++
		where = append(where, `s.zone_id=ANY($`+strconv.Itoa(n)+`)`)
		args = append(args, a.Permission.AllowedZoneIDs)
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
		type rowState struct {
			state    models.OutboundState
			inFlight bool
		}
		states := map[uuid.UUID]rowState{}
		ids := []uuid.UUID{}
		for rows.Next() {
			v, state, inFlight, e := scanSubmission(rows)
			if e != nil {
				return e
			}
			out = append(out, *v)
			ids = append(ids, v.ID)
			states[v.ID] = rowState{state, inFlight}
		}
		if e := rows.Err(); e != nil {
			return e
		}
		recips, e := loadSubmissionRecipients(ctx, tx, a.TenantID, ids)
		if e != nil {
			return e
		}
		for i := range out {
			out[i].Recipients = recips[out[i].ID]
			rs := states[out[i].ID]
			applySubmissionOutcome(&out[i], rs.state, rs.inFlight)
		}
		return nil
	})
	return out, total, e
}

func (s *PgStore) GetSubmission(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.Submission, error) {
	v := &company.Submission{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		where, args := submissionScope(a, 2)
		sv, state, inFlight, e := scanSubmission(tx.QueryRow(ctx, submissionSelect+` WHERE `+where+` AND s.id=$1`, append([]any{id}, args...)...))
		if e == pgx.ErrNoRows {
			return app.NotFound("submission not found")
		}
		if e != nil {
			return e
		}
		recips, e := loadSubmissionRecipients(ctx, tx, a.TenantID, []uuid.UUID{id})
		if e != nil {
			return e
		}
		sv.Recipients = recips[id]
		applySubmissionOutcome(sv, state, inFlight)
		*v = *sv
		return nil
	})
	if e != nil {
		return nil, e
	}
	return v, nil
}
