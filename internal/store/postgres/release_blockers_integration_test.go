package postgres_test

import (
 "context"
 "errors"
 "sync"
 "testing"
 "time"
 "github.com/google/uuid"
 "tabmail/internal/authz"
 "tabmail/internal/models"
 "tabmail/internal/store"
 "tabmail/internal/store/postgres"
 "tabmail/internal/testpg"
)

func rbPGTenant(t *testing.T,st *postgres.PgStore)*models.Tenant{
 t.Helper();v:=&models.Tenant{Name:uuid.NewString(),PlanID:uuid.MustParse("00000000-0000-0000-0000-000000000001")};must(t,st.CreateTenant(context.Background(),v));return v
}
func rbPGUser(t *testing.T,st *postgres.PgStore,tenant uuid.UUID,role models.UserRole)*models.User{
 t.Helper();u:=&models.User{TenantID:tenant,Email:uuid.NewString()+"@company.test",PasswordHash:"test",Role:role,IsActive:true};must(t,st.CreateUser(context.Background(),u));return u
}
func rbPGActor(u *models.User,tenant uuid.UUID)authz.Actor{return authz.Actor{Type:authz.PrincipalUser,ID:u.ID,TenantID:tenant,Role:u.Role,IsSuperAdmin:u.Role==models.RoleSuperAdmin,IsAdmin:u.Role==models.RoleAdmin}}
func rbNewRefresh(user uuid.UUID)*models.RefreshToken{return &models.RefreshToken{UserID:user,TokenHash:uuid.NewString(),ExpiresAt:time.Now().Add(time.Hour)}}

func TestReleasePostgresLastAdminConcurrent(t *testing.T){
 for _,mode:=range []string{"disable","demote","delete"}{t.Run(mode,func(t *testing.T){
  st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);operatorCompany:=rbPGTenant(t,st);operator:=rbPGUser(t,st,operatorCompany.ID,models.RoleSuperAdmin)
  members:=[]*models.User{rbPGUser(t,st,company.ID,models.RoleAdmin),rbPGUser(t,st,company.ID,models.RoleAdmin)};actor:=rbPGActor(operator,company.ID)
  start:=make(chan struct{});results:=make(chan error,2);var wg sync.WaitGroup
  for _,u:=range members{wg.Add(1);go func(id uuid.UUID){defer wg.Done();<-start
   if mode=="delete"{results<-st.DeleteUserGuarded(ctx,actor,company.ID,id);return}
   inactive:=false;role:=models.RoleUser;patch:=models.UserAdminPatch{}
   if mode=="disable"{patch.IsActive=&inactive}else{patch.Role=&role}
   _,err:=st.UpdateUserGuarded(ctx,actor,company.ID,id,patch);results<-err
  }(u.ID)}
  close(start);wg.Wait();close(results);success,blocked:=0,0
  for err:=range results{if err==nil{success++}else if errors.Is(err,store.ErrLastAdministrator){blocked++}else{t.Fatal(err)}}
  var count int;must(t,pool.QueryRow(ctx,`SELECT count(*) FROM users WHERE tenant_id=$1 AND is_active AND role IN ('admin','super_admin')`,company.ID).Scan(&count))
  if success!=1||blocked!=1||count!=1{t.Fatalf("success=%d blocked=%d active=%d",success,blocked,count)}
 })}
}
func TestReleasePostgresMemberPatchAndRoleRecheck(t *testing.T){
 st,_,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);admin:=rbPGUser(t,st,company.ID,models.RoleAdmin);u:=rbPGUser(t,st,company.ID,models.RoleUser);actor:=rbPGActor(admin,company.ID)
 inactive:=false;_,err:=st.UpdateUserGuarded(ctx,actor,company.ID,u.ID,models.UserAdminPatch{IsActive:&inactive});must(t,err)
 name:="Changed by an old form";got,err:=st.UpdateUserGuarded(ctx,actor,company.ID,u.ID,models.UserAdminPatch{DisplayName:&name});must(t,err)
 if got.IsActive||got.DisplayName!=name{t.Fatal("display patch restored stale identity fields")}
 operator:=rbPGUser(t,st,company.ID,models.RoleSuperAdmin);adminRole:=models.RoleAdmin
 _,err=st.UpdateUserGuarded(ctx,rbPGActor(operator,company.ID),company.ID,u.ID,models.UserAdminPatch{Role:&adminRole});must(t,err)
 if _,err=st.UpdateUserGuarded(ctx,actor,company.ID,u.ID,models.UserAdminPatch{DisplayName:&name});!authz.IsAuthzError(err){t.Fatalf("stale ordinary-target authorization: %v",err)}
 if err=st.DeleteUserGuarded(ctx,actor,company.ID,operator.ID);!authz.IsAuthzError(err){t.Fatalf("admin deleted super: %v",err)}
}
func TestReleasePostgresMemberAuditRollback(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);admin:=rbPGUser(t,st,company.ID,models.RoleAdmin);u:=rbPGUser(t,st,company.ID,models.RoleUser)
 _,err:=pool.Exec(ctx,`CREATE FUNCTION release_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$; CREATE TRIGGER release_reject_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION release_reject_audit()`);must(t,err)
 name:="must roll back";if _,err=st.UpdateUserGuarded(ctx,rbPGActor(admin,company.ID),company.ID,u.ID,models.UserAdminPatch{DisplayName:&name});err==nil{t.Fatal("audit failure ignored")}
 got,err:=st.GetUser(ctx,u.ID);must(t,err);if got.DisplayName==name{t.Fatal("member mutation escaped failed transaction")}
 if err=st.DeleteUserGuarded(ctx,rbPGActor(admin,company.ID),company.ID,u.ID);err==nil{t.Fatal("delete audit failure ignored")}
 got,err=st.GetUser(ctx,u.ID);must(t,err);if got==nil{t.Fatal("delete escaped failed audit transaction")}
}
func TestReleasePostgresRefreshConcurrentSingleConsumer(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);u:=rbPGUser(t,st,company.ID,models.RoleUser);root:=rbNewRefresh(u.ID);must(t,st.CreateRefreshToken(ctx,root))
 other:=rbNewRefresh(u.ID);must(t,st.CreateRefreshToken(ctx,other));start:=make(chan struct{})
 type result struct{ok,replay bool;err error};results:=make(chan result,2)
 for i:=0;i<2;i++{go func(){<-start;ok,replay,err:=st.RotateRefreshToken(ctx,root.TokenHash,rbNewRefresh(u.ID));results<-result{ok,replay,err}}()};close(start)
 successes,replays:=0,0;for i:=0;i<2;i++{r:=<-results;must(t,r.err);if r.ok{successes++};if r.replay{replays++}}
 if successes!=1||replays!=1{t.Fatalf("rotated=%d replays=%d",successes,replays)}
 var active int;must(t,pool.QueryRow(ctx,`SELECT count(*) FROM refresh_tokens WHERE family_id=$1 AND revoked_at IS NULL`,root.FamilyID).Scan(&active));if active!=0{t.Fatal("replay left family child active")}
 row,err:=st.GetRefreshToken(ctx,other.TokenHash);must(t,err);if row.RevokedAt!=nil{t.Fatal("replay revoked unrelated session family")}
}
func TestReleasePostgresRefreshDescendantAndLogoutRaces(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);u:=rbPGUser(t,st,company.ID,models.RoleUser)
 for _,mode:=range []string{"replay","logout"}{for i:=0;i<8;i++{
  root:=rbNewRefresh(u.ID);must(t,st.CreateRefreshToken(ctx,root));child:=rbNewRefresh(u.ID);ok,_,err:=st.RotateRefreshToken(ctx,root.TokenHash,child);must(t,err);if !ok{t.Fatal("initial rotation failed")}
  start:=make(chan struct{});results:=make(chan error,2)
  go func(){<-start;_,_,err:=st.RotateRefreshToken(ctx,child.TokenHash,rbNewRefresh(u.ID));results<-err}()
  go func(){<-start;if mode=="logout"{results<-st.RevokeRefreshTokenByHash(ctx,root.TokenHash)}else{_,_,err:=st.RotateRefreshToken(ctx,root.TokenHash,rbNewRefresh(u.ID));results<-err}}()
  close(start);must(t,<-results);must(t,<-results)
  var count int;must(t,pool.QueryRow(ctx,`SELECT count(*) FROM refresh_tokens WHERE family_id=$1 AND revoked_at IS NULL`,root.FamilyID).Scan(&count));if count!=0{t.Fatalf("%s race left %d active descendants",mode,count)}
 }}
}
func TestReleasePostgresRefreshRollback(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);u:=rbPGUser(t,st,company.ID,models.RoleUser);root:=rbNewRefresh(u.ID);other:=rbNewRefresh(u.ID);must(t,st.CreateRefreshToken(ctx,root));must(t,st.CreateRefreshToken(ctx,other))
 duplicate:=rbNewRefresh(u.ID);duplicate.TokenHash=other.TokenHash
 if ok,_,err:=st.RotateRefreshToken(ctx,root.TokenHash,duplicate);err==nil||ok{t.Fatal("duplicate replacement not rejected")}
 got,err:=st.GetRefreshToken(ctx,root.TokenHash);must(t,err);if got.RevokedAt!=nil{t.Fatal("insert failure consumed old token")}
 _,err=pool.Exec(ctx,`CREATE FUNCTION release_reject_refresh() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected refresh update failure'; END $$; CREATE TRIGGER release_reject_refresh BEFORE UPDATE ON refresh_tokens FOR EACH ROW EXECUTE FUNCTION release_reject_refresh()`);must(t,err)
 if ok,_,err:=st.RotateRefreshToken(ctx,root.TokenHash,rbNewRefresh(u.ID));err==nil||ok{t.Fatal("revoke failure not propagated")}
 if err=st.RevokeRefreshTokenByHash(ctx,root.TokenHash);err==nil{t.Fatal("logout database failure not propagated")}
 got,err=st.GetRefreshToken(ctx,root.TokenHash);must(t,err);if got.RevokedAt!=nil{t.Fatal("failed revoke changed token")}
}
func TestReleasePostgresSharedMailboxAndOutboundScope(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=rbPGTenant(t,st);sender:=rbPGUser(t,st,company.ID,models.RoleUser);reader:=rbPGUser(t,st,company.ID,models.RoleUser)
 z:=&models.DomainZone{TenantID:company.ID,Domain:"shared.company.test",IsVerified:true,MXVerified:true};must(t,st.CreateZone(ctx,z))
 mb:=&models.Mailbox{TenantID:company.ID,ZoneID:z.ID,FullAddress:"shared@shared.company.test",LocalPart:"shared",ResolvedDomain:z.Domain,AccessMode:models.AccessToken};must(t,st.CreateMailbox(ctx,mb))
 grant:=&models.MailboxGrant{TenantID:company.ID,MailboxID:mb.ID,UserID:reader.ID,CanRead:true};must(t,st.SetMailboxGrant(ctx,grant))
 j:=&models.OutboundJob{TenantID:company.ID,ZoneID:z.ID,UserID:&sender.ID,SenderUserID:&sender.ID,SenderMailboxID:&mb.ID,MailFrom:mb.FullAddress,RcptTo:[]string{"recipient@test.invalid"},Subject:"shared",TextBody:"protected"};must(t,st.CreateOutboundJob(ctx,j))
 ms:=authz.ZoneListFilter{TenantID:company.ID,AllZones:true,OwnerUserID:&reader.ID,GrantedUserID:&reader.ID};os:=authz.OwnerListFilter{TenantID:company.ID,UserID:&reader.ID,ReaderUserID:&reader.ID}
 boxes,n,err:=st.ListMailboxesScoped(ctx,ms,models.Page{});must(t,err);if n!=1||len(boxes)!=1{t.Fatalf("shared mailbox hidden count=%d",n)}
 jobs,n,err:=st.ListOutboundJobsScoped(ctx,os,models.Page{});must(t,err);if n!=1||len(jobs)!=1||jobs[0].ID!=j.ID{t.Fatalf("shared outbound hidden count=%d",n)}
 // An OR grant must not escape the allowlist or tenant clause.
 ms.AllZones=false;ms.ZoneIDs=[]uuid.UUID{uuid.New()};_,n,err=st.ListMailboxesScoped(ctx,ms,models.Page{});must(t,err);if n!=0{t.Fatal("grant escaped zone filter")}
 os.AllowedZoneIDs=ms.ZoneIDs;_,n,err=st.ListOutboundJobsScoped(ctx,os,models.Page{});must(t,err);if n!=0{t.Fatal("outbound grant escaped zone filter")}
 ms.AllZones=true;os.AllowedZoneIDs=nil
 grant.CanRead=false;must(t,st.SetMailboxGrant(ctx,grant));_,n,err=st.ListMailboxesScoped(ctx,ms,models.Page{});must(t,err);if n!=0{t.Fatal("revoked mailbox grant listed")}
 _,n,err=st.ListOutboundJobsScoped(ctx,os,models.Page{});must(t,err);if n!=0{t.Fatal("revoked outbound grant listed")}
 // Personal mailbox ownership is discoverable without owning the domain.
 _,err=pool.Exec(ctx,`UPDATE mailboxes SET owner_user_id=$2 WHERE id=$1`,mb.ID,reader.ID);must(t,err)
 _,n,err=st.ListMailboxesScoped(ctx,ms,models.Page{});must(t,err);if n!=1{t.Fatal("personal owner mailbox undiscoverable")}
 _,n,err=st.ListOutboundJobsScoped(ctx,os,models.Page{});must(t,err);if n!=1{t.Fatal("mailbox owner sent history undiscoverable")}
}
