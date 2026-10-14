//go:build !r5benchmark

package fileobj

import (
	"context"
	"testing"
)

type diagnosticValueForbidden struct{ context.Context }

func (diagnosticValueForbidden) Value(any) any {
	panic("normal production diagnostic looked up context")
}
func TestR5FileDiagnosticDefaultDisabled(t *testing.T) {
	ctx := diagnosticValueForbidden{context.Background()}
	if beginFileDiagnostic(ctx, "file_sync") != nil {
		t.Fatal("normal-build observer enabled")
	}
}
