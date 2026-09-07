package api

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"tabmail/internal/store"
)

// Field drift and route presence checks complement the Router and component
// integration tests; they do not claim to validate all OpenAPI/TypeScript semantics.
func TestIngressInspectionFrontendContract(t *testing.T) {
	raw, err := os.ReadFile("../../web/lib/api/ingress-recovery.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, dto := range []struct {
		name string
		typ  reflect.Type
	}{{"IngressInspection", reflect.TypeOf(store.IngressInspection{})}, {"IngressTarget", reflect.TypeOf(store.IngressTarget{})}} {
		match := regexp.MustCompile(`(?s)export interface ` + dto.name + ` \{(.*?)\n\}`).FindStringSubmatch(string(raw))
		if len(match) != 2 {
			t.Fatalf("missing TypeScript interface %s", dto.name)
		}
		fields := map[string]bool{}
		for _, field := range regexp.MustCompile(`(?m)^\s+(\w+)\??:`).FindAllStringSubmatch(match[1], -1) {
			fields[field[1]] = true
		}
		for i := 0; i < dto.typ.NumField(); i++ {
			tag := strings.Split(dto.typ.Field(i).Tag.Get("json"), ",")[0]
			if !fields[tag] {
				t.Errorf("missing frontend field %s.%s", dto.name, tag)
			}
			delete(fields, tag)
		}
		for field := range fields {
			t.Errorf("unbacked frontend field %s.%s", dto.name, field)
		}
	}
	doc, err := openAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(doc), "  /api/v1/admin/ingest/jobs/{id}:\n") != 1 {
		t.Fatal("missing/duplicate inspection route")
	}
	if !strings.Contains(string(doc), "observed_updated_at:") {
		t.Fatal("reviewed retry revision undocumented")
	}
}
