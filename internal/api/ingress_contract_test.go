package api

import (
	"strings"
	"testing"
)

func TestIngressOpenAPIDocumentIncludesRecoveryPaths(t *testing.T) {
	doc, err := openAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"  /api/v1/admin/ingest/jobs/{id}/recipients:", "  /api/v1/admin/ingest/jobs/{id}/retry:"} {
		if strings.Count(string(doc), path) != 1 {
			t.Fatalf("missing or duplicate operation %s", path)
		}
	}
	if strings.Count(string(doc), "\npaths:") != 1 || strings.Count(string(doc), "\ncomponents:") != 1 {
		t.Fatal("duplicate OpenAPI roots")
	}
}
