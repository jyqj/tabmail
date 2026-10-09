package postgres_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const r5GCScannerChildArgument = "--r5-gc-scanner-child"
const r5GCScannerChildDSN = "TABMAIL_R5_GC_CHILD_DSN"

// The package had no existing TestMain. Normal execution, including -test.list
// and an accidentally inherited child DSN, goes through the unchanged m.Run.
// Child mode is an exact private argv protocol, never a registered fake test.
func TestMain(m *testing.M) {
	privateArgument := false
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--r5-gc-") {
			privateArgument = true
		}
	}
	if !privateArgument {
		os.Exit(m.Run())
	}
	if len(os.Args) != 2 || os.Args[1] != r5GCScannerChildArgument {
		fmt.Fprintln(os.Stderr, "invalid GC scanner child arguments")
		os.Exit(2)
	}
	dsn := os.Getenv(r5GCScannerChildDSN)
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "missing GC scanner child fixture DSN")
		os.Exit(2)
	}
	// The ordinary helper returns only after its deferred store Close and
	// context cancellation. Never os.Exit from inside the owned DB lifetime.
	if err := r5RunGCTenantScannerProcess(dsn); err != nil {
		// PostgreSQL configuration errors may embed input; do not print a DSN.
		fmt.Fprintln(os.Stderr, "GC scanner child failed")
		os.Exit(1)
	}
	os.Exit(0)
}

func r5GCScannerEnvironment(dsn string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, r5GCScannerChildDSN+"=") {
			env = append(env, value)
		}
	}
	if dsn != "" {
		env = append(env, r5GCScannerChildDSN+"="+dsn)
	}
	return env
}

func TestR5AttachmentGCTenantProcessEntryProtocol(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		dsn     string
		exit    int
		present string
		absent  string
	}{
		{name: "entry_helper_not_registered", args: []string{"-test.list=^TestR5AttachmentGCScannerProcess$"}, absent: "TestR5AttachmentGCScannerProcess"},
		{name: "entry_normal_mode_ignores_child_env", args: []string{"-test.list=^TestR5AttachmentGCTenantActualProcessRestart$"}, dsn: "invalid-fixture-dsn-must-not-be-opened", present: "TestR5AttachmentGCTenantActualProcessRestart"},
		{name: "entry_child_missing_dsn", args: []string{r5GCScannerChildArgument}, exit: 2, present: "missing GC scanner child fixture DSN"},
		{name: "entry_unknown_child_argument", args: []string{"--r5-gc-unknown-child"}, exit: 2, present: "invalid GC scanner child arguments"},
		{name: "entry_duplicate_child_argument", args: []string{r5GCScannerChildArgument, r5GCScannerChildArgument}, exit: 2, present: "invalid GC scanner child arguments"},
		{name: "entry_child_with_unexpected_argument", args: []string{r5GCScannerChildArgument, "-test.v"}, exit: 2, present: "invalid GC scanner child arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], tc.args...)
			cmd.Env = r5GCScannerEnvironment(tc.dsn)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.exit {
				t.Fatalf("entry protocol exit: want=%d state=%v error=%v context=%v output=%s", tc.exit, cmd.ProcessState, err, ctx.Err(), out)
			}
			if tc.present != "" && !strings.Contains(string(out), tc.present) {
				t.Fatalf("entry protocol output missing %q: %s", tc.present, out)
			}
			if tc.absent != "" && strings.Contains(string(out), tc.absent) {
				t.Fatalf("ordinary full suite still registers the helper test: %s", out)
			}
			if tc.dsn != "" && strings.Contains(string(out), tc.dsn) {
				t.Fatal("entry protocol exposed child DSN")
			}
		})
	}
}
