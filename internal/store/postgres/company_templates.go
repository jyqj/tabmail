package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/enterprise"
	"tabmail/internal/models"
)

func templateFrom(ctx context.Context, q companyQuery, tenant, id uuid.UUID) (*enterprise.Template, error) {
	t := &enterprise.Template{}
	var vars []byte
	err := q.QueryRow(ctx, `SELECT t.id,t.tenant_id,t.family_id,t.version,t.name,t.subject,t.text_body,t.html_body,t.variables,t.status,t.created_at,ARRAY(SELECT mailbox_id FROM company_template_mailboxes WHERE tenant_id=t.tenant_id AND template_id=t.id ORDER BY mailbox_id) FROM company_templates t WHERE t.tenant_id=$1 AND t.id=$2`, tenant, id).Scan(&t.ID, &t.TenantID, &t.FamilyID, &t.Version, &t.Name, &t.Subject, &t.TextBody, &t.HTMLBody, &vars, &t.Status, &t.CreatedAt, &t.MailboxIDs)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(vars, &t.Variables); err != nil {
		return nil, err
	}
	return t, nil
}
func (s *PgStore) GetCompanyTemplate(ctx context.Context, tenant, id uuid.UUID) (*enterprise.Template, error) {
	return templateFrom(ctx, s.pool, tenant, id)
}
func (s *PgStore) ListCompanyTemplates(ctx context.Context, tenant, user uuid.UUID, admin bool) ([]enterprise.Template, error) {
	if admin {
		if _, err := companyAdmin(ctx, s.pool, tenant, user); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id FROM company_templates t WHERE t.tenant_id=$1 AND ($3 OR (t.status='published' AND EXISTS(SELECT 1 FROM company_template_mailboxes tm JOIN company_mailbox_grants g ON g.tenant_id=tm.tenant_id AND g.mailbox_id=tm.mailbox_id JOIN users u ON u.id=g.user_id AND u.tenant_id=g.tenant_id JOIN company_members cm ON cm.tenant_id=g.tenant_id AND cm.user_id=g.user_id WHERE tm.tenant_id=t.tenant_id AND tm.template_id=t.id AND g.user_id=$2 AND g.can_send AND u.is_active AND cm.company_role<>'viewer'))) ORDER BY t.name,t.version DESC`, tenant, user, admin)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []enterprise.Template{}
	for _, id := range ids {
		t, err := templateFrom(ctx, s.pool, tenant, id)
		if err != nil {
			return nil, err
		}
		if t != nil {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (s *PgStore) SaveCompanyTemplate(ctx context.Context, tenant, actor uuid.UUID, t enterprise.Template) (*enterprise.Template, error) {
	if t.Variables == nil {
		t.Variables = map[string]int{}
	}
	if err := enterprise.ValidateTemplate(t); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return nil, err
	}
	c, err := companyAdmin(ctx, tx, tenant, actor)
	if err != nil {
		return nil, err
	}
	t.ID = uuid.New()
	t.TenantID = tenant
	t.Status = "draft"
	t.Version = 1
	if t.FamilyID == uuid.Nil {
		t.FamilyID = t.ID
	} else {
		var latest int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM company_templates WHERE tenant_id=$1 AND family_id=$2`, tenant, t.FamilyID).Scan(&latest); err != nil {
			return nil, err
		}
		if latest == 0 {
			return nil, app.NotFound("template family not found")
		}
		t.Version = latest + 1
	}
	for _, id := range t.MailboxIDs {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailboxes WHERE tenant_id=$1 AND id=$2 AND zone_id=$3)`, tenant, id, c.PrimaryZoneID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, app.BadRequest("template scope includes an unavailable mailbox")
		}
	}
	vars, err := json.Marshal(t.Variables)
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO company_templates(id,tenant_id,family_id,version,name,subject,text_body,html_body,variables,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at`, t.ID, tenant, t.FamilyID, t.Version, t.Name, t.Subject, t.TextBody, t.HTMLBody, vars, actor).Scan(&t.CreatedAt); err != nil {
		return nil, err
	}
	for _, id := range t.MailboxIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO company_template_mailboxes(tenant_id,template_id,mailbox_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenant, t.ID, id); err != nil {
			return nil, err
		}
	}
	if err = auditCompany(ctx, tx, tenant, actor, "template.version.created", t.ID, map[string]any{"family": t.FamilyID, "version": t.Version}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &t, nil
}
func (s *PgStore) SetCompanyTemplateStatus(ctx context.Context, tenant, actor, id uuid.UUID, status string) error {
	if status != "published" && status != "retired" {
		return app.BadRequest("status must be published or retired")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return err
	}
	if _, err = companyAdmin(ctx, tx, tenant, actor); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE company_templates SET status=$3 WHERE tenant_id=$1 AND id=$2 AND ((status='draft' AND $3='published') OR (status IN ('draft','published') AND $3='retired'))`, tenant, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return app.Conflict("template not found or invalid transition; create a new version instead")
	}
	if err = auditCompany(ctx, tx, tenant, actor, "template.status.updated", id, map[string]string{"status": status}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// validateCompanySender is repeated inside the enqueue transaction and immediately
// before each delivery/retry. Neither an admin flag nor a domain wildcard is a grant.
func validateCompanySender(ctx context.Context, q companyQuery, j *models.OutboundJob, templateID *uuid.UUID) (uuid.UUID, *enterprise.Member, error) {
	c, err := companyFrom(ctx, q, j.TenantID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if c == nil {
		return uuid.Nil, nil, app.Forbidden("company not configured")
	}
	if j.UserID == nil || j.APIKeyID != nil || j.ZoneID != c.PrimaryZoneID {
		return uuid.Nil, nil, app.Forbidden("employee sender in primary domain required")
	}
	address, err := enterprise.CanonicalAddress(j.MailFrom)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if address != j.MailFrom {
		return uuid.Nil, nil, app.Forbidden("noncanonical sender")
	}
	var verified bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_zones WHERE tenant_id=$1 AND id=$2 AND is_verified AND mx_verified)`, j.TenantID, c.PrimaryZoneID).Scan(&verified); err != nil {
		return uuid.Nil, nil, err
	}
	if !verified {
		return uuid.Nil, nil, app.Forbidden("primary domain verification required")
	}
	var mid uuid.UUID
	if err = q.QueryRow(ctx, `SELECT id FROM mailboxes WHERE tenant_id=$1 AND zone_id=$2 AND full_address=$3 AND resolved_domain=$4`, j.TenantID, c.PrimaryZoneID, address, c.Domain).Scan(&mid); err == pgx.ErrNoRows {
		return uuid.Nil, nil, app.Forbidden("exact sender not provisioned")
	}
	if err != nil {
		return uuid.Nil, nil, err
	}
	m, err := memberFrom(ctx, q, j.TenantID, *j.UserID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	g, err := grantFrom(ctx, q, j.TenantID, *j.UserID, mid)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if err = enterprise.CheckSender(m, g, templateID != nil); err != nil {
		return uuid.Nil, nil, err
	}
	if templateID != nil {
		var valid bool
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM company_templates t JOIN company_template_mailboxes tm ON tm.tenant_id=t.tenant_id AND tm.template_id=t.id WHERE t.tenant_id=$1 AND t.id=$2 AND t.status='published' AND tm.mailbox_id=$3)`, j.TenantID, *templateID, mid).Scan(&valid); err != nil {
			return uuid.Nil, nil, err
		}
		if !valid {
			return uuid.Nil, nil, app.Forbidden("published template not authorized for this sender")
		}
	}
	return mid, m, nil
}
func (s *PgStore) CreateCompanyJob(ctx context.Context, j *models.OutboundJob, templateID *uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, j.TenantID); err != nil {
		return err
	}
	mid, m, err := validateCompanySender(ctx, tx, j, templateID)
	if err != nil {
		return err
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE tenant_id=$1 AND user_id=$2 AND created_at >= date_trunc('day',now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`, j.TenantID, m.UserID).Scan(&n); err != nil {
		return err
	}
	if n >= m.DailyQuota {
		return enterprise.ErrQuota
	}
	// Reserve quota, enqueue immutable payload, provenance and audit atomically.
	prepareOutboundJob(j)
	if err = insertOutboundJob(ctx, tx, j); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_submissions(job_id,tenant_id,mailbox_id,template_id,content_hash) VALUES($1,$2,$3,$4,$5)`, j.ID, j.TenantID, mid, templateID, enterprise.JobDigest(j)); err != nil {
		return err
	}
	if err = auditCompany(ctx, tx, j.TenantID, m.UserID, "mail.submitted", j.ID, map[string]any{"mailbox_id": mid, "template_id": templateID, "recipient_count": len(j.RcptTo)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PgStore) ValidateCompanyJob(ctx context.Context, j *models.OutboundJob) error {
	c, err := companyFrom(ctx, s.pool, j.TenantID)
	if err != nil || c == nil {
		return err
	}
	var templateID *uuid.UUID
	var digest string
	err = s.pool.QueryRow(ctx, `SELECT template_id,content_hash FROM company_submissions WHERE tenant_id=$1 AND job_id=$2`, j.TenantID, j.ID).Scan(&templateID, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Forbidden("missing company submission provenance")
	}
	if err != nil {
		return err
	}
	if enterprise.JobDigest(j) != digest {
		return app.Forbidden("submitted message has changed")
	}
	_, _, err = validateCompanySender(ctx, s.pool, j, templateID)
	return err
}
func (s *PgStore) HasCompanies(ctx context.Context) (bool, error) {
	var yes bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM company_settings)`).Scan(&yes)
	return yes, err
}
