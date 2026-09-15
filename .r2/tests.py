from patchutil import *
import sys

write('internal/api/release_r2_regression_test.go',r'''
package api_test
import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/alicebob/miniredis/v2"
 "github.com/google/uuid"
 "github.com/redis/go-redis/v9"
 "github.com/rs/zerolog"
 "tabmail/internal/api"
 "tabmail/internal/api/middleware"
 "tabmail/internal/config"
 "tabmail/internal/models"
 "tabmail/internal/outbound"
 "tabmail/internal/policy"
 "tabmail/internal/testutil"
)
func r2Router(t *testing.T,st *testutil.FakeStore,obj *testutil.MemoryObjectStore) http.Handler {
 t.Helper();mr:=miniredis.RunT(t);rdb:=redis.NewClient(&redis.Options{Addr:mr.Addr()});t.Cleanup(func(){rdb.Close()})
 return api.NewRouter(api.RouterConfig{Store:st,ObjectStore:obj,JWTSecret:"jwt-test-secret",MailboxTokenSecret:"mailbox-secret",PublicTenantID:publicTenantID,NamingMode:policy.NamingFull,RateLimiter:middleware.NewRateLimiter(rdb,st,1000,nil),OutboundService:outbound.NewService(config.Outbound{Enabled:true,Mode:"relay"},st,zerolog.Nop()),Logger:zerolog.Nop()})
}
func r2Request(t *testing.T,h http.Handler,u *models.User,method,path,body string)*httptest.ResponseRecorder{
 t.Helper();req:=httptest.NewRequest(method,path,strings.NewReader(body));req.Header.Set("Content-Type","application/json")
 if u!=nil {req.Header.Set("Authorization","Bearer "+issueAccessTokenForExistingUser(t,u))};rr:=httptest.NewRecorder();h.ServeHTTP(rr,req);return rr
}
func TestR2RedAdminCannotManagePeer(t *testing.T){
 for _,role:=range []models.UserRole{models.RoleAdmin,models.RoleSuperAdmin}{for _,action:=range []string{"modify","disable","delete"}{t.Run(string(role)+"/"+action,func(t *testing.T){
  st,obj,tenant:=seededStores(t);a:=seedUserForTest(t,st,tenant,models.RoleAdmin);peer:=seedUserForTest(t,st,tenant,role)
  method,body:="PATCH",`{"display_name":"changed"}`;if action=="disable"{body=`{"is_active":false}`};if action=="delete"{method="DELETE";body=""}
  rr:=r2Request(t,r2Router(t,st,obj),a,method,"/api/v1/admin/users/"+peer.ID.String(),body)
  if rr.Code!=403 {t.Fatalf("target-role guard: got %d %s",rr.Code,rr.Body.String())}
 })}}
}
func TestR2RedMailboxDefaultPrivate(t *testing.T){
 st,obj,tenant:=seededStores(t);a:=seedUserForTest(t,st,tenant,models.RoleAdmin)
 rr:=r2Request(t,r2Router(t,st,obj),a,"POST","/api/v1/mailboxes",`{"address":"private@mail.test","password":"Passw0rd!","retention_hours_override":0}`)
 if rr.Code!=201{t.Fatalf("create: %d %s",rr.Code,rr.Body.String())}
 var body struct{Data models.Mailbox `json:"data"`};if err:=json.Unmarshal(rr.Body.Bytes(),&body);err!=nil{t.Fatal(err)}
 if body.Data.AccessMode!=models.AccessToken || body.Data.OwnerUserID==nil || *body.Data.OwnerUserID!=a.ID {t.Fatalf("private owner default missing: %+v",body.Data)}
}
func TestR2RedOutboundAdminContent(t *testing.T){
 st,obj,tenant:=seededStores(t);a:=seedUserForTest(t,st,tenant,models.RoleAdmin);sender:=seedUserForTest(t,st,tenant,models.RoleUser)
 job:=&models.OutboundJob{TenantID:tenant,ZoneID:findTenantZone(t,st,tenant),UserID:&sender.ID,SenderUserID:&sender.ID,MailFrom:"sender@mail.test",Subject:"test",TextBody:"confidential-body",HTMLBody:"confidential-html",BCC:[]string{"hidden@example.test"},RcptTo:[]string{"visible@example.test","hidden@example.test"},To:[]string{"visible@example.test"},LastError:"RCPT TO hidden@example.test failed",SMTPResponse:"hidden@example.test rejected",HeadersJSON:json.RawMessage(`{"X-Secret":"private-header"}`)}
 if err:=st.CreateOutboundJob(context.Background(),job);err!=nil{t.Fatal(err)}
 rr:=r2Request(t,r2Router(t,st,obj),a,"GET","/api/v1/outbound/"+job.ID.String(),"")
 if rr.Code!=200{t.Fatalf("metadata: %d %s",rr.Code,rr.Body.String())}
 for _,secret:=range []string{"confidential-body","confidential-html","hidden@example.test","private-header"}{if strings.Contains(rr.Body.String(),secret){t.Fatalf("outbound disclosure: %s",secret)}}
 if !strings.Contains(rr.Body.String(),`"content_redacted":true`){t.Fatal("missing redaction flag")}
}
func TestR2RedWorkerTokenNeverSerialized(t *testing.T){
 token:=uuid.New();raw,err:=json.Marshal(models.OutboundJob{DeliveryToken:&token});if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),"delivery_token") || strings.Contains(string(raw),token.String()){t.Fatalf("worker credential disclosed: %s",raw)}
}
func TestR2MailboxOwnerValidationAndAnonymousBoundary(t *testing.T){
 st,obj,tenant:=seededStores(t);a:=seedUserForTest(t,st,tenant,models.RoleAdmin);employee:=seedUserForTest(t,st,tenant,models.RoleUser);h:=r2Router(t,st,obj)
 rr:=r2Request(t,h,a,"POST","/api/v1/mailboxes",fmt.Sprintf(`{"address":"employee@mail.test","password":"Passw0rd!","owner_user_id":"%s"}`,employee.ID))
 if rr.Code!=201{t.Fatalf("employee provision: %d %s",rr.Code,rr.Body.String())}
 if got:=r2Request(t,h,nil,"GET","/api/v1/mailbox/employee@mail.test","");got.Code<400{t.Fatalf("anonymous read %d",got.Code)}
 if got:=r2Request(t,h,employee,"GET","/api/v1/mailboxes","");got.Code!=200 || !strings.Contains(got.Body.String(),"employee@mail.test"){t.Fatalf("owner cannot find mailbox: %d %s",got.Code,got.Body.String())}
 for i,id:=range []uuid.UUID{uuid.New(),a.ID}{rr=r2Request(t,h,a,"POST","/api/v1/mailboxes",fmt.Sprintf(`{"address":"bad%d@mail.test","password":"Passw0rd!","owner_user_id":"%s"}`,i,id));if rr.Code!=400{t.Fatalf("invalid owner: %d %s",rr.Code,rr.Body.String())}}
 rr=r2Request(t,h,a,"POST","/api/v1/mailboxes",`{"address":"no-password@mail.test"}`);if rr.Code!=400{t.Fatalf("token without credential: %d",rr.Code)}
}
func TestR2SharedMailboxOutboundAndRetryBoundary(t *testing.T){
 st,obj,tenant:=seededStores(t);ctx:=context.Background();sender:=seedUserForTest(t,st,tenant,models.RoleUser);reader:=seedUserForTest(t,st,tenant,models.RoleUser)
 mb:=&models.Mailbox{ID:uuid.New(),TenantID:tenant,ZoneID:findTenantZone(t,st,tenant),FullAddress:"shared@mail.test",AccessMode:models.AccessToken};st.SeedMailbox(mb)
 job:=&models.OutboundJob{TenantID:tenant,ZoneID:mb.ZoneID,SenderMailboxID:&mb.ID,SenderUserID:&sender.ID,UserID:&sender.ID,MailFrom:mb.FullAddress,TextBody:"shared-secret",State:models.OutboundFailed,To:[]string{"x@example.test"},RcptTo:[]string{"x@example.test"}}
 if err:=st.CreateOutboundJob(ctx,job);err!=nil{t.Fatal(err)};h:=r2Router(t,st,obj)
 path:="/api/v1/outbound/"+job.ID.String()
 if rr:=r2Request(t,h,reader,"GET",path,"");rr.Code!=404{t.Fatalf("ungranted read: %d",rr.Code)}
 g:=&models.MailboxGrant{TenantID:tenant,MailboxID:mb.ID,UserID:reader.ID,CanRead:true};if err:=st.SetMailboxGrant(ctx,g);err!=nil{t.Fatal(err)}
 for _,p:=range []string{path,"/api/v1/outbound","/api/v1/mailboxes"}{rr:=r2Request(t,h,reader,"GET",p,"");if rr.Code!=200 || !strings.Contains(rr.Body.String(),"shared"){t.Fatalf("shared scope %s: %d %s",p,rr.Code,rr.Body.String())}}
 if rr:=r2Request(t,h,reader,"POST",path+"/retry","");rr.Code!=403{t.Fatalf("read-only grant retried sender's job: %d",rr.Code)}
 g.CanRead=false;if err:=st.SetMailboxGrant(ctx,g);err!=nil{t.Fatal(err)}
 if rr:=r2Request(t,h,reader,"GET",path,"");rr.Code!=404{t.Fatalf("revoked grant read: %d",rr.Code)}
 rr:=r2Request(t,h,sender,"GET",path,"");if rr.Code!=200 || !strings.Contains(rr.Body.String(),"shared-secret"){t.Fatalf("sender history: %d %s",rr.Code,rr.Body.String())}
}
''')
write('internal/api/handlers/release_r2_refresh_test.go',r'''
package handlers
import (
 "context"
 "errors"
 "net/http/httptest"
 "net/http"
 "strings"
 "sync"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/rs/zerolog"
 "tabmail/internal/authn"
 "tabmail/internal/models"
 "tabmail/internal/testutil"
)
type r2RefreshFailure struct{*testutil.FakeStore;old *models.RefreshToken}
func(s r2RefreshFailure)GetRefreshToken(context.Context,string)(*models.RefreshToken,error){return s.old,nil}
func(s r2RefreshFailure)RevokeRefreshToken(context.Context,uuid.UUID)error{return errors.New("injected revocation failure")}
func(s r2RefreshFailure)RotateRefreshToken(context.Context,string,*models.RefreshToken)(bool,bool,error){return false,false,errors.New("injected revocation failure")}
func TestR2RedRefreshRevocationFailure(t *testing.T){
 st:=testutil.NewFakeStore();u:=&models.User{ID:uuid.New(),TenantID:uuid.New(),Role:models.RoleUser,IsActive:true};if err:=st.CreateUser(context.Background(),u);err!=nil{t.Fatal(err)}
 h:=NewAuthHandler(r2RefreshFailure{st,&models.RefreshToken{ID:uuid.New(),UserID:u.ID,ExpiresAt:time.Now().Add(time.Hour)}},"jwt-test-secret",uuid.Nil,false,nil,true,zerolog.Nop())
 rr:=httptest.NewRecorder();h.Refresh(rr,httptest.NewRequest("POST","/api/v1/auth/refresh",strings.NewReader(`{"refresh_token":"old"}`)))
 if rr.Code!=401 || strings.Contains(rr.Body.String(),"access_token"){t.Fatalf("revocation failure issued access: %d %s",rr.Code,rr.Body.String())}
}
func TestR2RefreshCookieSingleConsumptionAndLogout(t *testing.T){
 ctx:=context.Background();st:=testutil.NewFakeStore();u:=&models.User{ID:uuid.New(),TenantID:uuid.New(),Role:models.RoleUser,IsActive:true};if err:=st.CreateUser(ctx,u);err!=nil{t.Fatal(err)}
 h:=NewAuthHandler(st,"jwt-test-secret",uuid.Nil,false,nil,true,zerolog.Nop())
 _,raw,err:=h.issueTokenPair(ctx,u);if err!=nil{t.Fatal(err)}
 start:=make(chan struct{});out:=make(chan *httptest.ResponseRecorder,2);var wg sync.WaitGroup
 for i:=0;i<2;i++{wg.Add(1);go func(){defer wg.Done();<-start;r:=httptest.NewRequest("POST","/api/v1/auth/refresh",nil);r.AddCookie(&http.Cookie{Name:RefreshCookieName,Value:raw});rr:=httptest.NewRecorder();h.Refresh(rr,r);out<-rr}()};close(start);wg.Wait();close(out)
 success,denied:=0,0;for rr:=range out{switch rr.Code{case 200:success++;for _,c:=range rr.Result().Cookies(){if c.Name==RefreshCookieName && (!c.HttpOnly || !c.Secure){t.Fatal("unsafe refresh cookie")}};case 401:denied++;default:t.Fatalf("unexpected refresh: %d %s",rr.Code,rr.Body.String())}}
 if success!=1 || denied!=1{t.Fatalf("concurrent refresh success=%d denied=%d",success,denied)}
 _,raw,err=h.issueTokenPair(ctx,u);if err!=nil{t.Fatal(err)}
 r:=httptest.NewRequest("POST","/api/v1/auth/logout",nil);r.AddCookie(&http.Cookie{Name:RefreshCookieName,Value:raw});rr:=httptest.NewRecorder();h.Logout(rr,r);if rr.Code!=204{t.Fatalf("logout %d",rr.Code)}
 r=httptest.NewRequest("POST","/api/v1/auth/refresh",nil);r.AddCookie(&http.Cookie{Name:RefreshCookieName,Value:raw});rr=httptest.NewRecorder();h.Refresh(rr,r);if rr.Code!=401{t.Fatalf("refresh after logout %d",rr.Code)}
 // The stateful fake stores hashes, never raw refresh secrets.
 rt,err:=st.GetRefreshToken(ctx,authn.HashToken(raw));if err!=nil || rt==nil || rt.RevokedAt==nil{t.Fatal("logout did not persist revocation")}
}
''')
write('internal/config/release_r2_test.go',r'''
package config
import("os";"strings";"testing")
func TestR2RedProductionCompanyDefaults(t *testing.T){
 raw,err:=os.ReadFile("../../docker-compose.prod.yml");if err!=nil{t.Fatal(err)};s:=string(raw)
 for _,required:=range []string{"TABMAIL_OPEN_REGISTRATION:-false","TABMAIL_OUTBOUND_ENABLED:-true","args:","INTERNAL_API_URL: \"http://tabmail-api:8080\""}{if !strings.Contains(s,required){t.Errorf("production missing %s",required)}}
}
''')
if len(sys.argv)>1 and sys.argv[1]=='red':
    sys.exit(0)

write('internal/authz/members_test.go',r'''
package authz
import("testing";"github.com/google/uuid";"tabmail/internal/models")
func TestR2MemberHierarchy(t *testing.T){
 tenant:=uuid.New();other:=uuid.New()
 for _,tc:=range []struct{name string;actor Actor;target uuid.UUID;role models.UserRole;want bool}{
  {"admin-user",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleUser,true},
  {"admin-peer",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleAdmin,false},
  {"admin-super",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleSuperAdmin,false},
  {"super-selected",Actor{TenantID:tenant,IsSuperAdmin:true},tenant,models.RoleAdmin,true},
  {"super-other-not-selected",Actor{TenantID:tenant,IsSuperAdmin:true},other,models.RoleUser,false},
  {"ordinary",Actor{TenantID:tenant},tenant,models.RoleUser,false},
  {"empty",Actor{},uuid.Nil,models.RoleUser,false},
 }{t.Run(tc.name,func(t *testing.T){if got:=CanManageTenantMember(tc.actor,tc.target,tc.role);got!=tc.want{t.Fatalf("got %v want %v",got,tc.want)}})}
}
''')
write('internal/store/postgres/release_r2_test.go',r'''
package postgres_test
import (
 "context"
 "errors"
 "fmt"
 "sync"
 "testing"
 "time"
 "github.com/google/uuid"
 "tabmail/internal/authz"
 "tabmail/internal/models"
 "tabmail/internal/store"
 "tabmail/internal/testpg"
)
func TestR2PostgresConcurrentLastAdminAndStaleEdit(t *testing.T){
 for _,operation:=range []string{"disable","demote","delete"}{t.Run(operation,func(t *testing.T){
  st,_,_:=testpg.NewPostgres(t);ctx:=context.Background();plan:=uuid.MustParse("00000000-0000-0000-0000-000000000001")
  company:=&models.Tenant{Name:"Company",PlanID:plan};must(t,st.CreateTenant(ctx,company));platform:=&models.Tenant{Name:"Platform",PlanID:plan};must(t,st.CreateTenant(ctx,platform))
  super:=&models.User{TenantID:platform.ID,Email:"super@platform.test",PasswordHash:"test",Role:models.RoleSuperAdmin,IsActive:true};must(t,st.CreateUser(ctx,super))
  a:=authz.Actor{Type:authz.PrincipalUser,ID:super.ID,TenantID:company.ID,IsSuperAdmin:true}
  users:=[]*models.User{};for i:=0;i<2;i++{u:=&models.User{TenantID:company.ID,Email:fmt.Sprintf("admin%d@company.test",i),PasswordHash:"test",Role:models.RoleAdmin,IsActive:true};must(t,st.CreateUser(ctx,u));u,err:=st.GetUser(ctx,u.ID);must(t,err);users=append(users,u)}
  start:=make(chan struct{});out:=make(chan error,2);var wg sync.WaitGroup
  for _,u:=range users{wg.Add(1);go func(u *models.User){defer wg.Done();<-start;if operation=="delete"{out<-st.DeleteUserGuarded(ctx,a,company.ID,u.ID);return};if operation=="disable"{u.IsActive=false}else{u.Role=models.RoleUser};out<-st.DeactivateOrDemoteUserGuarded(ctx,a,u)}(u)};close(start);wg.Wait();close(out)
  good,blocked:=0,0;for err:=range out{if err==nil{good++}else if errors.Is(err,store.ErrLastCompanyAdmin){blocked++}else{t.Fatalf("unexpected guard error: %v",err)}};if good!=1||blocked!=1{t.Fatalf("success=%d blocked=%d",good,blocked)}
  // A display-name edit from an old snapshot cannot restore a newer role/status.
  member:=&models.User{TenantID:company.ID,Email:"employee@company.test",PasswordHash:"test",Role:models.RoleUser,IsActive:true};must(t,st.CreateUser(ctx,member));member,err:=st.GetUser(ctx,member.ID);must(t,err);stale:=*member
  member.DisplayName="new";must(t,st.DeactivateOrDemoteUserGuarded(ctx,a,member));stale.DisplayName="old"
  if err=st.DeactivateOrDemoteUserGuarded(ctx,a,&stale);!errors.Is(err,store.ErrMemberChanged){t.Fatalf("stale update accepted: %v",err)}
 })}
}
func TestR2PostgresRefreshTransactions(t *testing.T){
 st,pool,_:=testpg.NewPostgres(t);ctx:=context.Background();company:=&models.Tenant{Name:"Refresh",PlanID:uuid.MustParse("00000000-0000-0000-0000-000000000001")};must(t,st.CreateTenant(ctx,company));u:=&models.User{TenantID:company.ID,Email:"refresh@company.test",PasswordHash:"test",Role:models.RoleUser,IsActive:true};must(t,st.CreateUser(ctx,u))
 initial:=func(hash string)*models.RefreshToken{r:=&models.RefreshToken{UserID:u.ID,TokenHash:hash,ExpiresAt:time.Now().Add(time.Hour)};must(t,st.CreateRefreshToken(ctx,r));return r}
 fresh:=func(hash string)*models.RefreshToken{return &models.RefreshToken{TokenHash:hash,ExpiresAt:time.Now().Add(time.Hour)}}
 old:=initial("old");independent:=initial("independent")
 start:=make(chan struct{});type result struct{ok,replay bool;err error};out:=make(chan result,2);var wg sync.WaitGroup
 for i:=0;i<2;i++{wg.Add(1);go func(i int){defer wg.Done();<-start;ok,replay,err:=st.RotateRefreshToken(ctx,"old",fresh(fmt.Sprintf("next%d",i)));out<-result{ok,replay,err}}(i)};close(start);wg.Wait();close(out)
 successes,replays:=0,0;for r:=range out{must(t,r.err);if r.ok{successes++};if r.replay{replays++}};if successes!=1||replays!=1{t.Fatalf("double consume: %d/%d",successes,replays)}
 var live int;must(t,pool.QueryRow(ctx,`SELECT count(*) FROM refresh_tokens WHERE family_id=$1 AND revoked_at IS NULL`,old.FamilyID).Scan(&live));if live!=0{t.Fatal("replay left a live descendant")}
 next:=fresh("independent-next");ok,_,err:=st.RotateRefreshToken(ctx,independent.TokenHash,next);must(t,err);if !ok{t.Fatal("replay revoked an independent session")}
 // Failed child insertion must roll back old-token revocation.
 rollback:=initial("rollback");ok,_,err=st.RotateRefreshToken(ctx,rollback.TokenHash,fresh("independent-next"));if err==nil||ok{t.Fatal("expected duplicate child insertion failure")}
 stored,err:=st.GetRefreshToken(ctx,rollback.TokenHash);must(t,err);if stored.RevokedAt!=nil{t.Fatal("failed rotation consumed old token")}
 // Expiry, inactive accounts, logout and replay tombstone retention.
 expired:=initial("expired");_,err=pool.Exec(ctx,`UPDATE refresh_tokens SET expires_at=now()-interval '1 second' WHERE id=$1`,expired.ID);must(t,err)
 ok,_,err=st.RotateRefreshToken(ctx,expired.TokenHash,fresh("expired-next"));must(t,err);if ok{t.Fatal("expired rotation accepted")}
 must(t,st.RevokeRefreshTokenByHash(ctx,next.TokenHash));ok,_,err=st.RotateRefreshToken(ctx,next.TokenHash,fresh("after-logout"));must(t,err);if ok{t.Fatal("logged-out rotation accepted")}
 root:=initial("tombstone-root");child:=fresh("tombstone-child");ok,_,err=st.RotateRefreshToken(ctx,root.TokenHash,child);must(t,err);if !ok{t.Fatal("root failed")}
 _,err=pool.Exec(ctx,`UPDATE refresh_tokens SET expires_at=now()-interval '1 hour' WHERE id=$1`,root.ID);must(t,err);must(t,st.DeleteExpiredRefreshTokens(ctx));stored,err=st.GetRefreshToken(ctx,root.TokenHash);must(t,err);if stored==nil{t.Fatal("live-family replay tombstone deleted")}
 // Replaying an ancestor and rotating its child concurrently cannot let a
 // grandchild escape family revocation, regardless of lock acquisition order.
 for i:=0;i<5;i++{root:=initial(fmt.Sprintf("race-root-%d",i));child:=fresh(fmt.Sprintf("race-child-%d",i));ok,_,err=st.RotateRefreshToken(ctx,root.TokenHash,child);must(t,err);if !ok{t.Fatal("root rotation failed")};start:=make(chan struct{});errs:=make(chan error,2)
  go func(){<-start;_,_,e:=st.RotateRefreshToken(ctx,root.TokenHash,fresh(uuid.NewString()));errs<-e}()
  go func(){<-start;_,_,e:=st.RotateRefreshToken(ctx,child.TokenHash,fresh(uuid.NewString()));errs<-e}();close(start);must(t,<-errs);must(t,<-errs)
  must(t,pool.QueryRow(ctx,`SELECT count(*) FROM refresh_tokens WHERE family_id=$1 AND revoked_at IS NULL`,root.FamilyID).Scan(&live));if live!=0{t.Fatal("concurrent descendant escaped replay")}
 }
}
func TestR2PostgresMailboxAndOutboundReadScopes(t *testing.T){
 st,_,_:=testpg.NewPostgres(t);ctx:=context.Background();tenant:=&models.Tenant{Name:"Scope",PlanID:uuid.MustParse("00000000-0000-0000-0000-000000000001")};must(t,st.CreateTenant(ctx,tenant))
 sender:=&models.User{TenantID:tenant.ID,Email:"sender@scope.test",PasswordHash:"test",Role:models.RoleUser,IsActive:true};must(t,st.CreateUser(ctx,sender));reader:=&models.User{TenantID:tenant.ID,Email:"reader@scope.test",PasswordHash:"test",Role:models.RoleUser,IsActive:true};must(t,st.CreateUser(ctx,reader))
 zone:=&models.DomainZone{TenantID:tenant.ID,Domain:"scope.test",IsVerified:true,MXVerified:true};must(t,st.CreateZone(ctx,zone))
 mb:=&models.Mailbox{TenantID:tenant.ID,ZoneID:zone.ID,FullAddress:"shared@scope.test",LocalPart:"shared",ResolvedDomain:zone.Domain,AccessMode:models.AccessToken};must(t,st.CreateMailbox(ctx,mb))
 grant:=&models.MailboxGrant{TenantID:tenant.ID,MailboxID:mb.ID,UserID:reader.ID,CanRead:true};must(t,st.SetMailboxGrant(ctx,grant))
 job:=&models.OutboundJob{TenantID:tenant.ID,ZoneID:zone.ID,SenderMailboxID:&mb.ID,SenderUserID:&sender.ID,UserID:&sender.ID,MailFrom:mb.FullAddress,RcptTo:[]string{"x@example.test"},TextBody:"private",MaxAttempts:5};must(t,st.CreateOutboundJob(ctx,job))
 a:=authz.Actor{Type:authz.PrincipalUser,ID:reader.ID,TenantID:tenant.ID}
 mbs,n,err:=st.ListMailboxesScoped(ctx,authz.MailboxListScope(a,tenant.ID),models.Page{});must(t,err);if n!=1||len(mbs)!=1{t.Fatalf("mailbox grant invisible: %d/%d",n,len(mbs))}
 jobs,n,err:=st.ListOutboundJobsScoped(ctx,authz.OutboundListScope(a,tenant.ID),models.Page{});must(t,err);if n!=1||len(jobs)!=1{t.Fatalf("shared sent history invisible: %d/%d",n,len(jobs))}
 a.Permission=&models.EffectivePermission{AllowedZoneIDs:[]uuid.UUID{uuid.New()}}
 _,n,err=st.ListMailboxesScoped(ctx,authz.MailboxListScope(a,tenant.ID),models.Page{});must(t,err);if n!=0{t.Fatal("grant bypassed zone restriction")}
 _,n,err=st.ListOutboundJobsScoped(ctx,authz.OutboundListScope(a,tenant.ID),models.Page{});must(t,err);if n!=0{t.Fatal("outbound grant bypassed zone restriction")}
 a.Permission=nil;grant.CanRead=false;must(t,st.SetMailboxGrant(ctx,grant));_,n,err=st.ListOutboundJobsScoped(ctx,authz.OutboundListScope(a,tenant.ID),models.Page{});must(t,err);if n!=0{t.Fatal("revoked grant visible")}
}
''')
