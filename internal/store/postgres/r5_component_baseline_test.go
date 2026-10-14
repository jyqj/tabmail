//go:build r5audit

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

type r5ComponentCall struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// P0 evidence only. A real Go test fixture owns the database, API and temporary
// credentials. Child Vitest mounts the shipping editor source in jsdom; this
// is component/HTTP/DB evidence, NOT a shipping-image browser certification.
func runR5ComponentBaseline(t *testing.T, code string) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("disposable DB required; no skipped component baseline")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("Node and installed frontend dependencies required")
	}
	f := seedCompany(t)
	ctx := context.Background()
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj) // Never start a delivery worker.
	handler := companyRouter(t, f, obj, svc)
	var mu sync.Mutex
	calls := []r5ComponentCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" || r.Method == "PATCH" {
			b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "fixture capture failed", 500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			mu.Lock()
			calls = append(calls, r5ComponentCall{r.Method, r.URL.Path, json.RawMessage(b)})
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	token := r3Token(t, f.admin)
	var profile models.PermissionProfile
	if code == "A01" {
		status, _ := r5AuditHTTP(t, server, token, "PUT", "/api/v1/admin/users/"+f.employee.ID.String()+"/permissions", map[string]any{"can_send": false, "allowed_zone_ids": []string{f.zone.ID.String()}})
		r5ExpectStatus(t, status, 200)
	} else {
		status, b := r5AuditHTTP(t, server, token, "POST", "/api/v1/admin/permissions", map[string]any{"name": "R5 component profile", "can_send": true})
		r5ExpectStatus(t, status, 201)
		var envelope struct {
			Data models.PermissionProfile `json:"data"`
		}
		must(t, json.Unmarshal(b, &envelope))
		profile = envelope.Data
	}
	mu.Lock()
	calls = nil
	mu.Unlock() // Only child-generated writes enter evidence.
	temp := t.TempDir()
	fixture := map[string]any{"case": code, "url": server.URL, "token": token, "admin": map[string]any{"id": f.admin.ID, "tenant_id": f.tenant.ID, "role": "admin", "email": f.admin.Email, "display_name": f.admin.DisplayName}, "employee_id": f.employee.ID, "employee_email": f.employee.Email, "zone_id": f.zone.ID, "profile_id": profile.ID, "profile_name": profile.Name}
	b, err := json.Marshal(fixture)
	must(t, err)
	fixturePath := filepath.Join(temp, "fixture.json")
	must(t, os.WriteFile(fixturePath, b, 0600))
	evidenceRoot := os.Getenv("TABMAIL_R5_COMPONENT_EVIDENCE")
	if evidenceRoot == "" {
		evidenceRoot = filepath.Join(temp, "evidence")
	}
	out := filepath.Join(evidenceRoot, code)
	must(t, os.MkdirAll(out, 0700))
	reportPath := filepath.Join(out, "vitest.json")
	if _, err = os.Stat(reportPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fresh component evidence directory required")
	}
	webDir, err := filepath.Abs("../../../web")
	must(t, err)
	childCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "node", filepath.Join(webDir, "node_modules/vitest/vitest.mjs"), "run", "--config", "vitest.r5audit.config.ts", "--reporter=json", "--outputFile", reportPath)
	cmd.Dir = webDir
	cmd.Env = append(os.Environ(), "TABMAIL_R5_COMPONENT_FIXTURE="+fixturePath)
	output, runErr := cmd.CombinedOutput()
	must(t, os.WriteFile(filepath.Join(out, "vitest.log"), output, 0600))
	if childCtx.Err() != nil {
		t.Fatal("component process timed out")
	}
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("expected secure-behavior failure, not success/setup; child error %v", runErr)
	}
	raw, err := os.ReadFile(reportPath)
	must(t, err)
	var report struct {
		NumTotalTests             int `json:"numTotalTests"`
		NumFailedTests            int `json:"numFailedTests"`
		NumPendingTests           int `json:"numPendingTests"`
		NumRuntimeErrorTestSuites int `json:"numRuntimeErrorTestSuites"`
		TestResults               []struct {
			AssertionResults []struct {
				FullName        string   `json:"fullName"`
				Status          string   `json:"status"`
				FailureMessages []string `json:"failureMessages"`
			} `json:"assertionResults"`
		} `json:"testResults"`
	}
	must(t, json.Unmarshal(raw, &report))
	if report.NumTotalTests != 1 || report.NumFailedTests != 1 || report.NumPendingTests != 0 || report.NumRuntimeErrorTestSuites != 0 || len(report.TestResults) != 1 || len(report.TestResults[0].AssertionResults) != 1 {
		t.Fatal("incomplete component execution; inspect child evidence")
	}
	assertion := report.TestResults[0].AssertionResults[0]
	if assertion.FullName != code+" secure editor behavior" || assertion.Status != "failed" || strings.Count(strings.Join(assertion.FailureMessages, "\n"), "R5_COMPONENT_DEFECT_"+code+":") != 1 {
		t.Fatal("component failed before target security assertion; inspect child evidence")
	}
	mu.Lock()
	observed := append([]r5ComponentCall(nil), calls...)
	mu.Unlock()
	var state any
	if code == "A01" {
		after, e := f.st.EffectivePermission(ctx, f.employee.ID)
		must(t, e)
		if len(observed) != 1 || observed[0].Method != "PUT" || !strings.HasSuffix(observed[0].Path, "/"+f.employee.ID.String()+"/permissions") {
			t.Fatal("unexpected component mutation sequence")
		}
		var body map[string]any
		must(t, json.Unmarshal(observed[0].Body, &body))
		if len(body) != 1 || body["daily_send_quota"] != float64(25) || !after.CanSend || len(after.AllowedZoneIDs) != 0 {
			t.Fatal("component request and DB state do not corroborate A01")
		}
		state = map[string]any{"can_send": after.CanSend, "restricted_zone_count": len(after.AllowedZoneIDs), "daily_send_quota": after.DailySendQuota}
	} else {
		after, e := f.st.GetPermissionProfile(ctx, profile.ID)
		must(t, e)
		if len(observed) != 2 || after == nil || !after.CanSend || after.Description != "R5 stale description" {
			t.Fatal("component requests and DB state do not corroborate A02")
		}
		for _, call := range observed {
			if call.Method != "PATCH" || call.Path != "/api/v1/admin/permissions/"+profile.ID.String() {
				t.Fatal("unexpected profile mutation")
			}
		}
		var revoked, stale map[string]any
		must(t, json.Unmarshal(observed[0].Body, &revoked))
		must(t, json.Unmarshal(observed[1].Body, &stale))
		if revoked["can_send"] != false || stale["can_send"] != true {
			t.Fatal("no observed revoke followed by stale restore")
		}
		state = map[string]any{"can_send": after.CanSend, "description": after.Description}
	}
	proof := map[string]any{"case": code, "product_fixed": false, "component": "original React editor and Base UI; auth context fixture only", "transport": "unmodified API/session clients and real loopback HTTP", "database": "isolated PostgreSQL", "requests": observed, "after": state, "vitest_failed_tests": 1, "vitest_skipped_tests": 0}
	proofBytes, e := json.MarshalIndent(proof, "", "  ")
	must(t, e)
	must(t, os.WriteFile(filepath.Join(out, "corroboration.json"), append(proofBytes, '\n'), 0600))
	t.Fatal(fmt.Sprintf("R5_COMPONENT_BASELINE_%s: original editor target failure corroborated by HTTP writes and PostgreSQL", code))
}
func TestR5ComponentA01PreservesOverrides(t *testing.T)  { runR5ComponentBaseline(t, "A01") }
func TestR5ComponentA02RejectsStaleProfile(t *testing.T) { runR5ComponentBaseline(t, "A02") }
