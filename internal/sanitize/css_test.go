package sanitize

import (
	"strings"
	"testing"
)

func TestSanitizeStyleFiltersUnsupportedProperties(t *testing.T) {
	out := sanitizeStyle("color:red;position:absolute;width:100px;background-image:url(x);padding:1px;")

	for _, want := range []string{"color:red;", "width:100px;", "padding:1px;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected sanitized style to contain %q: %s", want, out)
		}
	}
	for _, bad := range []string{"position:absolute", "background-image"} {
		if strings.Contains(out, bad) {
			t.Fatalf("expected sanitized style to drop %q: %s", bad, out)
		}
	}
}
