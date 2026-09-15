from patchutil import *

write('internal/authz/members.go', r'''
package authz

import (
 "github.com/google/uuid"
 "tabmail/internal/models"
)

// CanManageTenantMember constrains both the selected company and the target
// role. Middleware resolves X-Tenant-ID before constructing the Actor.
func CanManageTenantMember(actor Actor, tenant uuid.UUID, role models.UserRole) bool {
 if tenant == uuid.Nil || actor.TenantID != tenant { return false }
 if actor.IsSuperAdmin { return true }
 return actor.IsAdmin && role == models.RoleUser
}
''')
write('internal/store/members.go', r'''
package store
import "errors"
var (
 ErrLastCompanyAdmin = errors.New("cannot remove the last active company administrator")
 ErrMemberChanged = errors.New("member changed; reload before editing")
 ErrMemberNotFound = errors.New("member not found")
 ErrMemberInUse = errors.New("member owns company resources; transfer them before deletion")
)
''')
write('internal/store/postgres/members.go', r'''
package postgres

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "tabmail/internal/authz"
 "tabmail/internal/models"
 "tabmail/internal/store"
)

// Lock the COMPANY before any target row: locking two different users would
// otherwise allow concurrent demotions to both observe another administrator.
func (s *PgStore) lockMemberMutation(ctx context.Context, tx pgx.Tx, actor authz.Actor, tenant, id uuid.UUID) (*models.User, error) {
 if tenant == uuid.Nil || actor.TenantID != tenant || actor.Type != authz.PrincipalUser { return nil, authz.ErrForbidden("member management denied") }
 var locked uuid.UUID
 if err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&locked); err != nil { return nil, err }
 caller, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1`, actor.ID))
 if err != nil { return nil, err }
 if caller == nil || !caller.IsActive { return nil, authz.ErrForbidden("administrator is inactive") }
 actor.IsSuperAdmin = caller.Role == models.RoleSuperAdmin
 actor.IsAdmin = caller.Role == models.RoleAdmin
 if !actor.IsSuperAdmin && caller.TenantID != tenant { return nil, authz.ErrForbidden("company mismatch") }
 target, err := scanUser(tx.QueryRow(ctx, userSelect+` WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, tenant))
 if err != nil { return nil, err }
 if target == nil { return nil, store.ErrMemberNotFound }
 if !authz.CanManageTenantMember(actor, tenant, target.Role) { return nil, authz.ErrForbidden("cannot manage this member role") }
 return target, nil
}
func activeCompanyAdmin(u *models.User) bool {
 return u.IsActive && (u.Role == models.RoleAdmin || u.Role == models.RoleSuperAdmin)
}
func protectLastAdmin(ctx context.Context, tx pgx.Tx, old *models.User, removes bool) error {
 if !removes || !activeCompanyAdmin(old) { return nil }
 var count int
 if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE tenant_id=$1 AND id<>$2 AND is_active AND role IN ('admin','super_admin')`, old.TenantID, old.ID).Scan(&count); err != nil { return err }
 if count == 0 { return store.ErrLastCompanyAdmin }
 return nil
}

// All admin edits, including display-name-only edits, use this method. The
// expected updated_at prevents a stale full-row object from restoring roles.
func (s *PgStore) DeactivateOrDemoteUserGuarded(ctx context.Context, actor authz.Actor, u *models.User) error {
 tx, err := s.pool.Begin(ctx); if err != nil { return err }; defer tx.Rollback(ctx)
 old, err := s.lockMemberMutation(ctx, tx, actor, u.TenantID, u.ID); if err != nil { return err }
 if !old.UpdatedAt.Equal(u.UpdatedAt) { return store.ErrMemberChanged }
 var role models.UserRole
 if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, actor.ID).Scan(&role); err != nil { return err }
 if u.Role != old.Role && role != models.RoleSuperAdmin { return authz.ErrForbidden("only super admin can change member roles") }
 if u.Role != models.RoleUser && u.Role != models.RoleAdmin && u.Role != models.RoleSuperAdmin { return authz.ErrForbidden("invalid role") }
 if err = protectLastAdmin(ctx, tx, old, !activeCompanyAdmin(u)); err != nil { return err }
 err = tx.QueryRow(ctx, `UPDATE users SET display_name=$3,role=$4,is_active=$5,permission_profile_id=$6,updated_at=clock_timestamp() WHERE id=$1 AND tenant_id=$2 RETURNING updated_at`, u.ID,u.TenantID,u.DisplayName,u.Role,u.IsActive,u.PermissionProfileID).Scan(&u.UpdatedAt)
 if err != nil { return err }
 if _,err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id) VALUES($1,$2,'member.update','user',$3)`, u.TenantID,actor.AuditLabel(),u.ID); err != nil { return err }
 return tx.Commit(ctx)
}
func (s *PgStore) DeleteUserGuarded(ctx context.Context, actor authz.Actor, tenant, id uuid.UUID) error {
 if actor.ID == id { return authz.ErrForbidden("cannot delete yourself") }
 tx, err := s.pool.Begin(ctx); if err != nil { return err }; defer tx.Rollback(ctx)
 old, err := s.lockMemberMutation(ctx, tx, actor, tenant, id); if err != nil { return err }
 if err = protectLastAdmin(ctx, tx, old, true); err != nil { return err }
 if _,err = tx.Exec(ctx, `DELETE FROM tenant_api_keys WHERE owner_user_id=$1`, id); err != nil { return err }
 tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1 AND tenant_id=$2`, id,tenant)
 if err != nil {
  var pe *pgconn.PgError
  if errors.As(err,&pe) && pe.Code=="23503" { return store.ErrMemberInUse }
  return err
 }
 if tag.RowsAffected()!=1 { return store.ErrMemberChanged }
 if _,err = tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,resource_type,resource_id) VALUES($1,$2,'member.delete','user',$3)`, tenant,actor.AuditLabel(),id); err != nil { return err }
 return tx.Commit(ctx)
}
''')
p='internal/api/handlers/users_admin.go'
imports(p,'tabmail/internal/authz','tabmail/internal/store','errors')
replace(p,'type userAdminStore interface {','type userAdminStore interface {\n DeactivateOrDemoteUserGuarded(context.Context, authz.Actor, *models.User) error\n DeleteUserGuarded(context.Context, authz.Actor, uuid.UUID, uuid.UUID) error')
# Both paths recheck target role in the transaction; the early check avoids
# parsing or looking up privileged profile changes for an unauthorized target.
needle='if user == nil || user.TenantID != tenant.ID {\n\t\terrNotFound(w, "user not found")\n\t\treturn\n\t}'
replace(p,needle,needle+'\n actor := middleware.ActorFromContext(r.Context())\n if !authz.CanManageTenantMember(actor, tenant.ID, user.Role) { errForbidden(w, "cannot manage this member role"); return }',2)
replace(p,'h.store.UpdateUser(r.Context(), user)','h.store.DeactivateOrDemoteUserGuarded(r.Context(), actor, user)')
replace(p,'h.store.DeleteUser(r.Context(), userID)','h.store.DeleteUserGuarded(r.Context(), actor, tenant.ID, userID)')
replace(p,'h.logger.Err(err).Msg("update user")\n\t\terrInternal(w)','h.logger.Err(err).Msg("update user")\n  memberMutationError(w, err)')
replace(p,'h.logger.Err(err).Msg("delete user")\n\t\terrInternal(w)','h.logger.Err(err).Msg("delete user")\n  memberMutationError(w, err)')
Path(p).write_text(read(p)+r'''
func memberMutationError(w http.ResponseWriter, err error) {
 switch {
 case authz.IsAuthzError(err): errForbidden(w,err.Error())
 case errors.Is(err,store.ErrMemberNotFound): errNotFound(w,err.Error())
 case errors.Is(err,store.ErrLastCompanyAdmin),errors.Is(err,store.ErrMemberChanged),errors.Is(err,store.ErrMemberInUse): errConflict(w,err.Error())
 default: errInternal(w)
 }
}
''')
# Add the guarded methods to the aggregate store contract too.
p='internal/store/store.go'
replace(p,'UpdateUser(ctx context.Context, u *models.User) error','UpdateUser(ctx context.Context, u *models.User) error\n DeactivateOrDemoteUserGuarded(context.Context, authz.Actor, *models.User) error\n DeleteUserGuarded(context.Context, authz.Actor, uuid.UUID, uuid.UUID) error')

write('internal/store/postgres/migrations/00003_refresh_token_family.sql',r'''
-- +goose Up
ALTER TABLE refresh_tokens ADD COLUMN family_id UUID NOT NULL DEFAULT gen_random_uuid();
-- Existing tokens are independent sessions; never guess their ancestry.
UPDATE refresh_tokens SET family_id=id;
CREATE INDEX refresh_tokens_family ON refresh_tokens(family_id);
-- +goose Down
ALTER TABLE refresh_tokens DROP COLUMN family_id;
''')
p='internal/models/models.go'
replace(p,'type RefreshToken struct {','type RefreshToken struct {\n FamilyID uuid.UUID `json:"-" db:"family_id"`')
# Remove old implementations, retaining invitation helpers in users.go.
p='internal/store/postgres/users.go'
s=read(p); start=s.index('func (s *PgStore) CreateRefreshToken'); end=s.index('// ================================================================\n// Admin invitations',start); Path(p).write_text(s[:start]+s[end:])
write('internal/store/postgres/refresh_tokens.go',r'''
package postgres

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "tabmail/internal/models"
)
const refreshSelect = `SELECT id,user_id,token_hash,expires_at,created_at,revoked_at,family_id FROM refresh_tokens`
func scanRefresh(row pgx.Row) (*models.RefreshToken,error) {
 r:=&models.RefreshToken{}
 err:=row.Scan(&r.ID,&r.UserID,&r.TokenHash,&r.ExpiresAt,&r.CreatedAt,&r.RevokedAt,&r.FamilyID)
 if errors.Is(err,pgx.ErrNoRows) { return nil,nil }; return r,err
}
func (s *PgStore) GetRefreshToken(ctx context.Context, hash string) (*models.RefreshToken,error) { return scanRefresh(s.pool.QueryRow(ctx,refreshSelect+` WHERE token_hash=$1`,hash)) }
func (s *PgStore) CreateRefreshToken(ctx context.Context,r *models.RefreshToken) error {
 if r.ID==uuid.Nil { r.ID=uuid.New() }; if r.FamilyID==uuid.Nil { r.FamilyID=uuid.New() }
 r.CreatedAt=time.Now()
 _,err:=s.pool.Exec(ctx,`INSERT INTO refresh_tokens(id,user_id,token_hash,expires_at,created_at,family_id) VALUES($1,$2,$3,$4,$5,$6)`,r.ID,r.UserID,r.TokenHash,r.ExpiresAt,r.CreatedAt,r.FamilyID); return err
}
// Every rotation, replay and logout locks the family BEFORE the token row.
// A token-row-only lock lets a concurrent descendant escape replay revocation.
func lockRefreshFamily(ctx context.Context,tx pgx.Tx,hash string) (*models.RefreshToken,error) {
 var family uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT family_id FROM refresh_tokens WHERE token_hash=$1`,hash).Scan(&family)
 if errors.Is(err,pgx.ErrNoRows) { return nil,nil }; if err!=nil { return nil,err }
 if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,"refresh-family:"+family.String()); err!=nil { return nil,err }
 return scanRefresh(tx.QueryRow(ctx,refreshSelect+` WHERE token_hash=$1 FOR UPDATE`,hash))
}
func (s *PgStore) RotateRefreshToken(ctx context.Context,oldHash string,next *models.RefreshToken) (bool,bool,error) {
 if next==nil || next.TokenHash=="" || next.TokenHash==oldHash { return false,false,errors.New("invalid replacement refresh token") }
 tx,err:=s.pool.Begin(ctx); if err!=nil { return false,false,err }; defer tx.Rollback(ctx)
 old,err:=lockRefreshFamily(ctx,tx,oldHash); if err!=nil || old==nil { return false,false,err }
 if old.RevokedAt!=nil {
  if _,err=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE family_id=$1 AND revoked_at IS NULL`,old.FamilyID); err!=nil { return false,false,err }
  if err=tx.Commit(ctx); err!=nil { return false,false,err }; return false,true,nil
 }
 var active bool
 err=tx.QueryRow(ctx,`SELECT is_active FROM users WHERE id=$1 FOR SHARE`,old.UserID).Scan(&active)
 if errors.Is(err,pgx.ErrNoRows) { return false,false,nil }; if err!=nil { return false,false,err }; if !active { return false,false,nil }
 tag,err:=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()`,old.ID)
 if err!=nil { return false,false,err }; if tag.RowsAffected()!=1 { return false,false,nil }
 next.ID=uuid.New(); next.FamilyID=old.FamilyID; next.UserID=old.UserID; next.CreatedAt=time.Now()
 _,err=tx.Exec(ctx,`INSERT INTO refresh_tokens(id,user_id,token_hash,expires_at,created_at,family_id) VALUES($1,$2,$3,$4,clock_timestamp(),$5)`,next.ID,next.UserID,next.TokenHash,next.ExpiresAt,next.FamilyID)
 if err!=nil { return false,false,err }; if err=tx.Commit(ctx);err!=nil{return false,false,err}; return true,false,nil
}
// Logout revokes this session's family, including a child rotated just before
// logout acquired the lock. Other independent sessions remain valid.
func (s *PgStore) RevokeRefreshTokenByHash(ctx context.Context,hash string) error {
 tx,err:=s.pool.Begin(ctx); if err!=nil{return err}; defer tx.Rollback(ctx)
 old,err:=lockRefreshFamily(ctx,tx,hash); if err!=nil{return err}; if old==nil{return nil}
 if _,err=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE family_id=$1 AND revoked_at IS NULL`,old.FamilyID);err!=nil{return err}
 return tx.Commit(ctx)
}
func (s *PgStore) RevokeRefreshToken(ctx context.Context,id uuid.UUID) error {
 var hash string
 err:=s.pool.QueryRow(ctx,`SELECT token_hash FROM refresh_tokens WHERE id=$1`,id).Scan(&hash)
 if errors.Is(err,pgx.ErrNoRows){return nil};if err!=nil{return err};return s.RevokeRefreshTokenByHash(ctx,hash)
}
func (s *PgStore) RevokeUserRefreshTokens(ctx context.Context,id uuid.UUID) error {
 // Locking the user blocks new rotations while all existing sessions revoke.
 tx,err:=s.pool.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
 var u uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 FOR UPDATE`,id).Scan(&u);errors.Is(err,pgx.ErrNoRows){return nil}else if err!=nil{return err}
 // Rotation holds its token row before the user row. Avoid lock-order cycles
 // by invalidating with a single statement; PostgreSQL detects any deadlock
 // and the caller must fail closed rather than claiming logout succeeded.
 if _,err=tx.Exec(ctx,`UPDATE refresh_tokens SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL`,id);err!=nil{return err}
 return tx.Commit(ctx)
}
func (s *PgStore) DeleteExpiredRefreshTokens(ctx context.Context) error {
 // Keep replay tombstones for as long as the family has a live descendant.
 _,err:=s.pool.Exec(ctx,`DELETE FROM refresh_tokens r WHERE r.expires_at<now() AND NOT EXISTS (SELECT 1 FROM refresh_tokens c WHERE c.family_id=r.family_id AND c.revoked_at IS NULL AND c.expires_at>now())`);return err
}
''')
for p in ['internal/store/store.go','internal/api/handlers/auth.go']:
 replace(p,'CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error','CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error\n RotateRefreshToken(context.Context, string, *models.RefreshToken) (bool, bool, error)\n RevokeRefreshTokenByHash(context.Context, string) error')
p='internal/api/handlers/auth.go'
function(p,'func (h *AuthHandler) Refresh(',r'''
func (h *AuthHandler) Refresh(w http.ResponseWriter,r *http.Request) {
 var body struct { RefreshToken string `json:"refresh_token"` }; _=decodeBody(r,&body)
 raw:=refreshTokenFromRequest(r,body.RefreshToken)
 if raw=="" { errBadRequest(w,"refresh_token is required");return }
 replacement,hash,err:=authn.GenerateRefreshToken()
 if err!=nil { errInternal(w);return }
 next:=&models.RefreshToken{TokenHash:hash,ExpiresAt:time.Now().Add(authn.RefreshTokenTTL)}
 rotated,_,err:=h.store.RotateRefreshToken(r.Context(),authn.HashToken(raw),next)
 if err!=nil { h.logger.Err(err).Msg("refresh: atomic rotation failed") }
 if err!=nil || !rotated {
  h.clearRefreshCookie(w)
  writeJSON(w,http.StatusUnauthorized,envelope{Error:&apiErr{Code:"UNAUTHORIZED",Message:"invalid or expired refresh token"}});return
 }
 user,err:=h.store.GetUser(r.Context(),next.UserID)
 if err!=nil || user==nil || !user.IsActive {
  h.clearRefreshCookie(w)
  writeJSON(w,http.StatusUnauthorized,envelope{Error:&apiErr{Code:"UNAUTHORIZED",Message:"user not found or inactive"}});return
 }
 // No access token is minted or returned until rotation has COMMITTED.
 token,err:=authn.IssueAccessToken(h.jwtSecret,user)
 if err!=nil { errInternal(w);return }
 h.setRefreshCookie(w,replacement)
 ok(w,map[string]any{"access_token":token,"token_type":"Bearer","expires_in":int(authn.AccessTokenTTL.Seconds())})
}
''')
function(p,'func (h *AuthHandler) Logout(',r'''
func (h *AuthHandler) Logout(w http.ResponseWriter,r *http.Request) {
 var body struct { RefreshToken string `json:"refresh_token"` }; _=decodeBody(r,&body)
 raw:=refreshTokenFromRequest(r,body.RefreshToken)
 var err error
 if raw!="" { err=h.store.RevokeRefreshTokenByHash(r.Context(),authn.HashToken(raw))
 } else if user:=middleware.UserFromCtx(r.Context());user!=nil { err=h.store.RevokeUserRefreshTokens(r.Context(),user.ID) }
 if err!=nil { h.logger.Err(err).Msg("logout: revocation failed");errInternal(w);return }
 h.clearRefreshCookie(w);noContent(w)
}
''')
replace(p,'_ = h.store.RevokeUserRefreshTokens(r.Context(), user.ID)','if err := h.store.RevokeUserRefreshTokens(r.Context(), user.ID); err != nil { h.logger.Err(err).Msg("password changed but session revocation failed"); errInternal(w); return }')
# Migration assertions must now expect v3, not silently leave stale test claims.
p='internal/store/postgres/company_integration_test.go'
replace(p,'want := 2','want := 3')

# FakeStore has real, mutex-protected state and mirrors the transaction policy.
p='internal/testutil/fake_store.go'
replace(p,'type FakeStore struct {','type FakeStore struct {\n refreshTokens map[string]*models.RefreshToken')
p='internal/testutil/fake_store_users.go'
s=read(p); start=s.index('func (s *FakeStore) CreateRefreshToken'); end=s.index('func (s *FakeStore) CreateAdminInvitation',start); Path(p).write_text(s[:start]+s[end:])
imports(p,'time')
replace(p,'cp := *u\n\ts.users[cp.ID] = &cp','if u.CreatedAt.IsZero() { u.CreatedAt=time.Now() }; if u.UpdatedAt.IsZero() { u.UpdatedAt=u.CreatedAt }; cp := *u\n\ts.users[cp.ID] = &cp',2)
write('internal/testutil/fake_store_members_refresh.go',r'''
package testutil
import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "tabmail/internal/authz"
 "tabmail/internal/models"
 "tabmail/internal/store"
)
func (s *FakeStore) memberGuardLocked(actor authz.Actor,tenant,id uuid.UUID)(*models.User,error){
 caller:=s.users[actor.ID]
 if actor.Type!=authz.PrincipalUser || caller==nil || !caller.IsActive || actor.TenantID!=tenant {return nil,authz.ErrForbidden("inactive administrator")}
 actor.IsSuperAdmin=caller.Role==models.RoleSuperAdmin;actor.IsAdmin=caller.Role==models.RoleAdmin
 if !actor.IsSuperAdmin && caller.TenantID!=tenant{return nil,authz.ErrForbidden("company mismatch")}
 old:=s.users[id];if old==nil || old.TenantID!=tenant{return nil,store.ErrMemberNotFound}
 if !authz.CanManageTenantMember(actor,tenant,old.Role){return nil,authz.ErrForbidden("cannot manage this member role")};return old,nil
}
func (s *FakeStore) protectLastAdminLocked(old *models.User,remove bool)error{
 if !remove || !old.IsActive || (old.Role!=models.RoleAdmin && old.Role!=models.RoleSuperAdmin){return nil}
 for _,u:=range s.users {if u.ID!=old.ID && u.TenantID==old.TenantID && u.IsActive && (u.Role==models.RoleAdmin || u.Role==models.RoleSuperAdmin){return nil}}
 return store.ErrLastCompanyAdmin
}
func(s *FakeStore) DeactivateOrDemoteUserGuarded(_ context.Context,a authz.Actor,u *models.User)error{
 s.mu.Lock();defer s.mu.Unlock()
 old,err:=s.memberGuardLocked(a,u.TenantID,u.ID);if err!=nil{return err}
 if !old.UpdatedAt.Equal(u.UpdatedAt){return store.ErrMemberChanged}
 if u.Role!=old.Role && s.users[a.ID].Role!=models.RoleSuperAdmin{return authz.ErrForbidden("only super admin can change member roles")}
 if err=s.protectLastAdminLocked(old,!u.IsActive || (u.Role!=models.RoleAdmin && u.Role!=models.RoleSuperAdmin));err!=nil{return err}
 cp:=*u;cp.UpdatedAt=time.Now();s.users[u.ID]=&cp;u.UpdatedAt=cp.UpdatedAt;return nil
}
func(s *FakeStore) DeleteUserGuarded(_ context.Context,a authz.Actor,tenant,id uuid.UUID)error{
 s.mu.Lock();defer s.mu.Unlock()
 if a.ID==id{return authz.ErrForbidden("cannot delete yourself")}
 old,err:=s.memberGuardLocked(a,tenant,id);if err!=nil{return err}
 if err=s.protectLastAdminLocked(old,true);err!=nil{return err}
 for _,mb:=range s.mailboxes {if mb.OwnerUserID!=nil && *mb.OwnerUserID==id{return store.ErrMemberInUse}}
 delete(s.users,id)
 for hash,rt:=range s.refreshTokens{if rt.UserID==id{delete(s.refreshTokens,hash)}}
 return nil
}
func cloneRefresh(r *models.RefreshToken)*models.RefreshToken{if r==nil{return nil};cp:=*r;if r.RevokedAt!=nil{v:=*r.RevokedAt;cp.RevokedAt=&v};return &cp}
func(s *FakeStore) CreateRefreshToken(_ context.Context,r *models.RefreshToken)error{
 s.mu.Lock();defer s.mu.Unlock();if s.refreshTokens==nil{s.refreshTokens=map[string]*models.RefreshToken{}}
 if _,exists:=s.refreshTokens[r.TokenHash];exists{return errors.New("duplicate refresh token")}
 if r.ID==uuid.Nil{r.ID=uuid.New()};if r.FamilyID==uuid.Nil{r.FamilyID=uuid.New()};r.CreatedAt=time.Now();s.refreshTokens[r.TokenHash]=cloneRefresh(r);return nil
}
func(s *FakeStore) GetRefreshToken(_ context.Context,hash string)(*models.RefreshToken,error){s.mu.Lock();defer s.mu.Unlock();return cloneRefresh(s.refreshTokens[hash]),nil}
func(s *FakeStore) revokeFamilyLocked(family uuid.UUID){now:=time.Now();for _,r:=range s.refreshTokens{if r.FamilyID==family && r.RevokedAt==nil{r.RevokedAt=&now}}}
func(s *FakeStore) RotateRefreshToken(_ context.Context,hash string,next *models.RefreshToken)(bool,bool,error){
 s.mu.Lock();defer s.mu.Unlock()
 if next==nil || next.TokenHash=="" || next.TokenHash==hash{return false,false,errors.New("invalid replacement")}
 old:=s.refreshTokens[hash];if old==nil{return false,false,nil}
 if old.RevokedAt!=nil{s.revokeFamilyLocked(old.FamilyID);return false,true,nil}
 u:=s.users[old.UserID];if !old.ExpiresAt.After(time.Now()) || u==nil || !u.IsActive{return false,false,nil}
 if _,exists:=s.refreshTokens[next.TokenHash];exists{return false,false,errors.New("duplicate refresh token")}
 now:=time.Now();old.RevokedAt=&now;next.ID=uuid.New();next.UserID=old.UserID;next.FamilyID=old.FamilyID;next.CreatedAt=now;s.refreshTokens[next.TokenHash]=cloneRefresh(next);return true,false,nil
}
func(s *FakeStore) RevokeRefreshTokenByHash(_ context.Context,hash string)error{s.mu.Lock();defer s.mu.Unlock();if r:=s.refreshTokens[hash];r!=nil{s.revokeFamilyLocked(r.FamilyID)};return nil}
func(s *FakeStore) RevokeRefreshToken(_ context.Context,id uuid.UUID)error{s.mu.Lock();defer s.mu.Unlock();for _,r:=range s.refreshTokens{if r.ID==id{s.revokeFamilyLocked(r.FamilyID);break}};return nil}
func(s *FakeStore) RevokeUserRefreshTokens(_ context.Context,id uuid.UUID)error{s.mu.Lock();defer s.mu.Unlock();now:=time.Now();for _,r:=range s.refreshTokens{if r.UserID==id && r.RevokedAt==nil{r.RevokedAt=&now}};return nil}
func(s *FakeStore) DeleteExpiredRefreshTokens(_ context.Context)error{s.mu.Lock();defer s.mu.Unlock();now:=time.Now();live:=map[uuid.UUID]bool{};for _,r:=range s.refreshTokens{if r.RevokedAt==nil && r.ExpiresAt.After(now){live[r.FamilyID]=true}};for hash,r:=range s.refreshTokens{if r.ExpiresAt.Before(now)&&!live[r.FamilyID]{delete(s.refreshTokens,hash)}};return nil}
''')
