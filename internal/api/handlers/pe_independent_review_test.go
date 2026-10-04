//go:build r5protocol

package handlers_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPEIndependentUnmount(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("Explicit owned PG DSN required")
	}
	var f *r5UIFixture
	t.Cleanup(func() {
		if f != nil {
			r5TransportAssertReleased(t, f.pool.Config().ConnConfig.Database, f.server.Listener.Addr().String())
		}
	})
	f = r5UISeed(t)
	raw, err := json.Marshal(map[string]any{"api_url": f.server.URL, "token": r5UIToken(t, f.admin), "user": f.admin})
	r5UIMust(t, err)
	fixture := filepath.Join(t.TempDir(), "fixture.json")
	r5UIMust(t, os.WriteFile(fixture, raw, 0600))
	_, file, _, _ := runtime.Caller(0)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "node_modules/vitest/vitest.mjs", "run", "--config", "vitest.pe-independent.config.ts")
	cmd.Dir = filepath.Join(filepath.Dir(file), "../../../web")
	cmd.Env = append(os.Environ(), "TABMAIL_PE_INDEPENDENT_FIXTURE="+fixture)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Independent unmount probe failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
