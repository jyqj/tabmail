package postgres

import (
	"github.com/google/uuid"
	"tabmail/internal/company"
	"testing"
)

func TestPermissionEditorPatchParameterIntent(t *testing.T) {
	if permissionFieldParam(company.PermissionField[bool]{Present: true, Value: false}) != false {
		t.Fatal("false lost")
	}
	if permissionFieldParam(company.PermissionField[int]{Present: true, Value: 0}) != 0 {
		t.Fatal("zero lost")
	}
	if permissionFieldParam(company.PermissionField[int]{Present: true, Inherit: true, Value: 7}) != nil {
		t.Fatal("inherit must SQL NULL")
	}
	if permissionFieldParam(company.PermissionField[int]{Value: 7}) != nil {
		t.Fatal("absent insert must SQL NULL")
	}
	for _, mode := range []string{"all", "none"} {
		got, zones := permissionDomainPatchParams(company.PermissionField[company.DomainAccess]{Present: true, Value: company.DomainAccess{Mode: mode}})
		ids, ok := zones.([]uuid.UUID)
		if got != mode || !ok || ids == nil || len(ids) != 0 {
			t.Fatalf("%s requires distinct mode and nonnil empty array: %v/%v", mode, got, zones)
		}
	}
	for _, field := range []company.PermissionField[company.DomainAccess]{{Present: true, Inherit: true}, {Present: true, Value: company.DomainAccess{Mode: "inherit"}}} {
		mode, zones := permissionDomainPatchParams(field)
		if mode != "inherit" || zones != nil {
			t.Fatal("inherit requires SQL NULL zones")
		}
	}
	fields := permissionPatchFields(company.PermissionPatch{CanSend: company.PermissionField[bool]{Present: true}, MaxDomains: company.PermissionField[int]{Present: true, Inherit: true}})
	if len(fields) != 2 || fields[0] != "can_send" || fields[1] != "max_domains" {
		t.Fatalf("audit intent fields: %v", fields)
	}
}
