from patchutil import *

p='internal/testutil/fake_store_mailboxes.go'
replace(p,'if len(ownerZones) == 0 {','if len(ownerZones) == 0 && scope.GrantedUserID == nil {')
p='web/app/(dashboard)/console/mailboxes/page.tsx'
replace(p,'useState<"public" | "api_key" | "token">("public")','useState<"public" | "api_key" | "token">("token")')

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
