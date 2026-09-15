#!/usr/bin/env python3
"""One-use, assertion-checked workspace patch. Removed before publication."""
from pathlib import Path
import re

ROOT=Path('.')
def edit(path,old,new,count=1):
 p=ROOT/path;s=p.read_text()
 if s.count(old)!=count:raise RuntimeError(f'{path}: expected {count} matches, got {s.count(old)}: {old[:100]!r}')
 p.write_text(s.replace(old,new))
def span(s,sig):
 a=s.index(sig);b=s.index('{',a);level=0
 tokens=re.compile(r'`[^`]*`|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|//[^\n]*|/\*.*?\*/|[{}]',re.S)
 for m in tokens.finditer(s,b):
  if m.group()=='{':level+=1
  elif m.group()=='}':
   level-=1
   if level==0:return a,m.end()
 raise RuntimeError(sig)
def changefunc(path,sig,transform):
 p=ROOT/path;s=p.read_text();a,b=span(s,sig);p.write_text(s[:a]+transform(s[a:b])+s[b:])
def setfunc(path,sig,body):changefunc(path,sig,lambda _:body)

# A: no unguarded full-row update remains in either admin mutation endpoint.
edit('internal/store/store.go','type Store interface {','type Store interface {\n MemberGuardStore\n RefreshRotationStore')
edit('internal/api/handlers/users_admin.go','"tabmail/internal/models"','"tabmail/internal/models"\n "tabmail/internal/authz"\n "tabmail/internal/store"')
edit('internal/api/handlers/users_admin.go','type userAdminStore interface {','type userAdminStore interface {\n store.MemberGuardStore')
def update_member(s):
 s=s.replace('\n\tvar req struct {','\n actor := middleware.ActorFromContext(r.Context())\n if !authz.CanManageTenantMember(actor,tenant.ID,user.Role) { errForbidden(w,"cannot manage this member role");return }\n patch:=models.UserAdminPatch{}\n\tvar req struct {',1)
 s=s.replace('user.Role = newRole','patch.Role = &newRole').replace('user.IsActive = *req.IsActive','patch.IsActive = req.IsActive').replace('user.DisplayName = *req.DisplayName','patch.DisplayName = req.DisplayName')
 s=s.replace('if req.PermissionProfileID != nil {','if req.PermissionProfileID != nil {\n patch.SetPermissionProfile=true',1)
 s=s.replace('user.PermissionProfileID = nil','patch.PermissionProfileID = nil').replace('user.PermissionProfileID = &profileID','patch.PermissionProfileID = &profileID')
 old='if err := h.store.UpdateUser(r.Context(), user); err != nil {\n\t\th.logger.Err(err).Msg("update user")\n\t\terrInternal(w)\n\t\treturn\n\t}'
 assert old in s
 return s.replace(old,'updated,err:=h.store.UpdateUserGuarded(r.Context(),actor,tenant.ID,userID,patch)\n if err!=nil {h.writeMemberError(w,err);return}\n user=updated')
changefunc('internal/api/handlers/users_admin.go','func (h *UserAdminHandler) UpdateUserByAdmin',update_member)
def delete_member(s):
 old='if err := h.store.DeleteUser(r.Context(), userID); err != nil {\n\t\th.logger.Err(err).Msg("delete user")\n\t\terrInternal(w)\n\t\treturn\n\t}'
 assert old in s
 return s.replace(old,'actor:=middleware.ActorFromContext(r.Context())\n if !authz.CanManageTenantMember(actor,tenant.ID,user.Role){errForbidden(w,"cannot manage this member role");return}\n if err:=h.store.DeleteUserGuarded(r.Context(),actor,tenant.ID,userID);err!=nil{h.writeMemberError(w,err);return}')
changefunc('internal/api/handlers/users_admin.go','func (h *UserAdminHandler) DeleteUserByAdmin',delete_member)

# D: one atomic consume/insert; neither a new family nor a second token pair.
edit('internal/models/models.go','type RefreshToken struct {','type RefreshToken struct {\n FamilyID uuid.UUID `json:"-" db:"family_id"`')
edit('internal/api/handlers/auth.go','"tabmail/internal/models"','"tabmail/internal/models"\n "tabmail/internal/store"')
edit('internal/api/handlers/auth.go','type authStore interface {','type authStore interface {\n store.RefreshRotationStore')
setfunc('internal/api/handlers/auth.go','func (h *AuthHandler) Refresh',r'''func (h *AuthHandler) Refresh(w http.ResponseWriter,r *http.Request){
 var req struct{RefreshToken string `json:"refresh_token"`}
 _=decodeBody(r,&req)
 raw:=refreshTokenFromRequest(r,req.RefreshToken)
 if raw==""{errBadRequest(w,"refresh_token is required");return}
 nextRaw,nextHash,err:=authn.GenerateRefreshToken()
 if err!=nil{errInternal(w);return}
 next:=&models.RefreshToken{TokenHash:nextHash,ExpiresAt:time.Now().Add(authn.RefreshTokenTTL)}
 rotated,familyRevoked,err:=h.store.RotateRefreshToken(r.Context(),authn.HashToken(raw),next)
 if err!=nil||!rotated{
  if err!=nil{h.logger.Error().Err(err).Msg("refresh: atomic rotation failed")}
  if familyRevoked{h.logger.Warn().Msg("refresh: replay revoked token family")}
  h.clearRefreshCookie(w)
  writeJSON(w,http.StatusUnauthorized,envelope{Error:&apiErr{Code:"UNAUTHORIZED",Message:"invalid or expired refresh token"}});return
 }
 user,err:=h.store.GetUser(r.Context(),next.UserID)
 if err!=nil||user==nil||!user.IsActive{h.clearRefreshCookie(w);writeJSON(w,http.StatusUnauthorized,envelope{Error:&apiErr{Code:"UNAUTHORIZED",Message:"user unavailable"}});return}
 access,err:=authn.IssueAccessToken(h.jwtSecret,user)
 if err!=nil{errInternal(w);return}
 h.setRefreshCookie(w,nextRaw)
 ok(w,map[string]any{"access_token":access,"token_type":"Bearer","expires_in":int(authn.AccessTokenTTL.Seconds())})
}''')
setfunc('internal/api/handlers/auth.go','func (h *AuthHandler) Logout',r'''func (h *AuthHandler) Logout(w http.ResponseWriter,r *http.Request){
 var req struct{RefreshToken string `json:"refresh_token"`}
 _=decodeBody(r,&req)
 raw:=refreshTokenFromRequest(r,req.RefreshToken)
 var err error
 if raw!=""{err=h.store.RevokeRefreshTokenByHash(r.Context(),authn.HashToken(raw))}else if user:=middleware.UserFromCtx(r.Context());user!=nil{err=h.store.RevokeUserRefreshTokens(r.Context(),user.ID)}
 if err!=nil{h.logger.Error().Err(err).Msg("logout: revocation failed");errInternal(w);return}
 h.clearRefreshCookie(w);noContent(w)
}''')
# The ordinary login/invitation token insertion establishes a fresh family.
def create_refresh(s):
 s=s.replace('rt.CreatedAt = time.Now()','if rt.FamilyID==uuid.Nil{rt.FamilyID=rt.ID}\n rt.CreatedAt = time.Now()')
 s=s.replace('expires_at, created_at)','expires_at, created_at, family_id)').replace('VALUES ($1, $2, $3, $4, $5)','VALUES ($1, $2, $3, $4, $5, $6)').replace('rt.ExpiresAt, rt.CreatedAt)','rt.ExpiresAt, rt.CreatedAt, rt.FamilyID)')
 return s
changefunc('internal/store/postgres/users.go','func (s *PgStore) CreateRefreshToken',create_refresh)
changefunc('internal/store/postgres/users.go','func (s *PgStore) GetRefreshToken',lambda s:s.replace('created_at, revoked_at','created_at, revoked_at, family_id').replace('&rt.CreatedAt, &rt.RevokedAt)','&rt.CreatedAt, &rt.RevokedAt, &rt.FamilyID)'))
changefunc('internal/store/postgres/users.go','func (s *PgStore) RevokeRefreshToken',lambda s:s.replace('WHERE id = $1`','WHERE id = $1 AND revoked_at IS NULL`'))
edit('internal/testutil/fake_store.go','type FakeStore struct {','type FakeStore struct {\n refreshTokens map[string]*models.RefreshToken')
p=ROOT/'internal/testutil/fake_store_users.go';s=p.read_text();a=s.index('func (s *FakeStore) CreateRefreshToken');b=s.index('func (s *FakeStore) CreateAdminInvitation',a);p.write_text(s[:a]+s[b:])

# B: explicit presentation boundary, including errors/attempts and BCC aggregates.
edit('internal/models/models.go','`json:"delivery_token,omitempty" db:"delivery_token"`','`json:"-" db:"delivery_token"`')
edit('internal/models/models.go','type OutboundJob struct {','type OutboundJob struct {\n ContentRedacted bool `json:"content_redacted"`')
edit('internal/authz/authz.go','type OwnerListFilter struct {','type OwnerListFilter struct {\n ReaderUserID *uuid.UUID\n AllowedZoneIDs []uuid.UUID')
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) GetJob',lambda s:s.replace('ok(w, job)','h.writeOutboundView(w,r,job)'))
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) ListJobs',lambda s:s.replace('okList(w, items, total, pg.Page, pg.PerPage)','for i,job:=range items{view,viewErr:=h.redactOutboundJob(ctx,middleware.ActorFromContext(ctx),job);if viewErr!=nil{errInternal(w);return};items[i]=view}\n okList(w, items, total, pg.Page, pg.PerPage)'))
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) RetryJob',lambda s:s.replace('if err := h.outbound.ValidateJobAuthorization','if err:=h.authorizeOutboundRetry(ctx,job);err!=nil{if authz.IsAuthzError(err){errForbidden(w,err.Error())}else{errInternal(w)};return}\n if err := h.outbound.ValidateJobAuthorization').replace('ok(w, updatedJob)','h.writeOutboundView(w,r,updatedJob)'))
def attempts(s):
 s=s.replace('if _, err := h.getAccessibleOutboundJob(ctx, jobID); err != nil {','job, err := h.getAccessibleOutboundJob(ctx, jobID)\n if err != nil {')
 s=s.replace('ok(w, attempts)','allowed,err:=h.outboundContentAllowed(ctx,middleware.ActorFromContext(ctx),job);if err!=nil{errInternal(w);return}\n if !allowed{for i,a:=range attempts{cp:=*a;if cp.Error!=""{cp.Error="Delivery details restricted"};if cp.SMTPResponse!=""{cp.SMTPResponse="Protocol response restricted"};attempts[i]=&cp}}\n ok(w, attempts)')
 return s
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) ListAttempts',attempts)
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) getAccessibleOutboundJob',lambda s:s.replace('return nil, errOutboundJobNotFound\n\t}', 'allowed,err:=h.outboundContentAllowed(ctx,middleware.ActorFromContext(ctx),job);if err!=nil{return nil,err};if !allowed{return nil,errOutboundJobNotFound}\n\t}'))
changefunc('internal/api/handlers/outbound.go','func (h *OutboundHandler) listAccessibleOutboundJobs',lambda s:s.replace('return h.store.ListOutboundJobsScoped','actor:=middleware.ActorFromContext(ctx)\n scope.ReaderUserID=actor.EffectiveUserID()\n if actor.Permission!=nil{scope.AllowedZoneIDs=actor.Permission.AllowedZoneIDs}\n return h.store.ListOutboundJobsScoped'))
def outbound_scope(s):
 marker='\twhereSQL := strings.Join(where, " AND ")'
 addition=r'''
 if scope.ReaderUserID!=nil&&!scope.AllInTenant{
  n++;u:="$"+strconv.Itoa(n);args=append(args,*scope.ReaderUserID)
  shared:=`sender_mailbox_id IN (SELECT m.id FROM mailboxes m WHERE m.tenant_id=$1 AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND (m.owner_user_id=`+u+` OR EXISTS(SELECT 1 FROM mailbox_grants g WHERE g.tenant_id=m.tenant_id AND g.mailbox_id=m.id AND g.user_id=`+u+` AND g.can_read)))`
  where[len(where)-1]="("+where[len(where)-1]+" OR "+shared+")"
 }
 if len(scope.AllowedZoneIDs)>0{n++;where=append(where,"zone_id=ANY($"+strconv.Itoa(n)+")");args=append(args,scope.AllowedZoneIDs)}
'''
 assert marker in s;return s.replace(marker,addition+marker)
changefunc('internal/store/postgres/outbound.go','func (s *PgStore) ListOutboundJobsScoped',outbound_scope)
def fake_scope(s):
 s=s.replace('switch {','if !models.ZoneAllowed(scope.AllowedZoneIDs,job.ZoneID){return false}\n if scope.ReaderUserID!=nil&&job.SenderMailboxID!=nil{mb:=s.mailboxes[*job.SenderMailboxID];if mb!=nil&&mb.TenantID==scope.TenantID&&(mb.ExpiresAt==nil||mb.ExpiresAt.After(time.Now())){g:=s.mailboxGrants[[2]uuid.UUID{mb.ID,*scope.ReaderUserID}];if (mb.OwnerUserID!=nil&&*mb.OwnerUserID==*scope.ReaderUserID)||(g!=nil&&g.CanRead&&g.TenantID==scope.TenantID){return true}}}\n switch {',1)
 return s
changefunc('internal/testutil/fake_store_outbound.go','func (s *FakeStore) ListOutboundJobsScoped',fake_scope)
print('Applied A, B, D workspace changes')
