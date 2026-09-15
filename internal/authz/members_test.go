package authz

import (
 "testing"
 "github.com/google/uuid"
 "tabmail/internal/models"
)
func TestCanManageTenantMember(t *testing.T){
 tenant:=uuid.New();other:=uuid.New()
 for _,tc:=range []struct{name string;actor Actor;target uuid.UUID;role models.UserRole;want bool}{
  {"admin employee",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleUser,true},
  {"admin peer",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleAdmin,false},
  {"admin platform",Actor{TenantID:tenant,IsAdmin:true},tenant,models.RoleSuperAdmin,false},
  {"ordinary",Actor{TenantID:tenant},tenant,models.RoleUser,false},
  {"cross tenant",Actor{TenantID:tenant,IsAdmin:true},other,models.RoleUser,false},
  {"selected super",Actor{TenantID:tenant,IsSuperAdmin:true},tenant,models.RoleAdmin,true},
  {"super must select",Actor{TenantID:other,IsSuperAdmin:true},tenant,models.RoleAdmin,false},
  {"no selected tenant",Actor{IsSuperAdmin:true},uuid.Nil,models.RoleUser,false},
 }{t.Run(tc.name,func(t *testing.T){if got:=CanManageTenantMember(tc.actor,tc.target,tc.role);got!=tc.want{t.Fatalf("got %v want %v",got,tc.want)}})}
}
