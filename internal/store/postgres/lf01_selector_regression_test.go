package postgres_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// Dedicated regression producer, separate from the frozen formal catalog and
// shared component/Go fixtures. Missing opt-in inputs fail instead of skipping.
func TestLF01OwnedShippingSelector(t *testing.T) {
	if os.Getenv("TABMAIL_LF01_OWNED_PROBE") != "1" {
		t.Skip("opt-in dedicated owned PG/HTTP/UI probe")
	}
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN required")
	}
	for _, mode := range []string{"active", "frozen", "race", "target-race"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			if mode == "frozen" {
				_, err := f.pool.Exec(ctx, `UPDATE users SET is_active=false, session_version=session_version+1 WHERE id=$1`, f.employee.ID)
				must(t, err)
			}
			// Database guard proves the probe never reactivates the target.
			_, err := f.pool.Exec(ctx, `CREATE FUNCTION lf01_no_reactivation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.is_active=false AND NEW.is_active=true THEN RAISE EXCEPTION 'LF01 reactivation forbidden'; END IF; RETURN NEW; END $$; CREATE TRIGGER lf01_no_reactivation BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION lf01_no_reactivation()`)
			must(t, err)
			handler := companyRouter(t, f, testutil.NewMemoryObjectStore(), nil)
			if mode == "frozen" {
				r3HTTP(t, handler, r3Token(t, f.employee), "GET", "/api/v1/company/mailboxes", nil, 401)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			packet := map[string]any{"url": server.URL, "token": r3Token(t, f.admin), "user": f.admin, "target": f.employee.ID, "successor": f.other.ID, "mode": mode}
			raw, err := json.Marshal(packet)
			must(t, err)
			path := filepath.Join(t.TempDir(), "lf01.json")
			must(t, os.WriteFile(path, raw, 0600))
			childCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, "npm", "exec", "--", "vitest", "run", "--config", "vitest.lf01.config.ts")
			cmd.Dir = "../../../web"
			cmd.Env = append(os.Environ(), "TABMAIL_LF01_PROBE="+path)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("shipping panel probe: %v\n%s", err, output)
			}
			t.Logf("real shipping panel %s passed", mode)
			var active bool
			var completed int
			var owner uuid.UUID
			must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
			expectedOwner := f.other.ID
			if mode == "race" || mode == "target-race" {
				expectedOwner = f.employee.ID
			}
			if owner != expectedOwner {
				t.Fatal("mailbox ownership did not match disposition outcome")
			}
			must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM employee_offboarding_plans WHERE target_id=$1 AND state='executed'`, f.employee.ID).Scan(&completed))
			if mode == "race" || mode == "target-race" {
				if active != (mode == "race") || completed != 0 {
					t.Fatal("racing invalid successor changed target or completed disposition")
				}
			} else if active || completed != 1 {
				t.Fatal("target not frozen with exactly one completed disposition")
			}
		})
	}
}

func TestLF01OwnedHTTPNegativeBoundaries(t *testing.T) {
	if os.Getenv("TABMAIL_LF01_OWNED_PROBE") != "1" {
		t.Skip("opt-in dedicated owned PG/HTTP probe")
	}
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN required")
	}
	f := seedCompany(t)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), nil)
	token := r3Token(t, f.admin)
	path := func(id uuid.UUID) string { return "/api/v1/company/employees/" + id.String() + "/offboard/preview" }
	body := func(id uuid.UUID) any {
		return map[string]any{"successor_user_id": id, "reason": "Dedicated LF01 negative review", "options": company.OffboardingOptions{Drafts: "seal"}}
	}
	r3HTTP(t, h, token, "POST", path(f.employee.ID), body(f.employee.ID), 400)
	r3HTTP(t, h, token, "POST", path(f.admin.ID), body(f.other.ID), 400)
	foreignTenant := &models.Tenant{Name: "Foreign fixture", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(context.Background(), foreignTenant))
	foreign := &models.User{TenantID: foreignTenant.ID, Email: "foreign@fixture.test", DisplayName: "Foreign", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic"}
	must(t, f.st.CreateUser(context.Background(), foreign))
	r3HTTP(t, h, token, "POST", path(f.employee.ID), body(foreign.ID), 400)
	r3HTTP(t, h, token, "POST", path(foreign.ID), body(f.other.ID), 404)
	peer := &models.User{TenantID: f.tenant.ID, Email: "peer@fixture.test", DisplayName: "Peer", Role: models.RoleAdmin, IsActive: true, PasswordHash: "synthetic"}
	must(t, f.st.CreateUser(context.Background(), peer))
	r3HTTP(t, h, token, "POST", path(peer.ID), body(f.other.ID), 403)
	// Record the existing backend gap independently: same-tenant higher-role
	// successors are accepted by HTTP even though the panel excludes them.
	r3HTTP(t, h, token, "POST", path(f.employee.ID), body(peer.ID), 200)
	t.Log("BACKEND_GAP: admin caller can preview a higher-role successor; no backend widening in this change")
	_, err := f.pool.Exec(context.Background(), `UPDATE users SET is_active=false WHERE id=$1`, f.other.ID)
	must(t, err)
	r3HTTP(t, h, token, "POST", path(f.employee.ID), body(f.other.ID), 400)
}
