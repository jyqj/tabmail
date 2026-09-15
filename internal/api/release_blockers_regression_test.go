package api_test

// These tests use only baseline APIs so the exact same file can run red on
// main@6434118 and green on the proposed business tree.
import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
 "github.com/alicebob/miniredis/v2"
 "github.com/google/uuid"
 "github.com/redis/go-redis/v9"
 "github.com/rs/zerolog"
 "tabmail/internal/api"
 "tabmail/internal/api/middleware"
 "tabmail/internal/authn"
 "tabmail/internal/config"
 "tabmail/internal/models"
 "tabmail/internal/outbound"
 "tabmail/internal/policy"
 "tabmail/internal/store"
 "tabmail/internal/testutil"
)

func rbRouter(t *testing.T,st store.Store,obj *testutil.MemoryObjectStore)http.Handler{
 t.Helper();mr:=miniredis.RunT(t);rdb:=redis.NewClient(&redis.Options{Addr:mr.Addr()});t.Cleanup(func(){_ = rdb.Close()})
 return api.NewRouter(api.RouterConfig{Store:st,ObjectStore:obj,JWTSecret:"jwt-test-secret",MailboxTokenSecret:"mailbox-secret",PublicTenantID:publicTenantID,NamingMode:policy.NamingFull,StripPlus:true,HTTP:config.HTTP{CookieSecure:true},RateLimiter:middleware.NewRateLimiter(rdb,st,10000,nil),OutboundService:outbound.NewService(config.Outbound{Enabled:true},st,zerolog.Nop()),Logger:zerolog.Nop()})
}
func rbRequest(t *testing.T,h http.Handler,user *models.User,method,path,body string,cookie *http.Cookie)*httptest.ResponseRecorder{
 t.Helper();r:=httptest.NewRequest(method,path,strings.NewReader(body));r.Header.Set("Content-Type","application/json")
 if user!=nil{r.Header.Set("Authorization","Bearer "+issueAccessTokenForExistingUser(t,user))}
 if cookie!=nil{r.AddCookie(cookie)}
 w:=httptest.NewRecorder();h.ServeHTTP(w,r);return w
}

func TestReleaseBlockerAdminCannotManagePeer(t *testing.T){
 for _,role:=range []models.UserRole{models.RoleAdmin,models.RoleSuperAdmin}{
  for _,method:=range []string{"PATCH","DELETE"}{
   t.Run(string(role)+method,func(t *testing.T){
    st,obj,tenant:=seededStores(t);actor:=seedUserForTest(t,st,tenant,models.RoleAdmin);target:=seedUserForTest(t,st,tenant,role)
    w:=rbRequest(t,rbRouter(t,st,obj),actor,method,"/api/v1/admin/users/"+target.ID.String(),`{"is_active":false}`,nil)
    if w.Code!=http.StatusForbidden{t.Fatalf("expected 403, got %d %s",w.Code,w.Body.String())}
   })
  }
 }
}
func TestReleaseBlockerPrivateMailboxDefault(t *testing.T){
 st,obj,tenant:=seededStores(t);actor:=seedUserForTest(t,st,tenant,models.RoleAdmin)
 w:=rbRequest(t,rbRouter(t,st,obj),actor,"POST","/api/v1/mailboxes",`{"address":"private@mail.test","password":"disposable-mailbox-password","retention_hours_override":0}`,nil)
 if w.Code!=201{t.Fatalf("create: %d %s",w.Code,w.Body.String())}
 m,err:=st.GetMailboxByAddress(context.Background(),"private@mail.test");if err!=nil{t.Fatal(err)}
 if m==nil||m.AccessMode!=models.AccessToken||m.OwnerUserID==nil||*m.OwnerUserID!=actor.ID{t.Fatalf("not private and owned: %+v",m)}
}
func TestReleaseBlockerOutboundMetadataOnly(t *testing.T){
 st,obj,tenant:=seededStores(t);actor:=seedUserForTest(t,st,tenant,models.RoleAdmin);sender:=seedUserForTest(t,st,tenant,models.RoleUser)
 token:=uuid.New();j:=&models.OutboundJob{TenantID:tenant,UserID:&sender.ID,SenderUserID:&sender.ID,ZoneID:findTenantZone(t,st,tenant),MailFrom:"sender@mail.test",To:[]string{"public@example.test"},BCC:[]string{"hidden-secret@example.test"},RcptTo:[]string{"public@example.test","hidden-secret@example.test"},TextBody:"sensitive-body",HTMLBody:"sensitive-html",HeadersJSON:json.RawMessage(`{"X-Secret":"sensitive-header"}`),DeliveryToken:&token,LastError:"RCPT hidden-secret@example.test",SMTPResponse:"hidden-secret@example.test",Subject:"metadata"}
 if err:=st.CreateOutboundJob(context.Background(),j);err!=nil{t.Fatal(err)}
 h:=rbRouter(t,st,obj)
 for _,path:=range []string{"/api/v1/outbound/"+j.ID.String(),"/api/v1/outbound"}{
  w:=rbRequest(t,h,actor,"GET",path,"",nil)
  if w.Code!=200{t.Fatalf("status %d %s",w.Code,w.Body.String())}
  for _,secret:=range []string{"sensitive-body","sensitive-html","sensitive-header","hidden-secret@",token.String(),"delivery_token"}{if strings.Contains(w.Body.String(),secret){t.Errorf("%s disclosed %q",path,secret)}}
  if !strings.Contains(w.Body.String(),`"content_redacted":true`){t.Error("missing redaction marker")}
 }
}
func TestReleaseBlockerDeliveryTokenNeverSerialized(t *testing.T){
 token:=uuid.New();b,err:=json.Marshal(&models.OutboundJob{DeliveryToken:&token});if err!=nil{t.Fatal(err)}
 if strings.Contains(string(b),"delivery_token")||strings.Contains(string(b),token.String()){t.Fatal("worker fencing token is exposed")}
}

type rbFailedRotation struct{*testutil.FakeStore;token models.RefreshToken}
func(s rbFailedRotation)GetRefreshToken(context.Context,string)(*models.RefreshToken,error){r:=s.token;return &r,nil}
func(s rbFailedRotation)RevokeRefreshToken(context.Context,uuid.UUID)error{return errors.New("injected revoke failure")}
func(s rbFailedRotation)RotateRefreshToken(context.Context,string,*models.RefreshToken)(bool,bool,error){return false,false,errors.New("injected atomic revoke failure")}
func(s rbFailedRotation)RevokeRefreshTokenByHash(context.Context,string)error{return errors.New("injected logout failure")}
func TestReleaseBlockerRefreshFailsClosed(t *testing.T){
 st,obj,tenant:=seededStores(t);u:=seedUserForTest(t,st,tenant,models.RoleUser)
 failing:=rbFailedRotation{st,models.RefreshToken{ID:uuid.New(),UserID:u.ID,TokenHash:authn.HashToken("disposable-refresh"),ExpiresAt:time.Now().Add(time.Hour)}}
 w:=rbRequest(t,rbRouter(t,failing,obj),nil,"POST","/api/v1/auth/refresh",`{"refresh_token":"disposable-refresh"}`,nil)
 if w.Code!=401||strings.Contains(w.Body.String(),"access_token"){t.Fatalf("revoke failure issued tokens: %d %s",w.Code,w.Body.String())}
}
func TestReleaseBlockerLogoutFailsClosed(t *testing.T){
 st,obj,tenant:=seededStores(t);u:=seedUserForTest(t,st,tenant,models.RoleUser)
 failing:=rbFailedRotation{st,models.RefreshToken{ID:uuid.New(),UserID:u.ID,TokenHash:authn.HashToken("disposable-refresh"),ExpiresAt:time.Now().Add(time.Hour)}}
 w:=rbRequest(t,rbRouter(t,failing,obj),u,"POST","/api/v1/auth/logout",`{"refresh_token":"disposable-refresh"}`,nil)
 if w.Code!=500{t.Fatalf("logout reported success after revoke failure: %d %s",w.Code,w.Body.String())}
}
