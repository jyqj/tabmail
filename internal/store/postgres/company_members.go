package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// One company lock orders administrative mutations. Interactive identity and
// effective permissions are reloaded inside the transaction, never trusted
// from the HTTP handshake. Normal resource access is separate from management.
func (s *PgStore) companyTx(ctx context.Context, actor authz.Actor, admin bool, f func(pgx.Tx, authz.Actor) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockMemberTenant(ctx, tx, actor.TenantID); err != nil {
		return err
	}
	actor, err = currentMemberActor(ctx, tx, actor, actor.TenantID)
	if err != nil {
		return err
	}
	if admin && !actor.IsAdmin && !actor.IsSuperAdmin {
		return app.Forbidden("company administrator required")
	}
	if !actor.IsAdmin && !actor.IsSuperAdmin {
		actor.Permission, err = effectivePermission(ctx, tx, actor.ID)
		if err != nil {
			return err
		}
	}
	if err = f(tx, actor); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "40001" || pg.Code == "55P03") {
			return app.Conflict("resource changed or already exists; reload before retrying")
		}
		return err
	}
	return tx.Commit(ctx)
}
func companyAudit(ctx context.Context, tx pgx.Tx, a authz.Actor, action, kind string, id uuid.UUID, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,$3,$4,$5,$6)`, a.TenantID, a.AuditLabel(), action, kind, id, b)
	return e
}
func meaningfulReason(s string) bool { n := len(strings.TrimSpace(s)); return n >= 8 && n <= 1000 }
func (s *PgStore) GetCompanySettings(ctx context.Context, tenant uuid.UUID) (*company.Settings, error) {
	c := &company.Settings{}
	e := s.pool.QueryRow(ctx, `SELECT c.tenant_id,c.name,c.primary_zone_id,z.domain,c.revision FROM company_settings c JOIN domain_zones z ON z.id=c.primary_zone_id WHERE c.tenant_id=$1`, tenant).Scan(&c.TenantID, &c.Name, &c.PrimaryZoneID, &c.Domain, &c.Revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, e
}
func (s *PgStore) ConfigureCompany(ctx context.Context, a authz.Actor, c company.Settings) (*company.Settings, error) {
	c.Name = strings.TrimSpace(c.Name)
	if len(c.Name) < 1 || len(c.Name) > 120 {
		return nil, app.BadRequest("company name must be 1-120 bytes")
	}
	c.TenantID = a.TenantID
	err := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		var valid bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domain_zones WHERE tenant_id=$1 AND id=$2 AND is_verified AND mx_verified)`, a.TenantID, c.PrimaryZoneID).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return app.BadRequest("select a verified company domain with verified MX")
		}
		var old int
		e := tx.QueryRow(ctx, `SELECT revision FROM company_settings WHERE tenant_id=$1`, a.TenantID).Scan(&old)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if old != c.Revision {
			return app.Conflict("company settings changed")
		}
		if e = tx.QueryRow(ctx, `INSERT INTO company_settings(tenant_id,name,primary_zone_id) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET name=EXCLUDED.name,primary_zone_id=EXCLUDED.primary_zone_id,revision=company_settings.revision+1,updated_at=now() RETURNING revision`, a.TenantID, c.Name, c.PrimaryZoneID).Scan(&c.Revision); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "company.configure", "company", a.TenantID, map[string]any{"name": c.Name, "primary_zone_id": c.PrimaryZoneID, "revision": c.Revision})
	})
	if err != nil {
		return nil, err
	}
	return s.GetCompanySettings(ctx, a.TenantID)
}
func companyDomain(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) (uuid.UUID, string, error) {
	var id uuid.UUID
	var domain string
	e := tx.QueryRow(ctx, `SELECT z.id,z.domain FROM company_settings c JOIN domain_zones z ON z.id=c.primary_zone_id AND z.tenant_id=c.tenant_id WHERE c.tenant_id=$1 AND z.is_verified AND z.mx_verified`, tenant).Scan(&id, &domain)
	if errors.Is(e, pgx.ErrNoRows) {
		e = app.BadRequest("configure a verified primary company domain first")
	}
	return id, domain, e
}
func (s *PgStore) InviteEmployee(ctx context.Context, a authz.Actor, in company.InvitationInput, hash string) (*company.Invitation, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.LocalPart = strings.ToLower(strings.TrimSpace(in.LocalPart))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	addr, e := mail.ParseAddress(in.Email)
	if e != nil || addr.Address != in.Email || len(in.Email) > 254 || !company.ValidLocalPart(in.LocalPart) || len(in.DisplayName) < 1 || len(in.DisplayName) > 120 || len(hash) != 64 {
		return nil, app.BadRequest("valid email, display name and local part required")
	}
	out := &company.Invitation{ID: uuid.New(), Email: in.Email, DisplayName: in.DisplayName, ExpiresAt: time.Now().UTC().Add(72 * time.Hour)}
	e = s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		_, domain, e := companyDomain(ctx, tx, a.TenantID)
		if e != nil {
			return e
		}
		out.Address = in.LocalPart + "@" + domain
		var taken bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=$1) OR EXISTS(SELECT 1 FROM mailboxes WHERE full_address=$2) OR EXISTS(SELECT 1 FROM employee_invitations WHERE tenant_id=$3 AND (email=$1 OR mailbox_address=$2) AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>now())`, in.Email, out.Address, a.TenantID).Scan(&taken)
		if e != nil {
			return e
		}
		if taken {
			return app.Conflict("employee or address already exists or is invited")
		}
		if in.PermissionProfileID != nil {
			var ok bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM permission_profiles WHERE id=$1 AND (tenant_id=$2 OR tenant_id IS NULL))`, *in.PermissionProfileID, a.TenantID).Scan(&ok); e != nil {
				return e
			}
			if !ok {
				return app.BadRequest("permission profile is outside company")
			}
		}
		if e = tx.QueryRow(ctx, `INSERT INTO employee_invitations(id,tenant_id,email,display_name,mailbox_address,permission_profile_id,token_hash,invited_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING created_at`, out.ID, a.TenantID, out.Email, out.DisplayName, out.Address, in.PermissionProfileID, hash, a.ID, out.ExpiresAt).Scan(&out.CreatedAt); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "employee.invite", "invitation", out.ID, map[string]any{"email": out.Email, "address": out.Address})
	})
	return out, e
}
func scanInvitation(row pgx.Row) (company.Invitation, error) {
	v := company.Invitation{}
	e := row.Scan(&v.ID, &v.Email, &v.Address, &v.DisplayName, &v.ExpiresAt, &v.ConsumedAt, &v.RevokedAt, &v.CreatedAt)
	return v, e
}
func (s *PgStore) ListEmployeeInvitations(ctx context.Context, a authz.Actor) ([]company.Invitation, error) {
	out := []company.Invitation{}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, `SELECT id,email,mailbox_address,display_name,expires_at,consumed_at,revoked_at,created_at FROM employee_invitations WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200`, a.TenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanInvitation(rows)
			if e != nil {
				return e
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, e
}
func (s *PgStore) RevokeEmployeeInvitation(ctx context.Context, a authz.Actor, id uuid.UUID) error {
	return s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		tag, e := tx.Exec(ctx, `UPDATE employee_invitations SET revoked_at=now() WHERE tenant_id=$1 AND id=$2 AND consumed_at IS NULL AND revoked_at IS NULL`, a.TenantID, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return app.Conflict("invitation already used, revoked or unavailable")
		}
		return companyAudit(ctx, tx, a, "employee.invite_revoke", "invitation", id, nil)
	})
}
func (s *PgStore) ActivateEmployee(ctx context.Context, hash, passwordHash string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var tenant uuid.UUID
	e = tx.QueryRow(ctx, `SELECT tenant_id FROM employee_invitations WHERE token_hash=$1`, hash).Scan(&tenant)
	if errors.Is(e, pgx.ErrNoRows) {
		return app.BadRequest("invalid or expired invitation")
	}
	if e != nil {
		return e
	}
	if e = lockMemberTenant(ctx, tx, tenant); e != nil {
		return e
	}
	var id, inviter uuid.UUID
	var email, name, address string
	var profile *uuid.UUID
	e = tx.QueryRow(ctx, `SELECT id,email,display_name,mailbox_address,permission_profile_id,invited_by FROM employee_invitations WHERE token_hash=$1 AND expires_at>now() AND consumed_at IS NULL AND revoked_at IS NULL FOR UPDATE`, hash).Scan(&id, &email, &name, &address, &profile, &inviter)
	if errors.Is(e, pgx.ErrNoRows) {
		return app.BadRequest("invalid or expired invitation")
	}
	if e != nil {
		return e
	}
	var sponsor bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_active AND (role='super_admin' OR (tenant_id=$2 AND role='admin')))`, inviter, tenant).Scan(&sponsor); e != nil {
		return e
	}
	if !sponsor {
		return app.Forbidden("inviting administrator is no longer authorized")
	}
	zone, domain, e := companyDomain(ctx, tx, tenant)
	if e != nil {
		return e
	}
	parts := strings.Split(address, "@")
	if len(parts) != 2 || parts[1] != domain {
		return app.Conflict("company primary domain changed; request a new invitation")
	}
	if profile == nil {
		p := uuid.New()
		e = tx.QueryRow(ctx, `INSERT INTO permission_profiles(id,tenant_id,name,description,can_send,daily_send_quota,daily_receive_quota,max_mailboxes,max_domains,can_create_domains,can_create_routes,can_create_api_keys) VALUES($1,$2,'Company employee','Employee mailbox access; no infrastructure administration',true,200,0,1,0,false,false,false) ON CONFLICT(tenant_id,name) WHERE tenant_id IS NOT NULL DO UPDATE SET name=EXCLUDED.name RETURNING id`, p, tenant).Scan(&p)
		if e != nil {
			return e
		}
		profile = &p
	} else {
		var ok bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM permission_profiles WHERE id=$1 AND (tenant_id=$2 OR tenant_id IS NULL))`, *profile, tenant).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return app.Forbidden("invitation profile no longer available")
		}
	}
	uid := uuid.New()
	if _, e = tx.Exec(ctx, `INSERT INTO users(id,tenant_id,email,password_hash,display_name,role,is_active,permission_profile_id) VALUES($1,$2,$3,$4,$5,'user',true,$6)`, uid, tenant, email, passwordHash, name, profile); e != nil {
		return app.Conflict("employee email already exists")
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode,owner_user_id,mailbox_kind,retention_hours_override) VALUES($1,$2,$3,$4,$5,$6,'token',$7,'personal',0)`, uuid.New(), tenant, zone, parts[0], domain, address, uid); e != nil {
		return app.Conflict("employee mailbox already exists")
	}
	if _, e = tx.Exec(ctx, `UPDATE employee_invitations SET consumed_at=now() WHERE id=$1`, id); e != nil {
		return e
	}
	a := authz.Actor{ID: uid, Type: authz.PrincipalUser, TenantID: tenant}
	if e = companyAudit(ctx, tx, a, "employee.activate", "user", uid, map[string]any{"invitation_id": id, "mailbox": address}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *PgStore) mailboxAccessTx(ctx context.Context, tx pgx.Tx, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	mb, e := s.scanMailbox(tx.QueryRow(ctx, mailboxSelect+` WHERE m.tenant_id=$1 AND m.id=$2`, a.TenantID, id))
	if e != nil {
		return nil, e
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	v := &company.MailboxAccess{Mailbox: *mb}
	if e = tx.QueryRow(ctx, `SELECT lifecycle_revision FROM mailboxes WHERE id=$1`, id).Scan(&v.Revision); e != nil {
		return nil, e
	}
	if mb.ExpiresAt != nil && !mb.ExpiresAt.After(time.Now()) {
		return v, nil
	}
	if a.Permission != nil && !models.ZoneAllowed(a.Permission.AllowedZoneIDs, mb.ZoneID) {
		return v, nil
	}
	if mb.OwnerUserID != nil && *mb.OwnerUserID == a.ID {
		v.CanRead = true
		v.CanOrganize = true
		v.CanSend = true
	} else {
		e = tx.QueryRow(ctx, `SELECT can_read,can_organize,can_send,template_only FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3`, a.TenantID, id, a.ID).Scan(&v.CanRead, &v.CanOrganize, &v.CanSend, &v.TemplateOnly)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, e
		}
	}
	if !a.IsAdmin && !a.IsSuperAdmin && (a.Permission == nil || !a.Permission.CanSend) {
		v.CanSend = false
	}
	return v, nil
}
func (s *PgStore) ListWorkMailboxes(ctx context.Context, a authz.Actor) ([]company.MailboxAccess, error) {
	out := []company.MailboxAccess{}
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		rows, e := tx.Query(ctx, `SELECT id FROM mailboxes WHERE tenant_id=$1 AND ($2 OR owner_user_id=$3 OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.mailbox_id=mailboxes.id AND g.user_id=$3 AND (g.can_read OR g.can_send))) ORDER BY full_address LIMIT 500`, a.TenantID, a.IsAdmin || a.IsSuperAdmin, a.ID)
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
		for _, id := range ids {
			v, e := s.mailboxAccessTx(ctx, tx, a, id)
			if e != nil {
				return e
			}
			out = append(out, *v)
		}
		return nil
	})
	return out, e
}
func activeCompanyUser(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) error {
	var ok bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2 AND is_active)`, tenant, id).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return app.BadRequest("active same-company employee required")
	}
	return nil
}
func (s *PgStore) CreateWorkMailbox(ctx context.Context, a authz.Actor, in company.MailboxInput) (*models.Mailbox, error) {
	in.LocalPart = strings.ToLower(strings.TrimSpace(in.LocalPart))
	if !company.ValidLocalPart(in.LocalPart) || (in.Kind != "personal" && in.Kind != "shared") || in.RetentionHours != nil && (*in.RetentionHours < 0 || *in.RetentionHours > 876000) {
		return nil, app.BadRequest("invalid mailbox kind, address or retention")
	}
	if in.Kind == "personal" && in.OwnerUserID == nil {
		return nil, app.BadRequest("personal mailbox requires owner")
	}
	if in.Kind == "shared" && in.OwnerUserID != nil {
		return nil, app.BadRequest("shared mailbox belongs to company; use grants")
	}
	var out *models.Mailbox
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		zone, domain, e := companyDomain(ctx, tx, a.TenantID)
		if e != nil {
			return e
		}
		if in.OwnerUserID != nil {
			if e = activeCompanyUser(ctx, tx, a.TenantID, *in.OwnerUserID); e != nil {
				return e
			}
		}
		hours := 0
		if in.RetentionHours != nil {
			hours = *in.RetentionHours
		}
		id := uuid.New()
		if _, e = tx.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode,owner_user_id,mailbox_kind,retention_hours_override) VALUES($1,$2,$3,$4,$5,$6,'token',$7,$8,$9)`, id, a.TenantID, zone, in.LocalPart, domain, in.LocalPart+"@"+domain, in.OwnerUserID, in.Kind, hours); e != nil {
			return e
		}
		if e = companyAudit(ctx, tx, a, "mailbox.provision", "mailbox", id, in); e != nil {
			return e
		}
		out, e = s.scanMailbox(tx.QueryRow(ctx, mailboxSelect+` WHERE m.id=$1`, id))
		return e
	})
	return out, e
}
func (s *PgStore) TransferWorkMailbox(ctx context.Context, a authz.Actor, id, owner uuid.UUID, revision int64, reason string) error {
	if !meaningfulReason(reason) {
		return app.BadRequest("handover reason must be 8-1000 bytes")
	}
	return s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if e := activeCompanyUser(ctx, tx, a.TenantID, owner); e != nil {
			return e
		}
		var old *uuid.UUID
		var kind string
		var rev int64
		e := tx.QueryRow(ctx, `SELECT owner_user_id,mailbox_kind,lifecycle_revision FROM mailboxes WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, id).Scan(&old, &kind, &rev)
		if errors.Is(e, pgx.ErrNoRows) {
			return app.NotFound("mailbox not found")
		}
		if e != nil {
			return e
		}
		if rev != revision || kind != "personal" {
			return app.Conflict("only unchanged personal mailboxes can be handed over")
		}
		if _, e = tx.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$3,lifecycle_revision=lifecycle_revision+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID, id, owner); e != nil {
			return e
		}
		if old != nil {
			if _, e = tx.Exec(ctx, `DELETE FROM mailbox_grants WHERE mailbox_id=$1 AND user_id=$2`, id, *old); e != nil {
				return e
			}
		}
		return companyAudit(ctx, tx, a, "mailbox.handover", "mailbox", id, map[string]any{"old_owner": old, "new_owner": owner, "reason": reason})
	})
}
func (s *PgStore) OffboardEmployee(ctx context.Context, a authz.Actor, target, successor uuid.UUID, reason string) error {
	if !meaningfulReason(reason) || target == successor || target == a.ID {
		return app.BadRequest("distinct employee/successor and handover reason required")
	}
	return s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		u, e := scanUser(tx.QueryRow(ctx, userSelect+` WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, target))
		if e != nil {
			return e
		}
		if u == nil {
			return app.NotFound("employee not found")
		}
		if !authz.CanManageTenantMember(a, a.TenantID, u.Role) {
			return app.Forbidden("cannot manage this employee")
		}
		next := *u
		next.IsActive = false
		if e = guardMemberRemoval(ctx, tx, u, &next); e != nil {
			return e
		}
		if e = activeCompanyUser(ctx, tx, a.TenantID, successor); e != nil {
			return e
		}
		for _, stmt := range []string{`UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$2 AND revoked_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2)`, `DELETE FROM tenant_api_keys WHERE tenant_id=$1 AND owner_user_id=$2`, `DELETE FROM mailbox_grants WHERE tenant_id=$1 AND user_id=$2`, `DELETE FROM mail_template_grants WHERE tenant_id=$1 AND user_id=$2`, `UPDATE users SET is_active=false,session_version=session_version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`} {
			if _, e = tx.Exec(ctx, stmt, a.TenantID, target); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$3,lifecycle_revision=lifecycle_revision+1 WHERE tenant_id=$1 AND owner_user_id=$2`, a.TenantID, target, successor); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "employee.offboard", "user", target, map[string]any{"successor": successor, "reason": reason, "queued_sends": "reauthorized before next delivery"})
	})
}
func (s *PgStore) ListWorkGrants(ctx context.Context, a authz.Actor, id uuid.UUID) ([]models.MailboxGrant, error) {
	out := []models.MailboxGrant{}
	e := s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		if _, e := s.mailboxAccessTx(ctx, tx, a, id); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only,granted_by,created_at,updated_at FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 ORDER BY user_id`, a.TenantID, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			g := models.MailboxGrant{}
			if e = rows.Scan(&g.TenantID, &g.MailboxID, &g.UserID, &g.CanRead, &g.CanOrganize, &g.CanSend, &g.TemplateOnly, &g.GrantedBy, &g.CreatedAt, &g.UpdatedAt); e != nil {
				return e
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	return out, e
}
func (s *PgStore) SetWorkGrant(ctx context.Context, a authz.Actor, g models.MailboxGrant) error {
	if g.CanOrganize && !g.CanRead || g.TemplateOnly && !g.CanSend {
		return app.BadRequest("organize requires read; template-only requires send")
	}
	return s.companyTx(ctx, a, true, func(tx pgx.Tx, a authz.Actor) error {
		v, e := s.mailboxAccessTx(ctx, tx, a, g.MailboxID)
		if e != nil {
			return e
		}
		if v.Mailbox.OwnerUserID != nil && *v.Mailbox.OwnerUserID == g.UserID {
			return app.BadRequest("owner rights are intrinsic; use handover")
		}
		if e = activeCompanyUser(ctx, tx, a.TenantID, g.UserID); e != nil {
			return e
		}
		if !g.CanRead && !g.CanSend && !g.CanOrganize {
			_, e = tx.Exec(ctx, `DELETE FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3`, a.TenantID, g.MailboxID, g.UserID)
		} else {
			_, e = tx.Exec(ctx, `INSERT INTO mailbox_grants(tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only,granted_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(mailbox_id,user_id) DO UPDATE SET can_read=EXCLUDED.can_read,can_organize=EXCLUDED.can_organize,can_send=EXCLUDED.can_send,template_only=EXCLUDED.template_only,granted_by=EXCLUDED.granted_by,updated_at=now()`, a.TenantID, g.MailboxID, g.UserID, g.CanRead, g.CanOrganize, g.CanSend, g.TemplateOnly, a.ID)
		}
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, g.MailboxID); e != nil {
			return e
		}
		return companyAudit(ctx, tx, a, "mailbox.grant", "mailbox", g.MailboxID, map[string]any{"user_id": g.UserID, "read": g.CanRead, "organize": g.CanOrganize, "send": g.CanSend, "template_only": g.TemplateOnly})
	})
}

func (s *PgStore) GetWorkMailbox(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	var v *company.MailboxAccess
	e := s.companyTx(ctx, a, false, func(tx pgx.Tx, a authz.Actor) error {
		var e error
		v, e = s.mailboxAccessTx(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if !v.CanRead && !v.CanSend && !a.IsAdmin && !a.IsSuperAdmin {
			return app.NotFound("mailbox not found")
		}
		return nil
	})
	return v, e
}

// ConvertSharedMailbox is an explicit, audited migration of a legacy mailbox.
// Existing messages become permanent; historical personal ownership is never
// discarded as a side effect of reclassification.
func (s *PgStore) ConvertSharedMailbox(ctx context.Context, actor authz.Actor, id uuid.UUID, revision int64, reason string) error {
	if !meaningfulReason(reason) {
		return app.BadRequest("reason must be 8-1000 bytes")
	}
	return s.companyTx(ctx, actor, true, func(tx pgx.Tx, a authz.Actor) error {
		var kind string
		var owner *uuid.UUID
		var current int64
		if err := tx.QueryRow(ctx, `SELECT mailbox_kind,owner_user_id,lifecycle_revision FROM mailboxes WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, a.TenantID, id).Scan(&kind, &owner, &current); err != nil {
			return app.NotFound("mailbox not found")
		}
		if owner != nil || kind != "legacy" || current != revision {
			return app.Conflict("only an unchanged ownerless legacy mailbox may be converted")
		}
		if _, err := tx.Exec(ctx, `UPDATE mailboxes SET mailbox_kind='shared',access_mode='token',expires_at=NULL,retention_hours_override=0,password_hash=NULL,lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE messages SET expires_at=NULL WHERE mailbox_id=$1 AND deleted_at IS NULL`, id); err != nil {
			return err
		}
		return companyAudit(ctx, tx, a, "mailbox.convert_shared", "mailbox", id, map[string]any{"reason": reason, "revision": revision})
	})
}
