//go:build r5fixtures

package testpg_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"tabmail/internal/testpg"
	"testing"
	"time"
)

type r5CleanupIdentity struct {
	Database, HTTP, SMTP string
	CleanupObserved      bool
}

func TestR5FixtureFailureCleanup(t *testing.T) {
	if os.Getenv("TABMAIL_R5_FAILURE_CLEANUP_CHILD") == "1" {
		var f *testpg.R5HTTPFixture
		var identity r5CleanupIdentity
		// Registered before construction: this observer runs AFTER every helper
		// cleanup while the child process is still alive, not merely after OS exit.
		t.Cleanup(func() {
			if f == nil {
				return
			}
			select {
			case <-f.Relay.Closed():
			default:
				t.Error("SMTP helper cleanup observer did not see Closed")
				return
			}
			for _, address := range []string{identity.HTTP, identity.SMTP} {
				conn, err := net.DialTimeout("tcp", address, time.Second)
				if err == nil {
					conn.Close()
					t.Error("helper socket cleanup missing before child exit")
					return
				}
			}
			identity.CleanupObserved = true
			b, err := json.Marshal(identity)
			if err != nil {
				t.Error("cleanup metadata encode failed")
				return
			}
			if err = os.WriteFile(os.Getenv("TABMAIL_R5_FAILURE_CLEANUP_IDENTITY"), b, 0600); err != nil {
				t.Error("cleanup metadata write failed")
			}
		})
		f = testpg.NewR5HTTPFixture(t)
		parsed, err := url.Parse(f.Server.URL)
		if err != nil {
			t.Fatal(err)
		}
		cfg := f.Relay.Config()
		identity = r5CleanupIdentity{Database: f.Pool.Config().ConnConfig.Database, HTTP: parsed.Host, SMTP: net.JoinHostPort(cfg.RelayHost, fmt.Sprint(cfg.RelayPort))}
		b, err := json.Marshal(identity)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(os.Getenv("TABMAIL_R5_FAILURE_CLEANUP_IDENTITY"), b, 0600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("owned fixture deliberate failure cleanup proof")
	}
	sentinel := testpg.NewR5Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "owned-resource-identity.json")
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestR5FixtureFailureCleanup$", "-test.count=1", "-test.timeout=35s")
	child.Env = append(os.Environ(), "TABMAIL_R5_FAILURE_CLEANUP_CHILD=1", "TABMAIL_R5_FAILURE_CLEANUP_IDENTITY="+path)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytesContainsOwnedFailure(output) {
		t.Fatal("expected isolated deliberate child failure was not observed (private output omitted)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("owned child resource identity missing")
	}
	var identity r5CleanupIdentity
	if err = json.Unmarshal(raw, &identity); err != nil {
		t.Fatal(err)
	}
	if !identity.CleanupObserved {
		t.Fatal("child process exit is not proof of helper cleanup callbacks")
	}
	if !strings.HasPrefix(identity.Database, "tm_test_") || identity.Database == sentinel.Pool.Config().ConnConfig.Database {
		t.Fatal("child database is not a distinct test-owned resource")
	}
	var exists bool
	if err = sentinel.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, identity.Database).Scan(&exists); err != nil || exists {
		t.Fatal("failed fixture did not drop only its owned database", err)
	}
	for _, address := range []string{identity.HTTP, identity.SMTP} {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "127.0.0.1" {
			t.Fatal("unsafe/non-loopback child identity")
		}
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			connection.Close()
			t.Fatal("failed fixture listener still accepts connections")
		}
	}
	if err = sentinel.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id=$1)`, sentinel.Companies[0].Tenant.ID).Scan(&exists); err != nil || !exists {
		t.Fatal("child cleanup altered another owned fixture", err)
	}
}
func bytesContainsOwnedFailure(value []byte) bool {
	return strings.Contains(string(value), "owned fixture deliberate failure cleanup proof")
}
