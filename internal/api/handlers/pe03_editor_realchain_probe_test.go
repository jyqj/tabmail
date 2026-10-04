//go:build r5protocol

package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/config"
	"tabmail/internal/policy"
)

// Self-authored diagnostic only: reuses the unchanged disposable seed without
// calling any formal observer, acknowledgement, catalog acceptance or batch.
func TestPE03EditorRealChainProbe(t *testing.T) {
	if os.Getenv("TABMAIL_PE03_PROBE_OUTPUT") == "" || os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit owned PG and private probe output required")
	}
	root, err := filepath.Abs("../../..")
	r5UIMust(t, err)
	raw, err := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, err)
	hash := sha256.Sum256(raw)
	var catalog struct{ Cases []r5UICase }
	r5UIMust(t, json.Unmarshal(raw, &catalog))
	var c r5UICase
	for _, entry := range catalog.Cases {
		if entry.ID == "PE03" {
			c = entry
		}
	}
	if c.ID != "PE03" {
		t.Fatal("original PE03 absent")
	}
	for _, live := range []bool{false, true} {
		name := "original-buffered"
		if live {
			name = "live"
		}
		t.Run(name, func(t *testing.T) {
			f := r5UISeed(t)
			fixture := r5UISetup(t, f, c, "default", hex.EncodeToString(hash[:]))
			if live {
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				// Real router writes directly to the network, preserving SSE flushes.
				router := api.NewRouter(api.RouterConfig{Store: f.st, CompanyRepository: f.st, JWTSecret: r5UIJWT, MailboxTokenSecret: "ui-fixture-only", PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, f.st, 10000, nil), Logger: zerolog.Nop(), Readiness: f.st.Readiness})
				server := httptest.NewServer(router)
				t.Cleanup(server.Close)
				fixture["api_url"] = server.URL
				fixture["probe_live"] = true
			}
			private := filepath.Join(t.TempDir(), "fixture.json")
			payload, err := json.Marshal(fixture)
			r5UIMust(t, err)
			r5UIMust(t, os.WriteFile(private, payload, 0600))
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			defer cancel()
			report := filepath.Join(os.Getenv("TABMAIL_PE03_PROBE_OUTPUT"), name+".json")
			if _, err := os.Stat(report); !os.IsNotExist(err) {
				t.Fatal("fresh probe report required")
			}
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web/node_modules/vitest/vitest.mjs"), "run", "--config", "vitest.pe03-probe.config.ts", "--reporter=json", "--outputFile", report)
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE="+private, "TABMAIL_PE03_PROBE_SUMMARY="+filepath.Join(os.Getenv("TABMAIL_PE03_PROBE_OUTPUT"), name+"-summary.json"))
			logs, runErr := cmd.CombinedOutput()
			r5UIMust(t, os.WriteFile(filepath.Join(os.Getenv("TABMAIL_PE03_PROBE_OUTPUT"), name+".log"), logs, 0600))
			if runErr != nil {
				t.Fatalf("probe failed: %v; private diagnostic log retained", runErr)
			}
			var canSend bool
			var description string
			r5UIMust(t, f.pool.QueryRow(ctx, "SELECT can_send,description FROM permission_profiles WHERE id=$1", fixture["profile_id"]).Scan(&canSend, &description))
			if canSend || description != "Shared stale description" {
				t.Fatal("persistent post-save state violated revocation or draft intent")
			}
			t.Log("shipping stale alert + disabled Save + retained draft; direct old-security CAS409; reviewed description-only Save200; no restored sending")
		})
	}
}
