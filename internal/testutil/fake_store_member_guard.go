package testutil

import (
 "context"
 "time"
 "github.com/google/uuid"
 "tabmail/internal/authz"
 "tabmail/internal/models"
 "tabmail/internal/store"
)

func(s *FakeStore)memberActorLocked(actor authz.Actor,tenant uuid.UUID)(authz.Actor,error){
 u:=s.users[actor.ID]
 if actor.Type!=authz.PrincipalUser||actor.TenantID!=tenant||u==nil||!u.IsActive||(u.TenantID!=tenant&&u.Role!=models.RoleSuperAdmin){return actor,authz.ErrForbidden("administrator unavailable")}
 actor.IsAdmin=u.Role==models.RoleAdmin;actor.IsSuperAdmin=u.Role==models.RoleSuperAdmin;return actor,nil
}
func(s *FakeStore)guardRemovalLocked(old,next *models.User)error{
 if !models.IsActiveAdministrator(old)||models.IsActiveAdministrator(next){return nil}
 for _,u:=range s.users{if u.TenantID==old.TenantID&&u.ID!=old.ID&&models.IsActiveAdministrator(u){return nil}}
 return store.ErrLastAdministrator
}
func(s *FakeStore)UpdateUserGuarded(_ context.Context,actor authz.Actor,tenant,target uuid.UUID,patch models.UserAdminPatch)(*models.User,error){
 s.mu.Lock();defer s.mu.Unlock()
 var err error;actor,err=s.memberActorLocked(actor,tenant);if err!=nil{return nil,err}
 old:=s.users[target];if old==nil||old.TenantID!=tenant{return nil,store.ErrMemberNotFound}
 if !authz.CanManageTenantMember(actor,tenant,old.Role){return nil,authz.ErrForbidden("cannot manage this member")}
 next:=*old;patch.Apply(&next)
 if next.Role!=models.RoleUser&&next.Role!=models.RoleAdmin&&next.Role!=models.RoleSuperAdmin{return nil,authz.ErrForbidden("invalid member role")}
 if patch.Role!=nil&&next.Role==models.RoleSuperAdmin&&!actor.IsSuperAdmin{return nil,authz.ErrForbidden("cannot assign super_admin")}
 if err=s.guardRemovalLocked(old,&next);err!=nil{return nil,err}
 next.UpdatedAt=time.Now().UTC();s.users[target]=&next;cp:=next;return &cp,nil
}
func(s *FakeStore)DeleteUserGuarded(_ context.Context,actor authz.Actor,tenant,target uuid.UUID)error{
 s.mu.Lock();defer s.mu.Unlock()
 var err error;actor,err=s.memberActorLocked(actor,tenant);if err!=nil{return err}
 old:=s.users[target];if old==nil||old.TenantID!=tenant{return store.ErrMemberNotFound}
 if actor.ID==target||!authz.CanManageTenantMember(actor,tenant,old.Role){return authz.ErrForbidden("cannot delete this member")}
 if err=s.guardRemovalLocked(old,nil);err!=nil{return err}
 for _,m:=range s.mailboxes{if m.OwnerUserID!=nil&&*m.OwnerUserID==target{return store.ErrMemberOwnsMailbox}}
 for id,k:=range s.apiKeys{if k.OwnerUserID!=nil&&*k.OwnerUserID==target{delete(s.apiKeys,id)}}
 for hash,r:=range s.refreshTokens{if r.UserID==target{delete(s.refreshTokens,hash)}}
 for key,g:=range s.mailboxGrants{if g.UserID==target{delete(s.mailboxGrants,key)}}
 delete(s.users,target);return nil
}
