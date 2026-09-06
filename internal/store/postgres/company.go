package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/enterprise"
	"tabmail/internal/models"
)

var _ enterprise.Repository = (*PgStore)(nil)

type companyQuery interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func companyFrom(ctx context.Context, q companyQuery, tenant uuid.UUID) (*enterprise.Company, error) {
	c := &enterprise.Company{}
	err := q.QueryRow(ctx, `SELECT c.tenant_id,c.primary_zone_id,z.domain,c.name,c.owner_id,c.created_at FROM company_settings c JOIN domain_zones z ON z.id=c.primary_zone_id WHERE c.tenant_id=$1`, tenant).Scan(&c.TenantID, &c.PrimaryZoneID, &c.Domain, &c.Name, &c.OwnerID, &c.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}
func (s *PgStore) GetCompany(ctx context.Context, tenant uuid.UUID) (*enterprise.Company, error) {
	return companyFrom(ctx, s.pool, tenant)
}
func memberFrom(ctx context.Context, q companyQuery, tenant, user uuid.UUID) (*enterprise.Member, error) {
	m := &enterprise.Member{}
	err := q.QueryRow(ctx, `SELECT u.id,u.email,u.display_name,c.company_role,u.is_active,c.daily_send_quota FROM company_members c JOIN users u ON u.tenant_id=c.tenant_id AND u.id=c.user_id WHERE c.tenant_id=$1 AND c.user_id=$2`, tenant, user).Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.Active, &m.DailyQuota)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}
func (s *PgStore) GetCompanyMember(ctx context.Context, tenant, user uuid.UUID) (*enterprise.Member, error) {
	return memberFrom(ctx, s.pool, tenant, user)
}
func grantFrom(ctx context.Context, q companyQuery, tenant, user, mailbox uuid.UUID) (*enterprise.Grant, error) {
	g := &enterprise.Grant{}
	err := q.QueryRow(ctx, `SELECT g.mailbox_id,g.user_id,m.full_address,g.can_read,g.can_organize,g.can_send,g.template_only FROM company_mailbox_grants g JOIN mailboxes m ON m.id=g.mailbox_id AND m.tenant_id=g.tenant_id WHERE g.tenant_id=$1 AND g.user_id=$2 AND g.mailbox_id=$3`, tenant, user, mailbox).Scan(&g.MailboxID, &g.UserID, &g.Address, &g.Read, &g.Organize, &g.Send, &g.TemplateOnly)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return g, err
}
func (s *PgStore) GetCompanyGrant(ctx context.Context, tenant, user, mailbox uuid.UUID) (*enterprise.Grant, error) {
	return grantFrom(ctx, s.pool, tenant, user, mailbox)
}
func lockCompany(ctx context.Context, q companyQuery, tenant uuid.UUID) error {
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,77441))`, tenant.String())
	return err
}
func companyAdmin(ctx context.Context, q companyQuery, tenant, actor uuid.UUID) (*enterprise.Company, error) {
	c, err := companyFrom(ctx, q, tenant)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, app.NotFound("company not configured")
	}
	m, err := memberFrom(ctx, q, tenant, actor)
	if err != nil {
		return nil, err
	}
	if m == nil || !m.Active || m.Role != "admin" {
		return nil, app.Forbidden("company administrator required")
	}
	return c, nil
}
func auditCompany(ctx context.Context, q companyQuery, tenant, actor uuid.UUID, action string, resource uuid.UUID, detail any) error {
	data, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,$3,'company',$4,$5)`, tenant, "user:"+actor.String(), action, resource, data)
	return err
}

func (s *PgStore) EnableCompany(ctx context.Context, tenant, actor, zone uuid.UUID, name string) (*enterprise.Company, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, app.BadRequest("company name required (max 120 bytes)")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND tenant_id=$2 AND is_active AND role IN ('admin','super_admin'))`, actor, tenant).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, app.Forbidden("administrator in the target company required")
	}
	c, err := companyFrom(ctx, tx, tenant)
	if err != nil {
		return nil, err
	}
	if c != nil {
		if c.PrimaryZoneID != zone {
			return nil, app.Conflict("company is already configured with a different primary domain")
		}
		return c, nil
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM domain_zones WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, app.Conflict("company activation requires exactly one registered domain; migrate other assets first")
	}
	var domain string
	if err = tx.QueryRow(ctx, `SELECT domain FROM domain_zones WHERE tenant_id=$1 AND id=$2 AND is_verified AND mx_verified`, tenant, zone).Scan(&domain); err == pgx.ErrNoRows {
		return nil, app.BadRequest("verified primary domain required")
	}
	if err != nil {
		return nil, err
	}
	var incompatible bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailboxes WHERE tenant_id=$1 AND resolved_domain<>$2)`, tenant, domain).Scan(&incompatible); err != nil {
		return nil, err
	}
	if incompatible {
		return nil, app.Conflict("migrate subdomain mailboxes before company activation")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_settings(tenant_id,primary_zone_id,name,owner_id) VALUES($1,$2,$3,$4)`, tenant, zone, name, actor); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_members(tenant_id,user_id,company_role,daily_send_quota) SELECT tenant_id,id,CASE WHEN id=$2 THEN 'admin' ELSE 'viewer' END,500 FROM users WHERE tenant_id=$1`, tenant, actor); err != nil {
		return nil, err
	}
	// Imported viewers must not retain legacy platform/tenant administrator bypasses.
	if _, err = tx.Exec(ctx, `UPDATE users SET role='user' WHERE tenant_id=$1 AND id<>$2`, tenant, actor); err != nil {
		return nil, err
	}
	// Default-deny all old mailboxes: public/password access is removed, but mail is retained.
	for _, sql := range []string{
		`UPDATE domain_zones SET visibility='private',allow_random_subdomains=FALSE WHERE tenant_id=$1`,
		`UPDATE domain_routes SET auto_create_mailbox=FALSE WHERE zone_id IN (SELECT id FROM domain_zones WHERE tenant_id=$1)`,
		`UPDATE mailboxes SET access_mode='api_key',password_hash=NULL,expires_at=NULL WHERE tenant_id=$1`,
		`UPDATE messages SET retention_exempt=TRUE WHERE tenant_id=$1`,
		`DELETE FROM tenant_api_keys WHERE tenant_id=$1`,
		`UPDATE outbound_jobs SET state='dead',delivery_token=NULL,last_error='company activation: reauthorization required' WHERE tenant_id=$1 AND state IN ('pending','retry','processing')`,
	} {
		if _, err = tx.Exec(ctx, sql, tenant); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO system_settings(key,value,description) VALUES('open_registration','false','Disabled by company activation') ON CONFLICT(key) DO UPDATE SET value='false'`); err != nil {
		return nil, err
	}
	if err = auditCompany(ctx, tx, tenant, actor, "company.enabled", tenant, map[string]any{"domain": domain, "old_mailboxes": "unassigned; retained"}); err != nil {
		return nil, err
	}
	c, err = companyFrom(ctx, tx, tenant)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
func (s *PgStore) ListCompanyMembers(ctx context.Context, tenant uuid.UUID) ([]enterprise.Member, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.email,u.display_name,c.company_role,u.is_active,c.daily_send_quota FROM company_members c JOIN users u ON u.tenant_id=c.tenant_id AND u.id=c.user_id WHERE c.tenant_id=$1 ORDER BY u.email`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []enterprise.Member{}
	for rows.Next() {
		var m enterprise.Member
		if err = rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.Active, &m.DailyQuota); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *PgStore) ProvisionCompanyMember(ctx context.Context, tenant, actor uuid.UUID, p enterprise.Provision, tokenHash string) (*enterprise.Member, error) {
	p.LocalPart = strings.ToLower(strings.TrimSpace(p.LocalPart))
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if !enterprise.ValidLocalPart(p.LocalPart) || !enterprise.ValidRole(p.Role) || p.DisplayName == "" || len(p.DisplayName) > 120 || p.DailyQuota < 1 || p.DailyQuota > 10000 || len(tokenHash) != 64 {
		return nil, app.BadRequest("invalid employee fields")
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
	if p.Role == "admin" && actor != c.OwnerID {
		return nil, app.Forbidden("only company owner can appoint administrators")
	}
	if err = checkCompanyMailboxQuota(ctx, tx, tenant); err != nil {
		return nil, err
	}
	address := p.LocalPart + "@" + c.Domain
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=$1) OR EXISTS(SELECT 1 FROM mailboxes WHERE full_address=$1)`, address).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, app.Conflict("employee or mailbox already exists; assign existing assets explicitly")
	}
	uid, mid := uuid.New(), uuid.New()
	role := "user"
	if p.Role == "admin" {
		role = "admin"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,tenant_id,email,password_hash,display_name,role,is_active) VALUES($1,$2,$3,'!pending-activation',$4,$5,FALSE)`, uid, tenant, address, p.DisplayName, role); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_members(tenant_id,user_id,company_role,daily_send_quota) VALUES($1,$2,$3,$4)`, tenant, uid, p.Role, p.DailyQuota); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode) VALUES($1,$2,$3,$4,$5,$6,'api_key')`, mid, tenant, c.PrimaryZoneID, p.LocalPart, c.Domain, address); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_mailbox_grants(tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only) VALUES($1,$2,$3,TRUE,$4,$4,$5)`, tenant, mid, uid, p.Role != "viewer", p.Role == "restricted"); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_invitations(token_hash,tenant_id,user_id,expires_at) VALUES($1,$2,$3,now()+interval '72 hours')`, tokenHash, tenant, uid); err != nil {
		return nil, err
	}
	if err = auditCompany(ctx, tx, tenant, actor, "employee.provisioned", uid, map[string]any{"address": address, "role": p.Role}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &enterprise.Member{UserID: uid, Email: address, DisplayName: p.DisplayName, Role: p.Role, DailyQuota: p.DailyQuota}, nil
}
func (s *PgStore) ActivateCompanyMember(ctx context.Context, tokenHash, passwordHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize with suspension/re-invitation before consuming a single-use token.
	var tenant, user uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT tenant_id,user_id FROM company_invitations WHERE token_hash=$1`, tokenHash).Scan(&tenant, &user); err == pgx.ErrNoRows {
		return app.BadRequest("invalid or expired activation token")
	}
	if err != nil {
		return err
	}
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return err
	}
	var claimed uuid.UUID
	if err = tx.QueryRow(ctx, `UPDATE company_invitations SET consumed_at=now() WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>now() RETURNING user_id`, tokenHash).Scan(&claimed); err == pgx.ErrNoRows {
		return app.BadRequest("invalid or expired activation token")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,is_active=TRUE,updated_at=now() WHERE id=$1 AND tenant_id=$3`, user, passwordHash, tenant); err != nil {
		return err
	}
	if err = auditCompany(ctx, tx, tenant, user, "employee.activated", user, map[string]string{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PgStore) UpdateCompanyMember(ctx context.Context, tenant, actor, user uuid.UUID, role string, active bool, quota int) error {
	if !enterprise.ValidRole(role) || quota < 1 || quota > 10000 {
		return app.BadRequest("invalid employee role or quota")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return err
	}
	c, err := companyAdmin(ctx, tx, tenant, actor)
	if err != nil {
		return err
	}
	m, err := memberFrom(ctx, tx, tenant, user)
	if err != nil {
		return err
	}
	if m == nil {
		return app.NotFound("employee not found")
	}
	if user == c.OwnerID || user == actor {
		return app.Forbidden("use a separate administrator; company owner cannot be disabled or demoted")
	}
	if (m.Role == "admin" || role == "admin") && actor != c.OwnerID {
		return app.Forbidden("only owner can manage administrators")
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND password_hash='!pending-activation')`, user).Scan(&pending); err != nil {
		return err
	}
	if active && pending {
		return app.Conflict("employee must activate their account first")
	}
	dbrole := "user"
	if role == "admin" {
		dbrole = "admin"
	}
	if _, err = tx.Exec(ctx, `UPDATE company_members SET company_role=$3,daily_send_quota=$4 WHERE tenant_id=$1 AND user_id=$2`, tenant, user, role, quota); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET role=$3,is_active=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, user, dbrole, active); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, user); err != nil {
		return err
	}
	if !active {
		for _, sql := range []string{`DELETE FROM tenant_api_keys WHERE owner_user_id=$1`, `UPDATE company_invitations SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL`, `UPDATE outbound_jobs SET state='dead',delivery_token=NULL,last_error='employee suspended' WHERE user_id=$1 AND state IN ('pending','retry','processing')`} {
			if _, err = tx.Exec(ctx, sql, user); err != nil {
				return err
			}
		}
	}
	if err = auditCompany(ctx, tx, tenant, actor, "employee.updated", user, map[string]any{"role": role, "active": active, "quota": quota}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PgStore) CreateCompanyMailbox(ctx context.Context, tenant, actor uuid.UUID, local string) (*models.Mailbox, error) {
	local = strings.ToLower(strings.TrimSpace(local))
	if !enterprise.ValidLocalPart(local) {
		return nil, app.BadRequest("invalid mailbox name")
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
	if err = checkCompanyMailboxQuota(ctx, tx, tenant); err != nil {
		return nil, err
	}
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: c.PrimaryZoneID, LocalPart: local, ResolvedDomain: c.Domain, FullAddress: local + "@" + c.Domain, AccessMode: models.AccessAPIKey, CreatedAt: time.Now().UTC()}
	_, err = tx.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode) VALUES($1,$2,$3,$4,$5,$6,'api_key') ON CONFLICT(full_address) DO NOTHING`, mb.ID, tenant, c.PrimaryZoneID, local, c.Domain, mb.FullAddress)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM mailboxes WHERE full_address=$1`, mb.FullAddress).Scan(&id); err != nil {
		return nil, err
	}
	if id != mb.ID {
		return nil, app.Conflict("mailbox already exists")
	}
	if err = auditCompany(ctx, tx, tenant, actor, "mailbox.provisioned", mb.ID, map[string]string{"address": mb.FullAddress}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return mb, nil
}
func (s *PgStore) SetCompanyGrant(ctx context.Context, tenant, actor uuid.UUID, g enterprise.Grant) error {
	if g.Organize && !g.Read {
		return app.BadRequest("organize requires read")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return err
	}
	c, err := companyAdmin(ctx, tx, tenant, actor)
	if err != nil {
		return err
	}
	if g.UserID == actor && actor != c.OwnerID {
		return app.Forbidden("administrators cannot grant themselves mailbox access")
	}
	if err = tx.QueryRow(ctx, `SELECT full_address FROM mailboxes WHERE tenant_id=$1 AND id=$2 AND zone_id=$3`, tenant, g.MailboxID, c.PrimaryZoneID).Scan(&g.Address); err != nil {
		if err == pgx.ErrNoRows {
			return app.NotFound("company mailbox not found")
		}
		return err
	}
	m, err := memberFrom(ctx, tx, tenant, g.UserID)
	if err != nil {
		return err
	}
	if m == nil {
		return app.NotFound("employee not found")
	}
	if m.Role == "viewer" && (g.Send || g.Organize) {
		return app.BadRequest("viewer cannot receive send or organize capabilities")
	}
	_, err = tx.Exec(ctx, `INSERT INTO company_mailbox_grants(tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,mailbox_id,user_id) DO UPDATE SET can_read=$4,can_organize=$5,can_send=$6,template_only=$7`, tenant, g.MailboxID, g.UserID, g.Read, g.Organize, g.Send, g.TemplateOnly)
	if err != nil {
		return err
	}
	if err = auditCompany(ctx, tx, tenant, actor, "mailbox.grant.updated", g.MailboxID, g); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PgStore) ListCompanyGrants(ctx context.Context, tenant, user uuid.UUID, admin bool) ([]enterprise.Grant, error) {
	if admin {
		if _, err := companyAdmin(ctx, s.pool, tenant, user); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT g.mailbox_id,g.user_id,m.full_address,g.can_read,g.can_organize,g.can_send,g.template_only FROM company_mailbox_grants g JOIN mailboxes m ON m.tenant_id=g.tenant_id AND m.id=g.mailbox_id WHERE g.tenant_id=$1 AND ($3 OR g.user_id=$2) ORDER BY m.full_address,g.user_id`, tenant, user, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []enterprise.Grant{}
	for rows.Next() {
		var g enterprise.Grant
		if err = rows.Scan(&g.MailboxID, &g.UserID, &g.Address, &g.Read, &g.Organize, &g.Send, &g.TemplateOnly); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *PgStore) ListCompanyMailboxes(ctx context.Context, tenant, user uuid.UUID, p models.Page) ([]*models.Mailbox, int, error) {
	p = p.Normalize()
	predicate := `m.tenant_id=$1 AND EXISTS(SELECT 1 FROM company_mailbox_grants g JOIN users u ON u.id=g.user_id AND u.tenant_id=g.tenant_id WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=$2 AND g.can_read AND u.is_active)`
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mailboxes m WHERE `+predicate, tenant, user).Scan(&n); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, mailboxSelect+` WHERE `+predicate+` ORDER BY m.full_address LIMIT $3 OFFSET $4`, tenant, user, p.PerPage, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*models.Mailbox{}
	for rows.Next() {
		m, err := s.scanMailbox(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, n, rows.Err()
}

// Reissue is explicit and invalidates any previous pending invitation. It cannot
// be used as a password reset or to reactivate an account which already activated.
func (s *PgStore) ReissueCompanyInvitation(ctx context.Context, tenant, actor, user uuid.UUID, hash string) error {
	if len(hash) != 64 {
		return app.BadRequest("invalid token hash")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockCompany(ctx, tx, tenant); err != nil {
		return err
	}
	c, err := companyAdmin(ctx, tx, tenant, actor)
	if err != nil {
		return err
	}
	m, err := memberFrom(ctx, tx, tenant, user)
	if err != nil {
		return err
	}
	if m == nil {
		return app.NotFound("employee not found")
	}
	if m.Role == "admin" && actor != c.OwnerID {
		return app.Forbidden("only owner can invite administrators")
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2 AND NOT is_active AND password_hash='!pending-activation')`, tenant, user).Scan(&pending); err != nil {
		return err
	}
	if !pending {
		return app.Conflict("account is already activated")
	}
	if _, err = tx.Exec(ctx, `UPDATE company_invitations SET consumed_at=now() WHERE tenant_id=$1 AND user_id=$2 AND consumed_at IS NULL`, tenant, user); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO company_invitations(token_hash,tenant_id,user_id,expires_at) VALUES($1,$2,$3,now()+interval '72 hours')`, hash, tenant, user); err != nil {
		return err
	}
	if err = auditCompany(ctx, tx, tenant, actor, "employee.invitation.reissued", user, map[string]string{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func checkCompanyMailboxQuota(ctx context.Context, q companyQuery, tenant uuid.UUID) error {
	var limit, count int
	err := q.QueryRow(ctx, `SELECT COALESCE(o.max_mailboxes_per_domain,p.max_mailboxes_per_domain),(SELECT count(*) FROM mailboxes WHERE tenant_id=t.id) FROM tenants t JOIN plans p ON p.id=t.plan_id LEFT JOIN tenant_overrides o ON o.tenant_id=t.id WHERE t.id=$1`, tenant).Scan(&limit, &count)
	if err != nil {
		return err
	}
	if limit > 0 && count >= limit {
		return app.Forbidden("company mailbox capacity reached")
	}
	return nil
}
