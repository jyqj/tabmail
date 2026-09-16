package postgres

import (
	"context"
	"github.com/google/uuid"
	"net/mail"
	"strings"
	"tabmail/internal/app"
)

// BootstrapCompanyAdmin never promotes an existing identity. Configuration must
// name an unused login on first install, or the existing bootstrap on restart.
func (s *PgStore) BootstrapCompanyAdmin(ctx context.Context, email, hash string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(hash) < 20 {
		return false, app.BadRequest("invalid bootstrap identity")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('tabmail.company.bootstrap'))`); err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=$1)`, email).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	tenant, user := uuid.New(), uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO tenants(id,name,plan_id,is_super) VALUES($1,$2,'00000000-0000-0000-0000-000000000002',true)`, tenant, email); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,tenant_id,email,password_hash,display_name,role,is_active) VALUES($1,$2,$3,$4,'Administrator','super_admin',true)`, user, tenant, email, hash); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor,action,resource_type,resource_id,details) VALUES($1,$2,'bootstrap','company.bootstrap','user',$3,'{}')`, uuid.New(), tenant, user); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
