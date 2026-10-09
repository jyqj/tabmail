//go:build r5protocol

package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Diagnostic output contains field names/types only, never response values.
func r5ExternalShape(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, member := range value {
			out[key] = r5ExternalShape(member)
		}
		return out
	case []any:
		if len(value) == 0 {
			return "array(empty)"
		}
		return []any{r5ExternalShape(value[0])}
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}
func TestR5ExternalRC01ResponseShape(t *testing.T) {
	out := os.Getenv("TABMAIL_R5_EXTERNAL_RC01_SHAPE")
	if out == "" {
		t.Fatal("explicit private shape output required")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	raw, err := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, err)
	var cases struct{ Cases []r5UICase }
	r5UIMust(t, json.Unmarshal(raw, &cases))
	hash := sha256.Sum256(raw)
	for _, c := range cases.Cases {
		if c.ID != "RC01" {
			continue
		}
		f := r5UISeed(t)
		fixture := r5UISetup(t, f, c, "default", hex.EncodeToString(hash[:]))
		req, err := http.NewRequest(http.MethodGet, fmt.Sprint(fixture["api_url"])+"/api/v1/company/submissions/"+fmt.Sprint(fixture["submission_id"]), nil)
		r5UIMust(t, err)
		req.Header.Set("Authorization", "Bearer "+fixture["auth"].(map[string]any)["token"].(string))
		response, err := f.server.Client().Do(req)
		r5UIMust(t, err)
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("HTTP status %d", response.StatusCode)
		}
		var data any
		r5UIMust(t, json.NewDecoder(response.Body).Decode(&data))
		safe := map[string]any{"case": "RC01/default", "http_status": response.StatusCode, "shape": r5ExternalShape(data)}
		encoded, err := json.MarshalIndent(safe, "", "  ")
		r5UIMust(t, err)
		r5UIMust(t, os.WriteFile(out, encoded, 0600))
		return
	}
	t.Fatal("missing RC01 original input")
}

func TestR5ExternalRuntimeProbe(t *testing.T) {
	report := os.Getenv("TABMAIL_R5_EXTERNAL_PROBE_REPORT")
	if report == "" {
		t.Fatal("fresh private probe report required")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	launcher, node, cli := r5UIExternalRuntime(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	cmd := r5UIExternalVitestCommand(ctx, root, launcher, node, cli, "vitest.r5external-probe.config.ts", report)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real external probe failed: %v: %s", err, output)
	}
	raw, err := os.ReadFile(report)
	r5UIMust(t, err)
	var result struct {
		NumTotalTests, NumPassedTests, NumFailedTests, NumPendingTests, NumRuntimeErrorTestSuites int
		Success                                                                                   bool
	}
	r5UIMust(t, json.Unmarshal(raw, &result))
	if !result.Success || result.NumTotalTests != 1 || result.NumPassedTests != 1 || result.NumFailedTests != 0 || result.NumPendingTests != 0 || result.NumRuntimeErrorTestSuites != 0 {
		t.Fatal("probe missing, skipped or nonpassing")
	}
}
