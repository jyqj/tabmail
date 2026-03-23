package sanitize

import (
	"strings"
	"testing"
)

func TestHTMLRemovesUnsafeContentAndStyles(t *testing.T) {
	out, err := HTML(`<div style="color:red;position:absolute" onclick="x()"><script>alert(1)</script><style>p{position:absolute;color:red}</style><p style="font-weight:bold;position:absolute">hi</p></div>`)
	if err != nil {
		t.Fatalf("sanitize html: %v", err)
	}

	for _, bad := range []string{"<script", "<style", "onclick", "position:absolute"} {
		if strings.Contains(out, bad) {
			t.Fatalf("expected sanitized html to remove %q: %s", bad, out)
		}
	}
	for _, want := range []string{`style="color:red;"`, `style="font-weight:bold;"`, ">hi<"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected sanitized html to contain %q: %s", want, out)
		}
	}
}

func TestHTMLDropsStyleAttributeWhenNothingSafeRemains(t *testing.T) {
	out, err := HTML(`<div style="position:absolute" data-x="1">content</div>`)
	if err != nil {
		t.Fatalf("sanitize html: %v", err)
	}
	if strings.Contains(out, `style="`) {
		t.Fatalf("expected empty unsafe style attribute to be removed: %s", out)
	}
	if !strings.Contains(out, "content") {
		t.Fatalf("expected content to remain: %s", out)
	}
}
