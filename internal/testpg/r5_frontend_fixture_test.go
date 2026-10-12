//go:build r5fixtures

package testpg_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"tabmail/internal/testpg"
)

// This owner supplies one real fixture to the complete default frontend suite.
// It does not qualify the separate eight-owner external batch or its catalog.
// r5fixtures is the existing explicit PG/HTTP fixture compilation context.
// The supervisor selects this exact test; the original ^TestR5Fixture suite
// retains its separate twelve required top-level tests.
func TestR5FrontendOwnedFixture(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required")
	}
	output := os.Getenv("TABMAIL_R5_FRONTEND_OUTPUT")
	python := os.Getenv("TABMAIL_R5_FRONTEND_PYTHON")
	if !filepath.IsAbs(output) || !filepath.IsAbs(python) {
		t.Fatal("explicit frontend supervisor paths required")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	commandBytes, err := os.ReadFile(filepath.Join(output, "command.json"))
	if err != nil {
		t.Fatalf("frontend command receipt missing: %T", err)
	}
	var command []string
	if err := json.Unmarshal(commandBytes, &command); err != nil {
		t.Fatalf("frontend command receipt malformed: %T", err)
	}
	var private string
	if !t.Run("default-suite", func(owner *testing.T) {
		_, pool, _ := testpg.NewPostgres(owner)
		// This is the complete default suite (206 seconds in the baseline CI),
		// not one 75-second external-batch case. The existing batch is unchanged.
		ctx, cancel := context.WithTimeout(owner.Context(), 300*time.Second)
		defer cancel()
		private = testpg.NewR5LoopbackProbe(owner, ctx, pool, "frontend/default")
		// The dedicated Python child owns and physically joins npm and all of
		// its descendants. The outer supervisor also owns this Go process.
		arguments := []string{"-B", filepath.Join(root, "scripts/run_r5_frontend_fixture.py"),
			"--vitest-child", "--output-dir", output, "--"}
		arguments = append(arguments, command...)
		cmd := exec.CommandContext(ctx, python, arguments...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE="+private)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			owner.Errorf("complete default frontend child failed: %T", err)
		}
	}) {
		t.Fatal("frontend execution or fixture cleanup failed")
	}
	// t.Run returns only after LIFO HTTP/PG/temporary-file cleanup. A passing
	// parent terminal therefore cannot precede fixture-owner completion.
	if _, err := os.Lstat(private); !os.IsNotExist(err) {
		t.Fatal("private fixture cleanup was not observed")
	}
}
