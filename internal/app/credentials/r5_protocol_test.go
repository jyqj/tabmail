package credentials_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tabmail/internal/app/credentials"
)

// No policy implementation lives here: shared input is passed unchanged to the
// production validator and its normalized output is compared with target data.
func TestR5ProtocolAuditReasonSharedCases(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Cases []struct {
			ID    string `json:"id"`
			Input struct {
				Operation json.RawMessage `json:"operation"`
				Variants  []struct {
					Name, Value, Repeat, Prefix, Suffix string
					Count                               int
				} `json:"variants"`
			} `json:"input"`
			Expected struct {
				Variants []struct {
					Name       string
					Valid      bool
					Normalized string
				} `json:"variants"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	executed := 0
	for _, c := range manifest.Cases {
		if string(c.Input.Operation) != `"audit_reason_policy"` {
			continue
		}
		if len(c.Input.Variants) == 0 || len(c.Input.Variants) != len(c.Expected.Variants) {
			t.Fatal("missing shared audit reason variants")
		}
		executed++
		for i, v := range c.Input.Variants {
			t.Run(c.ID+"/"+v.Name, func(t *testing.T) {
				want := c.Expected.Variants[i]
				if want.Name != v.Name {
					t.Fatal("input/expected variant mismatch")
				}
				value := v.Value
				if v.Repeat != "" {
					if v.Count < 0 || v.Count > 10000 {
						t.Fatal("invalid repeat input")
					}
					value = v.Prefix + strings.Repeat(v.Repeat, v.Count) + v.Suffix
				}
				got, err := credentials.AuditReason(value)
				if (err == nil) != want.Valid || got != want.Normalized {
					t.Fatalf("production AuditReason: bytes=%d got %q err=%v; valid=%v normalized=%q", len(value), got, err, want.Valid, want.Normalized)
				}
				if !want.Valid && !errors.Is(err, credentials.ErrAuditReason) {
					t.Fatalf("wrong rejection sentinel: %v", err)
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("expected one shared audit reason case, got %d", executed)
	}
}
