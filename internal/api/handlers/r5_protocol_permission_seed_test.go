//go:build r5protocol

package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/company"
)

// These are setup regressions, never formal component observations or case qualification.
func TestR5PermissionSeedSetup(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required; no skip")
	}
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	r5UIMust(t, err)
	digest := sha256.Sum256(raw)
	var catalog struct{ Cases []r5UICase }
	r5UIMust(t, json.Unmarshal(raw, &catalog))
	count := 0
	for _, c := range catalog.Cases {
		if c.ID != "PE01" && c.ID != "PE02" && c.ID != "PE04" {
			continue
		}
		variants := []string{"default"}
		if c.ID == "PE02" {
			variants = []string{"omitted", "null", "false", "0", "[]"}
		}
		for _, variant := range variants {
			count++
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := r5UISeed(t)
				packet := r5UISetup(t, f, c, variant, hex.EncodeToString(digest[:]))
				if string(packet["input"].(json.RawMessage)) != string(c.Input) || packet["variant"] != variant || packet["case_sha256"] != hex.EncodeToString(digest[:]) {
					t.Fatal("setup changed case input/identity")
				}
				if len(f.trace) != 3 || f.trace[0].Method != "GET" || f.trace[1].Method != "PATCH" || f.trace[2].Method != "GET" {
					t.Fatal("setup must use shipping GET/PATCH/GET")
				}
				for _, call := range f.trace {
					if call.Status != 200 || call.Path != "/api/v1/admin/users/"+f.employee.ID.String()+"/permission-editor" {
						t.Fatal("setup used unexpected HTTP command")
					}
				}
				snapshot := r5UIReadPermissionEditor(t, f, r5UIToken(t, f.admin))
				r5PermissionSeedPersistent(t, f, snapshot)
			})
		}
	}
	if count != 7 {
		t.Fatalf("setup coverage drift: %d", count)
	}
}

// Read actual stored columns through an independent observer connection. This
// checks persisted raw values and the trigger-owned revision, not Effective.
func r5PermissionSeedPersistent(t *testing.T, f *r5UIFixture, snapshot company.PermissionEditorSnapshot) {
	t.Helper()
	var canSend bool
	var quota int
	var mode, revision string
	var zones []uuid.UUID
	r5UIMust(t, f.pool.QueryRow(context.Background(), `SELECT o.can_send,o.daily_send_quota,o.domain_access_mode,o.allowed_zone_ids,u.permission_revision::text FROM users u JOIN user_permission_overrides o ON o.user_id=u.id WHERE u.id=$1 AND u.tenant_id=$2`, f.employee.ID, f.tenant.ID).Scan(&canSend, &quota, &mode, &zones, &revision))
	if canSend || quota != 19 || mode != "list" || len(zones) != 1 || zones[0] != f.zone.ID || revision != snapshot.Revision.UserRevision {
		t.Fatal("persistent seed columns/revision differ from raw HTTP readback")
	}
}

func r5PermissionSeedStoredState(t *testing.T, f *r5UIFixture) string {
	t.Helper()
	var state string
	r5UIMust(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('user',(SELECT to_jsonb(u) FROM users u WHERE id=$1),'overrides',(SELECT to_jsonb(o) FROM user_permission_overrides o WHERE user_id=$1),'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM audit_log a))::text`, f.employee.ID).Scan(&state))
	return state
}

func TestR5PermissionSeedLegacy409AndCAS(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required; no skip")
	}
	f := r5UISeed(t)
	token := r5UIToken(t, f.admin)
	before := r5UIReadPermissionEditor(t, f, token)
	r5UISeedPermissionOverrides(t, f, token)
	seeded := r5UIReadPermissionEditor(t, f, token)
	r5PermissionSeedPersistent(t, f, seeded)
	stored := r5PermissionSeedStoredState(t, f)
	for _, method := range []string{"PUT", "DELETE"} {
		response := r5UICall(t, f, token, method, "/api/v1/admin/users/"+f.employee.ID.String()+"/permissions", map[string]any{"can_send": true}, 409)
		var envelope struct{ Error struct{ Code string } }
		r5UIMust(t, json.Unmarshal(response, &envelope))
		if envelope.Error.Code != "CONFLICT" {
			t.Fatal("legacy rejection lacks CONFLICT")
		}
		if r5PermissionSeedStoredState(t, f) != stored {
			t.Fatal("legacy command changed persisted state/audit")
		}
	}
	path := "/api/v1/admin/users/" + f.employee.ID.String() + "/permission-editor"
	r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": before.Revision, "patch": map[string]any{"can_send": true}}, 409)
	if r5PermissionSeedStoredState(t, f) != stored {
		t.Fatal("stale CAS changed persisted state/audit")
	}
	// A current empty patch succeeds without changing raw intent or persistent revision.
	r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": seeded.Revision, "patch": map[string]any{}}, 200)
	if r5PermissionSeedStoredState(t, f) != stored {
		t.Fatal("empty current CAS changed persisted state/audit")
	}
	r5PermissionSeedPersistent(t, f, r5UIReadPermissionEditor(t, f, token))
	// Restore inheritance and recreate the same raw values through supported HTTP.
	// Equal values must never make the old persistent observation current again.
	r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": seeded.Revision, "patch": map[string]any{"can_send": nil, "daily_send_quota": nil, "domain_access": nil}}, 200)
	cleared := r5UIReadPermissionEditor(t, f, token)
	if cleared.Overrides == nil || cleared.Overrides.CanSend != nil || cleared.Overrides.DailySendQuota != nil || cleared.Overrides.DomainAccess.Mode != "inherit" || cleared.Revision.Equal(seeded.Revision) {
		t.Fatal("inheritance reset did not preserve distinct raw intent/revision")
	}
	r5UISeedPermissionOverrides(t, f, token)
	recreated := r5UIReadPermissionEditor(t, f, token)
	r5PermissionSeedPersistent(t, f, recreated)
	if recreated.Revision.Equal(seeded.Revision) || recreated.Revision.Equal(cleared.Revision) {
		t.Fatal("persistent revision returned to old observation")
	}
	stored = r5PermissionSeedStoredState(t, f)
	r5UICall(t, f, token, "PATCH", path, map[string]any{"expected_revision": seeded.Revision, "patch": map[string]any{"can_send": true}}, 409)
	if r5PermissionSeedStoredState(t, f) != stored {
		t.Fatal("recreated-value stale CAS changed persisted state/audit")
	}
}
