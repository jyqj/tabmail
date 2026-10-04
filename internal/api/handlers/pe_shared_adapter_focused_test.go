//go:build r5protocol

package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Dedicated local-runtime diagnostic, not the formal collector/qualification
// producer. Reuses unchanged Go-owned catalog setup and shipping TSX consumers.
func TestPESharedAdapterFocused(t *testing.T) {
	evidence := os.Getenv("TABMAIL_PE_FOCUSED_EVIDENCE")
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" || evidence == "" {
		t.Fatal("explicit owned PG DSN and private evidence root required")
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	raw, err := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, err)
	digest := sha256.Sum256(raw)
	var catalog struct{ Cases []r5UICase }
	r5UIMust(t, json.Unmarshal(raw, &catalog))
	count := 0
	for _, c := range catalog.Cases {
		if c.ID != "PE01" && c.ID != "PE02" && c.ID != "PE03" && c.ID != "PE04" {
			continue
		}
		variants := []string{"default"}
		if c.ID == "PE02" {
			variants = []string{"omitted", "null", "false", "0", "[]"}
		}
		for _, variant := range variants {
			count++
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				var f *r5UIFixture
				t.Cleanup(func() {
					if f != nil {
						r5TransportAssertReleased(t, f.pool.Config().ConnConfig.Database, f.server.Listener.Addr().String())
					}
				})
				f = r5UISeed(t)
				packet := r5UISetup(t, f, c, variant, hex.EncodeToString(digest[:]))
				private := filepath.Join(t.TempDir(), "fixture.json")
				encoded, err := json.Marshal(packet)
				r5UIMust(t, err)
				r5UIMust(t, os.WriteFile(private, encoded, 0600))
				name := c.ID + "-" + strings.ReplaceAll(variant, "[]", "empty")
				streamReport := filepath.Join(evidence, name+"-stream.json")
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "node", "node_modules/vitest/vitest.mjs", "run", "--config", "vitest.r5protocol.config.ts", "--reporter=json", "--outputFile", filepath.Join(evidence, name+"-vitest.json"))
				cmd.Dir = filepath.Join(root, "web")
				cmd.Env = append(os.Environ(), "TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE="+private, "TABMAIL_PE_FOCUSED_STREAM_REPORT="+streamReport)
				output, runErr := cmd.CombinedOutput()
				r5UIMust(t, os.WriteFile(filepath.Join(evidence, name+".log"), output, 0600))
				var stream struct {
					Ready    int  `json:"ready_frames"`
					Profile  int  `json:"profile_update_frames"`
					Override int  `json:"override_patch_frames"`
					Errors   int  `json:"stream_errors"`
					Released bool `json:"all_streams_released"`
					UI       bool `json:"ui_blocked"`
					CAS      bool `json:"direct_cas409"`
				}
				report, err := os.ReadFile(streamReport)
				r5UIMust(t, err)
				r5UIMust(t, json.Unmarshal(report, &stream))
				t.Logf("wire ready=%d profile_update=%d override_patch=%d errors=%d released=%v UIblocked=%v directCAS409=%v", stream.Ready, stream.Profile, stream.Override, stream.Errors, stream.Released, stream.UI, stream.CAS)
				if runErr != nil {
					t.Fatalf("shipping component probe failed; private diagnostic: %s", filepath.Join(evidence, name+".log"))
				}
				if stream.Ready < 1 || stream.Errors != 0 || !stream.Released {
					t.Fatal("genuine ready or stream cleanup missing")
				}
				if c.ID == "PE03" && (stream.Profile < 1 || !stream.UI || !stream.CAS) {
					t.Fatal("profile wire invalidation/UI block/direct CAS chain missing")
				}
				if c.ID == "PE04" && (stream.Override < 2 || !stream.UI || !stream.CAS) {
					t.Fatal("reset/restoration invalidation/UI block/direct CAS chain missing")
				}
				// Independent persistent observation, not effective-to-raw inference.
				var send *bool
				var quota *int
				var mode string
				if c.ID == "PE03" {
					var canSend bool
					var description string
					r5UIMust(t, f.pool.QueryRow(context.Background(), `SELECT can_send,description FROM permission_profiles WHERE id=$1`, packet["profile_id"]).Scan(&canSend, &description))
					if canSend || description != "" {
						t.Fatal("persisted profile revocation/description differs")
					}
				} else {
					r5UIMust(t, f.pool.QueryRow(context.Background(), `SELECT can_send,daily_send_quota,domain_access_mode FROM user_permission_overrides WHERE user_id=$1`, f.employee.ID).Scan(&send, &quota, &mode))
					if variant == "null" {
						if send != nil {
							t.Fatal("raw null reset missing")
						}
					} else if send == nil || *send {
						t.Fatal("raw false restriction missing")
					}
					expectedQuota := 19
					if c.ID == "PE01" || variant == "omitted" {
						expectedQuota = 25
					}
					if variant == "0" {
						expectedQuota = 0
					}
					if quota == nil || *quota != expectedQuota {
						t.Fatal("raw quota intent differs")
					}
					expectedMode := "list"
					if variant == "[]" {
						expectedMode = "all"
					}
					if mode != expectedMode {
						t.Fatal("raw domain intent differs")
					}
				}
			})
		}
	}
	if count != 8 {
		t.Fatalf("focused PE variant coverage drift: %d", count)
	}
}
