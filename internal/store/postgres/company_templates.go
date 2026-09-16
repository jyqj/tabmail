package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

const companyTemplateSelect = `SELECT id,name,draft,revision,retired,updated_at FROM mail_templates`

func scanCompanyTemplate(row pgx.Row) (company.Template, error) {
	v := company.Template{}
	var raw []byte
	e := row.Scan(&v.ID, &v.Name, &raw, &v.Revision, &v.Retired, &v.UpdatedAt)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(raw, &v.Draft)
	return v, e
}

const companyVersionSelect = `SELECT v.id,v.template_id,t.name,v.version,v.snapshot,v.content_hash,v.published_at,v.revoked_at FROM mail_template_versions v JOIN mail_templates t ON t.id=v.template_id AND t.tenant_id=v.tenant_id`

func scanCompanyVersion(row pgx.Row) (company.TemplateVersion, error) {
	v := company.TemplateVersion{}
	var raw []byte
	e := row.Scan(&v.ID, &v.TemplateID, &v.Name, &v.Version, &raw, &v.ContentHash, &v.PublishedAt, &v.RevokedAt)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(raw, &v.Snapshot)
	return v, e
}
func (s *PgStore) ListMailTemplates(ctx context.Context, a authz.Actor) ([]company.Template, error) {
	out := []company.Template{}
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, companyTemplateSelect+` WHERE tenant_id=$1 ORDER BY name LIMIT 500`, a.TenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanCompanyTemplate(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (s *PgStore) SaveMailTemplate(ctx context.Context, a authz.Actor, v company.Template) (*company.Template, error) {
	v.Name = strings.TrimSpace(v.Name)
	if len(v.Name) < 1 || len(v.Name) > 120 {
		return nil, app.BadRequest("template name must be 1-120 bytes")
	}
	if e := company.ValidateTemplate(v.Draft); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(v.Draft)
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if v.ID == uuid.Nil {
			if v.Revision != 0 {
				return app.Conflict("new template revision must be zero")
			}
			v.ID = uuid.New()
			if e := tx.QueryRow(ctx, `INSERT INTO mail_templates(id,tenant_id,name,draft,created_by) VALUES($1,$2,$3,$4,$5) RETURNING revision,updated_at`, v.ID, a.TenantID, v.Name, raw, a.ID).Scan(&v.Revision, &v.UpdatedAt); e != nil {
				return e
			}
		} else {
			e := tx.QueryRow(ctx, `UPDATE mail_templates SET name=$3,draft=$4,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND revision=$5 RETURNING revision,updated_at,retired`, a.TenantID, v.ID, v.Name, raw, v.Revision).Scan(&v.Revision, &v.UpdatedAt, &v.Retired)
			if errors.Is(e, pgx.ErrNoRows) {
				return app.Conflict("template changed; reload draft")
			}
			if e != nil {
				return e
			}
		}
		return companyAudit(ctx, tx, a, "template.save", "template", v.ID, map[string]any{"revision": v.Revision, "draft_hash": company.Digest(v.Draft)})
	})
	return &v, e
}
func (s *PgStore) PublishMailTemplate(ctx context.Context, a authz.Actor, id uuid.UUID, revision int) (*company.TemplateVersion, error) {
	var out company.TemplateVersion
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		t, e := scanCompanyTemplate(tx.QueryRow(ctx, companyTemplateSelect+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, id))
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("template not found")
		}
		if e != nil {
			return e
		}
		if t.Revision != revision || t.Retired {
			return app.Conflict("template changed or retired")
		}
		if e = company.ValidateTemplate(t.Draft); e != nil {
			return e
		}
		raw, _ := json.Marshal(t.Draft)
		out = company.TemplateVersion{ID: uuid.New(), TemplateID: id, Name: t.Name, Snapshot: t.Draft, ContentHash: company.Digest(t.Draft)}
		if e = tx.QueryRow(ctx, `INSERT INTO mail_template_versions(id,tenant_id,template_id,version,snapshot,content_hash,published_by) SELECT $1,$2,$3,COALESCE(MAX(version),0)+1,$4,$5,$6 FROM mail_template_versions WHERE template_id=$3 RETURNING version,published_at`, out.ID, a.TenantID, id, raw, out.ContentHash, a.ID).Scan(&out.Version, &out.PublishedAt); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE mail_templates SET revision=revision+1,updated_at=now() WHERE id=$1`, id); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "template.publish", "template", id, map[string]any{"version_id": out.ID, "version": out.Version, "content_hash": out.ContentHash})
	})
	return &out, e
}
func (s *PgStore) SetMailTemplateRetired(ctx context.Context, a authz.Actor, id uuid.UUID, revision int, retired bool) error {
	return s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		tag, e := tx.Exec(ctx, `UPDATE mail_templates SET retired=$4,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND revision=$3`, a.TenantID, id, revision, retired)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("template changed")
		}
		return companyAudit(ctx, tx, a, "template.retire", "template", id, map[string]any{"retired": retired})
	})
}
func (s *PgStore) ListTemplateVersions(ctx context.Context, a authz.Actor, id uuid.UUID) ([]company.TemplateVersion, error) {
	out := []company.TemplateVersion{}
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, companyVersionSelect+` WHERE v.tenant_id=$1 AND v.template_id=$2 ORDER BY v.version DESC LIMIT 200`, a.TenantID, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanCompanyVersion(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (s *PgStore) ListTemplateGrants(ctx context.Context, a authz.Actor, id uuid.UUID) ([]company.TemplateGrant, error) {
	out := []company.TemplateGrant{}
	e := s.companyReadTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, `SELECT template_id,mailbox_id,user_id FROM mail_template_grants WHERE tenant_id=$1 AND template_id=$2 ORDER BY mailbox_id,user_id`, a.TenantID, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			g := company.TemplateGrant{}
			if e = rows.Scan(&g.TemplateID, &g.MailboxID, &g.UserID); e != nil {
				return e
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, e
}
func (s *PgStore) SetTemplateGrant(ctx context.Context, a authz.Actor, g company.TemplateGrant, enabled bool) error {
	return s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if _, e := s.mailboxAccessTx(ctx, tx, a, g.MailboxID); e != nil {
			return e
		}
		if e := activeCompanyUser(ctx, tx, a.TenantID, g.UserID); e != nil {
			return e
		}
		var valid bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mail_templates WHERE tenant_id=$1 AND id=$2)`, a.TenantID, g.TemplateID).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return app.NotFound("template not found")
		}
		var e error
		if enabled {
			_, e = tx.Exec(ctx, `INSERT INTO mail_template_grants(tenant_id,template_id,mailbox_id,user_id,granted_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, a.TenantID, g.TemplateID, g.MailboxID, g.UserID, a.ID)
		} else {
			_, e = tx.Exec(ctx, `DELETE FROM mail_template_grants WHERE tenant_id=$1 AND template_id=$2 AND mailbox_id=$3 AND user_id=$4`, a.TenantID, g.TemplateID, g.MailboxID, g.UserID)
		}
		if e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "template.grant", "template", g.TemplateID, map[string]any{"mailbox_id": g.MailboxID, "user_id": g.UserID, "enabled": enabled})
	})
}
func (s *PgStore) ListUsableTemplates(ctx context.Context, a authz.Actor, mailbox uuid.UUID) ([]company.TemplateVersion, error) {
	out := []company.TemplateVersion{}
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		access, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !access.CanSend {
			return app.Forbidden("mailbox send permission required")
		}
		rows, e := tx.Query(ctx, companyVersionSelect+` WHERE v.tenant_id=$1 AND NOT t.retired AND v.revoked_at IS NULL AND v.version=(SELECT max(w.version) FROM mail_template_versions w WHERE w.template_id=v.template_id AND w.revoked_at IS NULL) AND ($4 OR EXISTS(SELECT 1 FROM mail_template_grants g WHERE g.template_id=v.template_id AND g.tenant_id=$1 AND g.mailbox_id=$2 AND g.user_id=$3)) ORDER BY t.name`, a.TenantID, mailbox, a.ID, a.IsTenantAdmin())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanCompanyVersion(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}

// Validate immutable provenance and CURRENT usage/From rights. A template grant
// never grants send-as. API keys must remain owned by this active employee.
func (s *PgStore) TemplateForSend(ctx context.Context, tenant uuid.UUID, user, key *uuid.UUID, mailbox, version uuid.UUID) (*company.TemplateVersion, string, string, error) {
	if user == nil {
		return nil, "", "", authz.ErrForbidden("published templates require an employee sender")
	}
	a := authz.Actor{Type: authz.PrincipalUser, ID: *user, TenantID: tenant}
	var out company.TemplateVersion
	var employee, companyName string
	e := s.companyReadTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		access, e := s.mailboxAccessTx(ctx, tx, a, mailbox)
		if e != nil {
			return e
		}
		if !access.CanSend {
			return authz.ErrForbidden("template sender mailbox permission revoked")
		}
		if key != nil {
			var valid bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_api_keys WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3 AND (expires_at IS NULL OR expires_at>now()))`, *key, tenant, *user).Scan(&valid)
			if e != nil {
				return e
			}
			if !valid {
				return authz.ErrForbidden("template sending key revoked")
			}
		}
		out, e = scanCompanyVersion(tx.QueryRow(ctx, companyVersionSelect+` WHERE v.tenant_id=$1 AND v.id=$2 AND NOT t.retired AND v.revoked_at IS NULL AND ($5 OR EXISTS(SELECT 1 FROM mail_template_grants g WHERE g.tenant_id=$1 AND g.template_id=v.template_id AND g.mailbox_id=$3 AND g.user_id=$4))`, tenant, version, mailbox, *user, a.IsTenantAdmin()))
		if errors.Is(e, pgx.ErrNoRows) {
			return authz.ErrForbidden("published template unavailable or not granted")
		}
		if e != nil {
			return e
		}
		if out.ContentHash != company.Digest(out.Snapshot) {
			return authz.ErrForbidden("published template integrity mismatch")
		}
		if e = tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, *user).Scan(&employee); e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `SELECT name FROM company_settings WHERE tenant_id=$1`, tenant).Scan(&companyName)
		if errors.Is(e, pgx.ErrNoRows) {
			return authz.ErrForbidden("company settings unavailable")
		}
		return e
	})
	return &out, employee, companyName, e
}

// TemplateContentDigest ties the final queued MIME inputs to the selected
// immutable version. It deliberately excludes current profile display names.
var _ = models.RoleUser
