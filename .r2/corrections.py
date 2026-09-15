from patchutil import *
import json

p='web/app/(dashboard)/console/mailboxes/page.tsx'
assert 'useState<AccessMode>("token")' in read(p)
replace(p,'placeholder={t("mailboxes.inheritDefault")}','placeholder={t("mailboxes.retentionPermanentHint")}')
replace(p,'Number.isNaN(retentionHours)','!Number.isInteger(retentionHours)')
p='internal/store/postgres/migrate.go'
replace(p,'version_id NOT IN (0,1,2)','version_id NOT IN (0,1,2,3)')
p='internal/store/postgres/outbound.go'
s=read(p);start=s.index('func (s *PgStore) ListOutboundJobsScoped');end=s.index('\n// Claim one job',start)
part=s[start:end];part=part.replace('\n\twhereSQL := strings.Join(where, " AND ")','');part=part.replace('\n\tvar total int','\n whereSQL := strings.Join(where, " AND ")\n\tvar total int');Path(p).write_text(s[:start]+part+s[end:])
p='internal/store/postgres/release_r2_test.go'
replace(p,'mb:=&models.Mailbox{TenantID:tenant.ID,ZoneID:zone.ID,FullAddress:"shared@scope.test",LocalPart:"shared",ResolvedDomain:zone.Domain,AccessMode:models.AccessToken}', 'passwordHash:="fixture-password-hash"\n mb:=&models.Mailbox{TenantID:tenant.ID,ZoneID:zone.ID,FullAddress:"shared@scope.test",LocalPart:"shared",ResolvedDomain:zone.Domain,AccessMode:models.AccessToken,PasswordHash:&passwordHash}')

for locale in ['zh','en']:
 p=f'web/locales/{locale}.json';messages=json.loads(read(p))
 assert 'mailboxes.retentionError' in messages
 messages['mailboxes.retentionError']='留存小时数必须是非负整数（0 表示永久保留）' if locale=='zh' else 'Retention hours must be a non-negative integer (0 means permanent)'
 messages['mailboxes.retentionPermanentHint']='0 = 永久保留；留空继承默认值' if locale=='zh' else '0 = permanent; empty inherits default'
 Path(p).write_text(json.dumps(messages,ensure_ascii=False,indent=2)+'\n')

p='internal/api/release_r2_regression_test.go'
Path(p).write_text(read(p)+r'''
func TestR2SuperAdminSelectedTenantAndOwnerValidation(t *testing.T) {
 st,obj,company:=seededStores(t);ctx:=context.Background()
 other:=&models.Tenant{Name:"Platform",PlanID:uuid.MustParse("00000000-0000-0000-0000-000000000001")}
 if err:=st.CreateTenant(ctx,other);err!=nil{t.Fatal(err)}
 super:=seedUserForTest(t,st,other.ID,models.RoleSuperAdmin)
 admin:=seedUserForTest(t,st,company,models.RoleAdmin)
 h:=r2Router(t,st,obj)
 req:=httptest.NewRequest("PATCH","/api/v1/admin/users/"+admin.ID.String(),strings.NewReader(`{"display_name":"Managed company admin"}`))
 req.Header.Set("Content-Type","application/json");req.Header.Set("Authorization","Bearer "+issueAccessTokenForExistingUser(t,super));req.Header.Set("X-Tenant-ID",company.String())
 rr:=httptest.NewRecorder();h.ServeHTTP(rr,req)
 if rr.Code!=200 {t.Fatalf("selected-company super admin denied: %d %s",rr.Code,rr.Body.String())}
 foreign:=seedUserForTest(t,st,other.ID,models.RoleUser)
 inactive:=seedUserForTest(t,st,company,models.RoleUser);inactive.IsActive=false;if err:=st.UpdateUser(ctx,inactive);err!=nil{t.Fatal(err)}
 for i,id:=range []uuid.UUID{foreign.ID,inactive.ID} {
  rr=r2Request(t,h,admin,"POST","/api/v1/mailboxes",fmt.Sprintf(`{"address":"invalid-owner-%d@mail.test","password":"Passw0rd!","owner_user_id":"%s"}`,i,id))
  if rr.Code!=400 {t.Fatalf("foreign/inactive owner accepted: %d %s",rr.Code,rr.Body.String())}
 }
}
''')
p='internal/api/handlers/release_r2_refresh_test.go'
Path(p).write_text(read(p)+r'''
type r2LogoutFailure struct{*testutil.FakeStore}
func(s r2LogoutFailure) RevokeRefreshTokenByHash(context.Context,string)error{return errors.New("injected logout failure")}
func TestR2LogoutDoesNotClaimRevocationOnError(t *testing.T){
 h:=NewAuthHandler(r2LogoutFailure{testutil.NewFakeStore()},"jwt-test-secret",uuid.Nil,false,nil,true,zerolog.Nop())
 rr:=httptest.NewRecorder();r:=httptest.NewRequest("POST","/api/v1/auth/logout",strings.NewReader(`{"refresh_token":"old"}`));h.Logout(rr,r)
 if rr.Code!=500{t.Fatalf("logout failure hidden: %d %s",rr.Code,rr.Body.String())}
 for _,cookie:=range rr.Result().Cookies(){if cookie.Name==RefreshCookieName && cookie.MaxAge<0{t.Fatal("failed server logout pretended to clear session")}}
}
''')
