package testpg

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const fixtureSetupChildMode = "TABMAIL_TESTPG_SETUP_CHILD_MODE"

// Exercise the real pgx connection path. The listener accepts TCP but never
// answers PostgreSQL's startup message, so only cancellation can end setup.
// A child test isolates NewPostgres's fatal failure from the parent assertions.
func TestPostgresFixtureSetupDeadlineAndCancellation(t *testing.T) {
	if mode := os.Getenv(fixtureSetupChildMode); mode != "" {
		if mode == "cancelled-parent" {
			// testing cancels t.Context before running cleanup callbacks.
			t.Cleanup(func() { NewPostgres(t) })
			return
		}
		if mode != "deadline" {
			t.Fatal("unknown fixture setup child mode")
		}
		NewPostgres(t)
		t.Fatal("blocked PostgreSQL startup unexpectedly initialized a fixture")
	}

	for _, tc := range []struct {
		name        string
		testTimeout string
		wantDial    bool
	}{
		{name: "deadline", testTimeout: "12s", wantDial: true},
		{name: "cancelled-parent", testTimeout: "3s", wantDial: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			accepted := make(chan net.Conn, 1)
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				if conn, err := listener.Accept(); err == nil {
					accepted <- conn
				}
			}()
			t.Cleanup(func() {
				_ = listener.Close()
				<-joined
				select {
				case conn := <-accepted:
					_ = conn.Close()
				default:
				}
			})
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable,
				"-test.run=^TestPostgresFixtureSetupDeadlineAndCancellation$",
				"-test.timeout="+tc.testTimeout, "-test.v")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, fixtureSetupChildMode+"=") &&
					!strings.HasPrefix(entry, "TABMAIL_TEST_DB_DSN=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, fixtureSetupChildMode+"="+tc.name,
				"TABMAIL_TEST_DB_DSN=postgres://fixture_test:disposable@"+listener.Addr().String()+"/fixture_test?sslmode=disable")
			started := time.Now()
			output, err := cmd.CombinedOutput()
			elapsed := time.Since(started)
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
				t.Fatalf("setup must return an ordinary test failure, got exit %v after %s: %s", err, elapsed, output)
			}
			if ctx.Err() != nil || elapsed > 6*time.Second || strings.Contains(string(output), "panic: test timed out") {
				t.Fatalf("fixture setup did not leave time for cleanup: elapsed=%s output=%s", elapsed, output)
			}
			if strings.Contains(string(output), "unexpectedly initialized") {
				t.Fatalf("setup accepted a listener that never completed the PostgreSQL handshake: %s", output)
			}
			if tc.wantDial {
				select {
				case conn := <-accepted:
					_ = conn.Close()
				default:
					t.Fatal("deadline regression did not reach the real PostgreSQL connection path")
				}
			} else {
				select {
				case conn := <-accepted:
					_ = conn.Close()
					t.Fatal("an already cancelled test started a PostgreSQL connection")
				default:
				}
			}
		})
	}
}
