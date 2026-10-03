//go:build r5protocol

package postgres_test

// Shared-input adapters use the actual PostgreSQL repository and shipping
// router. Fixture writes construct the declared scenario; expected values are
// used only for assertions, never returned by a fake repository.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

type r5ProtocolCase struct {
	ID       string                     `json:"id"`
	Input    map[string]json.RawMessage `json:"input"`
	Expected struct {
		Status   int `json:"http_status"`
		Variants []struct {
			Name   string
			Status int
			Code   string
		} `json:"variants"`
		Wire struct {
			Code   string `json:"code"`
			Reason string `json:"reason_mode"`
		} `json:"wire_error"`
	} `json:"expected"`
}

func r5SharedCases(t *testing.T) []r5ProtocolCase {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable TABMAIL_TEST_DB_DSN required; no skip")
	}
	_, file, _, _ := runtime.Caller(0)
	raw, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
	must(t, e)
	var m struct {
		Cases []r5ProtocolCase `json:"cases"`
	}
	must(t, json.Unmarshal(raw, &m))
	return m.Cases
}
func r5Input[T any](t *testing.T, c r5ProtocolCase, key string) T {
	t.Helper()
	var v T
	if raw, ok := c.Input[key]; ok {
		must(t, json.Unmarshal(raw, &v))
	}
	return v
}
func r5SharedRouter(t *testing.T, f *companyFixture) http.Handler {
	obj := testutil.NewMemoryObjectStore()
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
	svc.SetObjectStore(obj) // No worker starts; no outbound network traffic.
	return companyRouter(t, f, obj, svc)
}
func r5Wire(t *testing.T, h http.Handler, token, method, path string, body any, c r5ProtocolCase) *httptest.ResponseRecorder {
	t.Helper()
	raw, e := json.Marshal(body)
	must(t, e)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != c.Expected.Status {
		if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_RT03" && w.Code == 200 {
			t.Errorf("R5_PROTOCOL_TARGET_RT03: restore accepted an expired item; target is 409 and no mutation")
		} else if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_LF01" && w.Code == 409 && strings.Contains(w.Body.String(), "employee is already inactive") {
			t.Errorf("R5_PROTOCOL_TARGET_LF01: frozen employee rejected before disposition")
		} else if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_LF06" && w.Code == 200 {
			t.Errorf("R5_PROTOCOL_TARGET_LF06: old executed plan replayed after reactivation")
		} else {
			t.Fatalf("protocol status: want %d got %d: %s", c.Expected.Status, w.Code, w.Body.String())
		}
	}
	if w.Code >= 400 {
		var env struct {
			Error map[string]json.RawMessage `json:"error"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &env))
		var code, msg string
		must(t, json.Unmarshal(env.Error["code"], &code))
		must(t, json.Unmarshal(env.Error["message"], &msg))
		wantCode := c.Expected.Wire.Code
		if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_LF01" && w.Code == 409 {
			wantCode = "CONFLICT"
		}
		if code != wantCode || msg == "" {
			t.Fatalf("wire code/message: %q %q", code, msg)
		}
		if c.Expected.Wire.Reason == "absent" {
			if _, ok := env.Error["reason"]; ok {
				t.Fatal("reason must be absent for this entrypoint")
			}
		}
		if strings.Contains(w.Body.String(), "PRIVATE_PROTOCOL_BODY") {
			t.Fatal("denied body leaked")
		}
	}
	return w
}
func TestR5ProtocolContentSharedCases(t *testing.T) {
	cases := r5SharedCases(t)
	executed := 0
	for _, c := range cases {
		if r5Input[string](t, c, "protocol_consumer") != "sent_content" {
			continue
		}
		executed++
		variants := []string{"default"}
		for _, key := range []string{"hard_expiry_relative_to_now", "purge_after_relative_to_now"} {
			if _, ok := c.Input[key]; ok {
				variants = r5Input[[]string](t, c, key)
			}
		}
		if raw, ok := c.Input["principal"]; ok && len(raw) > 0 && raw[0] == '[' {
			variants = r5Input[[]string](t, c, "principal")
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				h := r5SharedRouter(t, f)
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
				attachment, err := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.shared.ID, Filename: "protocol.txt", Size: 3})
				must(t, err)
				must(t, f.st.FinishMailAttachment(ctx, f.u, attachment.ID, company.Hash("abc")))
				j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.shared.ID, MailFrom: f.shared.FullAddress, To: []string{"client@fixture.test"}, RcptTo: []string{"client@fixture.test"}, AttachmentIDs: []uuid.UUID{attachment.ID}, Subject: "protocol", TextBody: "PRIVATE_PROTOCOL_BODY", State: models.OutboundSent}
				must(t, f.st.CreateOutboundJob(ctx, j))
				actor := f.u
				token := r3Token(t, f.employee)
				if raw, ok := c.Input["can_read"]; ok && string(raw) == "false" {
					must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: true}))
					if variant == "company_admin" || variant == "platform_super_admin" {
						actor = f.a
						if variant == "platform_super_admin" {
							_, e := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
							must(t, e)
							f.admin.Role = models.RoleSuperAdmin
							actor.Role = models.RoleSuperAdmin
						}
						token = r3Token(t, f.admin)
					}
				}
				if r5Input[bool](t, c, "address_reused") {
					// Live archive FKs deliberately prohibit physical deletion. Retain the
					// old identity in a retired namespace, revoke its grant, then
					// reassign the original address to a genuinely different ID.
					_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET local_part='retired_support',full_address='retired_support@company.test' WHERE id=$1`, f.shared.ID)
					must(t, e)
					must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}))
					replacement, e := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "support", Kind: "shared"})
					must(t, e)
					if replacement.ID == f.shared.ID || replacement.FullAddress != j.MailFrom {
						t.Fatal("address reassignment fixture mismatch")
					}
					must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: replacement.ID, UserID: f.employee.ID, CanRead: true}))
				}
				if raw, ok := c.Input["tenant_matches"]; ok && string(raw) == "false" {
					foreign := &models.Tenant{Name: "Protocol foreign", PlanID: f.tenant.PlanID}
					must(t, f.st.CreateTenant(ctx, foreign))
					u := &models.User{TenantID: foreign.ID, Email: "foreign@fixture.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
					must(t, f.st.CreateUser(ctx, u))
					actor = f.u
					actor.ID = u.ID
					actor.TenantID = foreign.ID
					token = r3Token(t, u)
				}
				for key, column := range map[string]string{"hard_expiry_relative_to_now": "expires_at", "purge_after_relative_to_now": "purge_after"} {
					if _, ok := c.Input[key]; ok {
						_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET `+column+`=clock_timestamp()-CASE WHEN $3 THEN interval '1 second' ELSE interval '0 seconds' END,deleted_at=CASE WHEN $2 THEN clock_timestamp() ELSE deleted_at END WHERE asset_id=$1`, j.ID, column == "purge_after", variant == "before")
						must(t, e)
					}
				}
				if r5Input[string](t, c, "authoritative_query") == "error" {
					_, e := f.pool.Exec(ctx, `DROP TABLE sent_mail_items CASCADE`)
					must(t, e)
				}
				content, e := f.st.GetSubmissionContent(ctx, actor, j.ID)
				if c.Expected.Status == 200 {
					must(t, e)
					if content == nil || content.TextBody != j.TextBody {
						t.Fatal("live body mismatch")
					}
				} else {
					if e == nil || content != nil {
						t.Fatal("repository exposed denied content")
					}
					if c.Expected.Status < 500 {
						v, ok := app.As(e)
						if !ok || v.Kind != app.KindNotFound {
							t.Fatalf("repository error mismatch: %v", e)
						}
					}
				}
				r5Wire(t, h, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, c)
				meta, e := f.st.GetSubmissionAttachment(ctx, actor, j.ID, attachment.ID)
				if c.Expected.Status == 200 {
					must(t, e)
					if meta.ID != attachment.ID {
						t.Fatal("attachment scope mismatch")
					}
				} else if e == nil || meta != nil {
					t.Fatal("attachment bypassed same content boundary")
				}
				r5Wire(t, h, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/attachments", nil, c)
			})
		}
	}
	if executed != 8 {
		t.Fatalf("shared content coverage drift: %d", executed)
	}
}

func TestR5ProtocolCredentialSharedCases(t *testing.T) {
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "content_credentials" {
			continue
		}
		for _, state := range r5Input[[]string](t, c, "credential_state") {
			t.Run(c.ID+"/"+state, func(t *testing.T) {
				f := seedCompany(t)
				h := r5SharedRouter(t, f)
				j := r5LegacyJob(t, f)
				token := r3Token(t, f.employee)
				switch state {
				case "anonymous":
					token = ""
				case "revoked":
					_, e := f.pool.Exec(context.Background(), `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.employee.ID)
					must(t, e)
				case "frozen":
					_, e := f.pool.Exec(context.Background(), `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
					must(t, e)
				default:
					t.Fatal("unknown credential input")
				}
				r5Wire(t, h, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, c)
			})
		}
	}
}

func TestR5ProtocolRetentionSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "inbound_retention" {
			continue
		}
		executed++
		variants := []string{"default"}
		if _, ok := c.Input["hard_expiry_or_purge_after_relative_to_now"]; ok {
			variants = nil
			for _, cutoff := range r5Input[[]string](t, c, "hard_expiry_or_purge_after_relative_to_now") {
				for _, axis := range []string{"expires_at", "purge_after"} {
					variants = append(variants, axis+"/"+cutoff)
				}
			}
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				h := r5SharedRouter(t, f)
				mb := f.shared
				if r5Input[string](t, c, "mailbox_kind") == "personal" {
					mb = f.personal
				}
				read := true
				if raw, ok := c.Input["can_read"]; ok && string(raw) == "false" {
					read = false
				}
				organize := true
				if raw, ok := c.Input["can_organize"]; ok && string(raw) == "false" {
					organize = false
				}
				if _, ok := c.Input["rejected_grant_configuration"]; ok {
					e := grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanOrganize: true})
					v, ok := app.As(e)
					if !ok || v.Kind != app.KindBadRequest {
						t.Fatalf("organize-only configuration must be rejected: %v", e)
					}
					snapshot, e := f.st.GetWorkMailbox(ctx, f.a, mb.ID)
					must(t, e)
					grantCase := c
					grantCase.Expected.Status = 400
					grantCase.Expected.Wire.Code = "BAD_REQUEST"
					r5Wire(t, h, r3Token(t, f.admin), "PUT", "/api/v1/company/mailboxes/"+mb.ID.String()+"/grants", map[string]any{"user_id": f.employee.ID, "can_read": false, "can_organize": true, "revision": snapshot.Revision}, grantCase)
				}
				if mb.OwnerUserID == nil || *mb.OwnerUserID != f.employee.ID {
					must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanRead: read, CanOrganize: organize}))
				}
				m := &models.Message{TenantID: f.tenant.ID, MailboxID: mb.ID, ZoneID: f.zone.ID, Sender: "client@fixture.test", Recipients: []string{mb.FullAddress}, Subject: "retention", RawObjectKey: "test/raw"}
				must(t, f.st.CreateMessage(ctx, m))
				finite := r5Input[string](t, c, "hard_expiry") == "finite_future" || r5Input[string](t, c, "purge_after") == "existing_future"
				if finite {
					_, e := f.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp()+interval '7 days' WHERE id=$1`, m.ID)
					must(t, e)
				} else {
					_, e := f.pool.Exec(ctx, `UPDATE messages SET expires_at=NULL WHERE id=$1`, m.ID)
					must(t, e)
				}
				var actions []string
				raw := c.Input["operation"]
				if len(raw) > 0 && raw[0] == '[' {
					must(t, json.Unmarshal(raw, &actions))
				} else {
					var op string
					must(t, json.Unmarshal(raw, &op))
					if op == "restore" {
						actions = []string{"restore"}
					} else {
						actions = []string{"trash"}
					}
				}
				if actions[0] == "restore" {
					_, e := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '1 day' WHERE id=$1`, m.ID)
					must(t, e)
				}
				if _, ok := c.Input["hard_expiry_or_purge_after_relative_to_now"]; ok {
					_, e := f.pool.Exec(ctx, `UPDATE messages SET `+strings.Split(variant, "/")[0]+`=clock_timestamp()-CASE WHEN $2 THEN interval '1 second' ELSE interval '0 seconds' END,deleted_at=clock_timestamp() WHERE id=$1`, m.ID, strings.HasSuffix(variant, "/before"))
					must(t, e)
				}
				var beforeExpiry, beforePurge *time.Time
				must(t, f.pool.QueryRow(ctx, `SELECT expires_at,purge_after FROM messages WHERE id=$1`, m.ID).Scan(&beforeExpiry, &beforePurge))
				path := "/api/v1/company/mailboxes/" + mb.ID.String() + "/messages/" + m.ID.String() + "/actions"
				for _, action := range actions {
					// Each observed command comes from the real router. We do not replace
					// its result with the target, even for the known A05 restore defect.
					r5Wire(t, h, r3Token(t, f.employee), "POST", path, map[string]string{"action": action}, c)
					var expiry, purge *time.Time
					must(t, f.pool.QueryRow(ctx, `SELECT expires_at,purge_after FROM messages WHERE id=$1`, m.ID).Scan(&expiry, &purge))
					if !reflect.DeepEqual(beforeExpiry, expiry) {
						if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_RT01" {
							t.Errorf("R5_PROTOCOL_TARGET_RT01: %s changed immutable hard expiry", action)
						} else if r5Input[string](t, c, "target_failure_marker") == "R5_PROTOCOL_TARGET_RT03" {
							t.Errorf("R5_PROTOCOL_TARGET_RT03: expired restore changed immutable hard expiry")
						} else {
							t.Error("hard expiry changed")
						}
					}
					if action == "trash" && beforePurge != nil && !reflect.DeepEqual(beforePurge, purge) {
						t.Error("repeat trash extended purge deadline")
					}
					beforePurge = purge
				}
			})
		}
	}
	if executed != 6 {
		t.Fatalf("retention coverage drift: %d", executed)
	}
}

func TestR5ProtocolRecoverySharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "outbound_recovery" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			j := r5LegacyJob(t, f)
			reason := "Synthetic required recovery audit"
			path := "/api/v1/company/outbound/" + j.ID.String() + "/inspect"
			if raw, ok := c.Input["principal"]; ok && len(raw) > 0 && raw[0] == '[' {
				for _, role := range r5Input[[]string](t, c, "principal") {
					t.Run(role, func(t *testing.T) {
						u := f.employee
						if role == "company_admin" {
							u = f.admin
						}
						r5Wire(t, h, r3Token(t, u), "POST", path, map[string]string{"reason": reason}, c)
					})
				}
				return
			}
			_, e := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
			must(t, e)
			f.admin.Role = models.RoleSuperAdmin
			if r5Input[string](t, c, "required_audit") == "error" {
				_, e = f.pool.Exec(ctx, `CREATE FUNCTION r5_protocol_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='outbound.break_glass' THEN RAISE EXCEPTION 'isolated protocol audit fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER r5_protocol_audit_fail BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION r5_protocol_audit_fail()`)
				must(t, e)
			}
			variants := r5Input[[]struct {
				Name, Value, Repeat, Prefix, Suffix string
				Count                               int
			}](t, c, "variants")
			if len(variants) == 0 {
				w := r5Wire(t, h, r3Token(t, f.admin), "POST", path, map[string]string{"reason": reason}, c)
				if c.Expected.Status == 200 {
					if strings.Contains(w.Body.String(), "delivery_token") {
						t.Fatal("recovery leaked execution secret")
					}
					if !strings.Contains(w.Body.String(), "PRIVATE_LEGACY_BODY") {
						t.Fatal("controlled recovery missing body")
					}
					var count int
					must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.break_glass' AND resource_id=$1`, j.ID).Scan(&count))
					if count != 1 {
						t.Fatal("required audit missing")
					}
				}
				return
			}
			var expected struct {
				Variants []struct {
					Name       string
					Valid      bool
					Normalized string
				} `json:"variants"`
			}
			// Expected is assertion data only. HTTP still executes the shipping handler.
			_, file, _, _ := runtime.Caller(0)
			raw, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
			must(t, e)
			var doc struct {
				Cases []struct {
					ID       string
					Expected json.RawMessage
				}
			}
			must(t, json.Unmarshal(raw, &doc))
			for _, row := range doc.Cases {
				if row.ID == c.ID {
					must(t, json.Unmarshal(row.Expected, &expected))
				}
			}
			for _, v := range variants {
				t.Run(v.Name, func(t *testing.T) {
					value := v.Value
					if v.Repeat != "" {
						value = v.Prefix + strings.Repeat(v.Repeat, v.Count) + v.Suffix
					}
					valid := false
					normalized := ""
					found := false
					for _, want := range expected.Variants {
						if want.Name == v.Name {
							valid = want.Valid
							normalized = want.Normalized
							found = true
						}
					}
					if !found {
						t.Fatal("missing expected variant")
					}
					want := c
					if valid {
						want.Expected.Status = 200
						want.Expected.Wire.Code = ""
					}
					var before int
					must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.break_glass' AND resource_id=$1`, j.ID).Scan(&before))
					r5Wire(t, h, r3Token(t, f.admin), "POST", path, map[string]string{"reason": value}, want)
					var after int
					must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='outbound.break_glass' AND resource_id=$1`, j.ID).Scan(&after))
					if valid {
						if after != before+1 {
							t.Fatal("valid reason not audited")
						}
						var stored string
						must(t, f.pool.QueryRow(ctx, `SELECT details->>'reason' FROM audit_log WHERE action='outbound.break_glass' AND resource_id=$1 ORDER BY created_at DESC LIMIT 1`, j.ID).Scan(&stored))
						if stored != normalized {
							t.Fatal("audit normalization mismatch")
						}
					} else if after != before {
						t.Fatal("invalid reason executed recovery")
					}
				})
			}
		})
	}
	if executed != 4 {
		t.Fatalf("recovery coverage drift: %d", executed)
	}
}

func TestR5ProtocolDomainSharedCases(t *testing.T) {
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "inbound_domain" {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: r5Input[bool](t, c, "has_grant")}))
			m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "client@fixture.test", Recipients: []string{f.shared.FullAddress}, Subject: "domain", RawObjectKey: "test/raw"}
			must(t, f.st.CreateMessage(ctx, m))
			p := r5SnapshotProfile(t, f, true, false)
			p.AllowedZoneIDs = []uuid.UUID{uuid.New()}
			must(t, f.st.UpdatePermissionProfile(ctx, p))
			content, e := f.st.GetWorkMessage(ctx, f.u, f.shared.ID, m.ID)
			v, ok := app.As(e)
			if content != nil || !ok || v.Kind != app.KindForbidden {
				t.Fatalf("domain repository denial mismatch: %v", e)
			}
			r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/mailboxes/"+f.shared.ID.String()+"/messages/"+m.ID.String(), nil, c)
		})
	}
}

func TestR5ProtocolReceiptSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "safe_receipt" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			state := r5Input[models.OutboundState](t, c, "job_state")
			if state == "" {
				state = models.OutboundSent
			}
			j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, State: state, Subject: r5Input[string](t, c, "subject"), TextBody: r5Input[string](t, c, "text_body"), SMTPResponse: r5Input[string](t, c, "smtp_response"), InFlightDomain: r5Input[string](t, c, "in_flight_domain")}
			recipients := r5Input[[]struct{ Category, Address, State string }](t, c, "recipients")
			if len(recipients) == 0 {
				recipients = []struct{ Category, Address, State string }{{"to", "visible@fixture.test", "accepted"}}
			}
			for _, r := range recipients {
				j.RcptTo = append(j.RcptTo, r.Address)
				switch r.Category {
				case "to":
					j.To = append(j.To, r.Address)
				case "cc":
					j.CC = append(j.CC, r.Address)
				case "bcc":
					j.BCC = append(j.BCC, r.Address)
				default:
					t.Fatal("unknown recipient category")
				}
			}
			must(t, f.st.CreateOutboundJob(ctx, j))
			for _, r := range recipients {
				_, e := f.pool.Exec(ctx, `UPDATE outbound_recipients SET state=$3 WHERE job_id=$1 AND address=$2`, j.ID, r.Address, r.State)
				must(t, e)
			}
			if r5Input[bool](t, c, "content_expired") {
				_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
				must(t, e)
			} else if raw, ok := c.Input["content_allowed"]; ok && string(raw) == "false" {
				_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
				must(t, e)
			}
			receipt, e := f.st.GetSubmission(ctx, f.u, j.ID)
			must(t, e)
			w := r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+j.ID.String(), nil, c)
			var env struct{ Data company.Submission }
			must(t, json.Unmarshal(w.Body.Bytes(), &env))
			if env.Data.Capabilities == nil || env.Data.Capabilities.ViewContent {
				t.Fatal("expired receipt advertised body access")
			}
			if env.Data.Status != receipt.Status {
				t.Fatal("HTTP/DB receipt status contradiction")
			}
			var projection struct {
				Status            string
				Retry             bool
				DeliveryUncertain bool     `json:"delivery_uncertain"`
				Forbidden         []string `json:"forbidden_fields"`
				Hidden            []string `json:"hidden_addresses"`
			}
			// Read assertion projection from the same shared case file.
			_, file, _, _ := runtime.Caller(0)
			raw, e := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../docs/company-mail/evidence/R5-PROTOCOL-CASES.json"))
			must(t, e)
			var doc struct {
				Cases []struct {
					ID       string
					Expected struct{ Projection json.RawMessage }
				}
			}
			must(t, json.Unmarshal(raw, &doc))
			for _, row := range doc.Cases {
				if row.ID == c.ID && len(row.Expected.Projection) > 0 {
					must(t, json.Unmarshal(row.Expected.Projection, &projection))
				}
			}
			if projection.Status != "" {
				if receipt.Status != projection.Status || env.Data.DeliveryUncertain != projection.DeliveryUncertain || env.Data.Capabilities.Retry != projection.Retry {
					t.Fatalf("receipt status/capability mismatch: %+v", env.Data)
				}
				for _, address := range projection.Hidden {
					if strings.Contains(w.Body.String(), address) {
						t.Errorf("R5_PROTOCOL_TARGET_RC03: ordinary receipt disclosed BCC address")
					}
				}
				for _, field := range projection.Forbidden {
					if strings.Contains(w.Body.String(), `"`+field+`"`) {
						t.Fatalf("receipt exposed private field %s", field)
					}
				}
			}
		})
	}
	if executed != 4 {
		t.Fatalf("receipt coverage drift: %d", executed)
	}
}

func TestR5ProtocolOffboardingSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "offboarding" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "private departure draft"}})
			must(t, e)
			frozen := r5Input[string](t, c, "target_state") == "frozen"
			if frozen {
				_, e = f.pool.Exec(ctx, `UPDATE users SET is_active=false,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
				must(t, e)
			}
			base := "/api/v1/company/employees/" + f.employee.ID.String() + "/offboard/"
			setup := c
			setup.Expected.Status = 200
			setup.Expected.Wire.Code = ""
			previewCase := setup
			if frozen {
				previewCase = c
			}
			preview := r5Wire(t, h, r3Token(t, f.admin), "POST", base+"preview", map[string]any{"successor_user_id": f.other.ID, "options": map[string]string{"drafts": "seal"}, "reason": "Synthetic authorized departure disposition"}, previewCase)
			if preview.Code != 200 {
				var active bool
				must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
				if active {
					t.Fatal("frozen preview reactivated target")
				}
				return
			}
			plan := r3Data[company.OffboardingPlan](t, preview)
			if plan.ID == uuid.Nil {
				t.Fatal("actual preview omitted plan id")
			}
			var another *company.OffboardingPlan
			if r5Input[bool](t, c, "another_plan_completed_same_lifecycle") {
				p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Second preview for same lifecycle")
				must(t, e)
				another = p
			}
			if r5Input[bool](t, c, "asset_id_revision_or_eligibility_changed") {
				draft.Payload.Subject = "Changed same-count asset revision"
				_, e = f.st.SaveMailDraft(ctx, f.u, *draft)
				must(t, e)
			}
			if r5Input[string](t, c, "audit_or_mid_write") == "error" {
				_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_protocol_offboard_audit_fail CHECK(action<>'employee.offboard')`)
				must(t, e)
			}
			executeCase := c
			if r5Input[bool](t, c, "same_plan_replay") || r5Input[bool](t, c, "another_plan_completed_same_lifecycle") || r5Input[bool](t, c, "new_employment_lifecycle") {
				executeCase = setup
			}
			first := r5Wire(t, h, r3Token(t, f.admin), "POST", strings.TrimSuffix(base, "/"), map[string]any{"plan_id": plan.ID}, executeCase)
			var active, sealed bool
			var version int64
			var owner uuid.UUID
			var audits int
			var state string
			must(t, f.pool.QueryRow(ctx, `SELECT is_active,session_version FROM users WHERE id=$1`, f.employee.ID).Scan(&active, &version))
			must(t, f.pool.QueryRow(ctx, `SELECT sealed_at IS NOT NULL,user_id FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&sealed, &owner))
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='employee.offboard' AND resource_id=$1`, f.employee.ID).Scan(&audits))
			must(t, f.pool.QueryRow(ctx, `SELECT state FROM employee_offboarding_plans WHERE id=$1`, plan.ID).Scan(&state))
			if executeCase.Expected.Status == 200 {
				if active || !sealed || owner != f.employee.ID || state != "executed" || audits != 1 {
					t.Fatal("successful disposition invariant mismatch")
				}
			} else if executeCase.Expected.Status >= 400 {
				if !active || sealed || owner != f.employee.ID || state != "preview" || audits != 0 {
					t.Fatal("failed disposition was not atomically rolled back")
				}
			}
			if r5Input[bool](t, c, "same_plan_replay") {
				again := r5Wire(t, h, r3Token(t, f.admin), "POST", strings.TrimSuffix(base, "/"), map[string]any{"plan_id": plan.ID}, c)
				a := r3Data[company.OffboardingPlan](t, first)
				b := r3Data[company.OffboardingPlan](t, again)
				if a.ExecutedAt == nil || b.ExecutedAt == nil || !a.ExecutedAt.Equal(*b.ExecutedAt) {
					t.Fatal("replay changed persistent receipt")
				}
				var nowVersion int64
				var nowAudits int
				must(t, f.pool.QueryRow(ctx, `SELECT session_version FROM users WHERE id=$1`, f.employee.ID).Scan(&nowVersion))
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='employee.offboard' AND resource_id=$1`, f.employee.ID).Scan(&nowAudits))
				if nowVersion != version || nowAudits != audits {
					t.Fatal("replay repeated revocation/audit")
				}
			}
			if another != nil {
				r5Wire(t, h, r3Token(t, f.admin), "POST", strings.TrimSuffix(base, "/"), map[string]any{"plan_id": another.ID}, c)
				var s string
				must(t, f.pool.QueryRow(ctx, `SELECT state FROM employee_offboarding_plans WHERE id=$1`, another.ID).Scan(&s))
				if s != "preview" {
					t.Fatal("second plan executed")
				}
			}
			if r5Input[bool](t, c, "new_employment_lifecycle") {
				// Reactivation is through the actual administrative writer, not an
				// expected-response transport stub; it must not revive old credentials.
				r3HTTP(t, h, r3Token(t, f.admin), "PATCH", "/api/v1/admin/users/"+f.employee.ID.String(), map[string]bool{"is_active": true}, 200)
				r5Wire(t, h, r3Token(t, f.admin), "POST", strings.TrimSuffix(base, "/"), map[string]any{"plan_id": plan.ID}, c)
				must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
				if !active {
					t.Fatal("old plan disabled new lifecycle")
				}
			}
		})
	}
	if executed != 6 {
		t.Fatalf("offboarding coverage drift: %d", executed)
	}
}

// PE-only transport helpers adapt shared legacy field intent to the shipping
// editor protocol. They do not change shared inputs, expected values or policy.
func r5PEEditorResponse(t *testing.T, w *httptest.ResponseRecorder, user uuid.UUID) company.PermissionEditorSnapshot {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("formal PE setup status: want 200 got %d: %s", w.Code, w.Body.String())
	}
	snapshot := r3Data[company.PermissionEditorSnapshot](t, w)
	if snapshot.UserID != user || snapshot.Effective == nil {
		t.Fatal("formal PE editor returned incorrect subject or absent effective permission")
	}
	must(t, snapshot.Revision.Validate())
	return snapshot
}

func r5PEEditorSnapshot(t *testing.T, h http.Handler, token string, user uuid.UUID) company.PermissionEditorSnapshot {
	t.Helper()
	return r5PEEditorResponse(t, r5Observed(t, h, token, "GET", "/api/v1/admin/users/"+user.String()+"/permission-editor", nil), user)
}

func r5PEProfileSnapshot(t *testing.T, h http.Handler, token string, id uuid.UUID) models.PermissionProfile {
	t.Helper()
	w := r5Observed(t, h, token, "GET", "/api/v1/admin/permissions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("formal PE profile listing: want 200 got %d: %s", w.Code, w.Body.String())
	}
	for _, profile := range r3Data[[]models.PermissionProfile](t, w) {
		if profile.ID == id {
			if profile.Revision == "" {
				t.Fatal("formal PE profile has no CAS revision")
			}
			return profile
		}
	}
	t.Fatal("formal PE profile not visible in administrator listing")
	return models.PermissionProfile{}
}

func r5PEWirePatch(t *testing.T, original map[string]any) map[string]any {
	t.Helper()
	wire := map[string]any{}
	for field, value := range original {
		if field != "allowed_zone_ids" {
			wire[field] = value
			continue
		}
		if value == nil {
			wire["domain_access"] = nil
			continue
		}
		raw, err := json.Marshal(value)
		must(t, err)
		var zones []uuid.UUID
		must(t, json.Unmarshal(raw, &zones))
		mode := "list"
		if len(zones) == 0 {
			// The declared legacy [] intent is explicit all, not inherit/none.
			mode, zones = "all", []uuid.UUID{}
		}
		wire["domain_access"] = map[string]any{"mode": mode, "zone_ids": zones}
	}
	return wire
}

func r5PEAssignEditor(t *testing.T, h http.Handler, token string, user uuid.UUID, expected company.PermissionRevision, selected *models.PermissionProfile, patch map[string]any) company.PermissionEditorSnapshot {
	t.Helper()
	body := map[string]any{"expected_revision": expected, "profile_id": nil, "profile_revision": nil, "patch": patch}
	if selected != nil {
		body["profile_id"], body["profile_revision"] = selected.ID, selected.Revision
	}
	return r5PEEditorResponse(t, r5Observed(t, h, token, "POST", "/api/v1/admin/users/"+user.String()+"/permission-editor/assignment", body), user)
}

func r5PEUserRevisionAdvanced(t *testing.T, before, after company.PermissionRevision) {
	t.Helper()
	must(t, before.Validate())
	must(t, after.Validate())
	// Validated positive canonical decimal strings avoid float precision loss.
	advanced := len(after.UserRevision) > len(before.UserRevision) || len(after.UserRevision) == len(before.UserRevision) && after.UserRevision > before.UserRevision
	if before.UserID != after.UserID || before.TenantID != after.TenantID || !advanced {
		t.Fatal("formal PE assignment did not advance its stable subject revision")
	}
}

func TestR5ProtocolPermissionIntentSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "permission_intent" {
			continue
		}
		executed++
		variants := []string{"quota_patch"}
		if _, ok := c.Input["field_values"]; ok {
			variants = nil
			for _, raw := range r5Input[[]json.RawMessage](t, c, "field_values") {
				var name string
				if string(raw) == `"omitted"` {
					name = "omitted"
				} else {
					name = string(raw)
				}
				variants = append(variants, name)
			}
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				h := r5SharedRouter(t, f)
				path := "/api/v1/admin/users/" + f.employee.ID.String() + "/permission-editor"
				setup := c
				setup.Expected.Status = 200
				setup.Expected.Wire.Code = ""
				seed := map[string]any{"can_send": false, "allowed_zone_ids": []uuid.UUID{f.zone.ID}, "daily_send_quota": 19}
				token := r3Token(t, f.admin)
				observed := r5PEEditorSnapshot(t, h, token, f.employee.ID)
				seededWire := r5Wire(t, h, token, "PATCH", path, map[string]any{"expected_revision": observed.Revision, "patch": r5PEWirePatch(t, seed)}, setup)
				seeded := r5PEEditorResponse(t, seededWire, f.employee.ID)
				var beforeSend *bool
				var beforeZones []uuid.UUID
				must(t, f.pool.QueryRow(ctx, `SELECT can_send,allowed_zone_ids FROM user_permission_overrides WHERE user_id=$1`, f.employee.ID).Scan(&beforeSend, &beforeZones))
				if beforeSend == nil || *beforeSend || len(beforeZones) != 1 {
					t.Fatal("permission restriction fixture failed")
				}
				patch := map[string]any{}
				if variant == "quota_patch" {
					must(t, json.Unmarshal(c.Input["patch"], &patch))
				} else {
					switch variant {
					case "omitted":
						patch["daily_send_quota"] = 25
					case "null":
						patch["can_send"] = nil
					case "false":
						patch["can_send"] = false
					case "0":
						patch["daily_send_quota"] = 0
					case "[]":
						patch["allowed_zone_ids"] = []uuid.UUID{}
					default:
						t.Fatal("unknown field intent")
					}
				}
				current := r5PEEditorSnapshot(t, h, token, f.employee.ID)
				if !current.Revision.Equal(seeded.Revision) {
					t.Fatal("formal PE setup changed before the shared field-intent request")
				}
				r5Wire(t, h, token, "PATCH", path, map[string]any{"expected_revision": current.Revision, "patch": r5PEWirePatch(t, patch)}, setup)
				var afterSend *bool
				var afterZones []uuid.UUID
				var quota *int
				must(t, f.pool.QueryRow(ctx, `SELECT can_send,allowed_zone_ids,daily_send_quota FROM user_permission_overrides WHERE user_id=$1`, f.employee.ID).Scan(&afterSend, &afterZones, &quota))
				marker := r5Input[string](t, c, "target_failure_marker")
				if _, changed := patch["can_send"]; !changed && !reflect.DeepEqual(beforeSend, afterSend) {
					t.Errorf("%s: omitted can_send erased original restriction", marker)
				}
				if _, changed := patch["allowed_zone_ids"]; !changed && !reflect.DeepEqual(beforeZones, afterZones) {
					t.Errorf("%s: omitted allowed_zone_ids erased original restriction", marker)
				}
				switch variant {
				case "null":
					if afterSend != nil {
						t.Fatal("explicit null did not restore inheritance")
					}
				case "false":
					if afterSend == nil || *afterSend {
						t.Fatal("explicit false lost")
					}
				case "0":
					if quota == nil || *quota != 0 {
						t.Fatal("explicit zero lost")
					}
				case "[]":
					if afterZones == nil || len(afterZones) != 0 {
						t.Fatal("explicit empty domain set collapsed to inheritance")
					}
					snapshot := r5PEEditorSnapshot(t, h, token, f.employee.ID)
					if snapshot.Overrides == nil || snapshot.Overrides.DomainAccess.Mode != "all" || snapshot.FieldSources["domain_access"] != "override" {
						t.Fatal("explicit legacy [] domain intent lost all/override provenance")
					}
				}
			})
		}
	}
	if executed != 2 {
		t.Fatalf("permission intent coverage drift: %d", executed)
	}
}

func TestR5ProtocolBCCAssetSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "bcc_asset" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			mb := f.personal
			if r5Input[string](t, c, "mailbox_kind") == "shared" {
				mb = f.shared
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: mb.ID, UserID: f.employee.ID, CanRead: r5Input[bool](t, c, "can_read"), CanSend: true}))
			}
			att, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: mb.ID, Filename: "bcc-proof.txt", Size: 3})
			must(t, e)
			must(t, f.st.FinishMailAttachment(ctx, f.u, att.ID, company.Hash("abc")))
			categories := r5Input[[]string](t, c, "recipient_categories")
			if len(categories) == 0 {
				categories = []string{"to", "cc", "bcc"}
			}
			payload := company.DraftPayload{Subject: "durable BCC protocol", TextBody: "PRIVATE_PROTOCOL_BODY", AttachmentIDs: []uuid.UUID{att.ID}}
			for _, category := range categories {
				switch category {
				case "to":
					payload.To = append(payload.To, "visible@recipient.test")
				case "cc":
					payload.CC = append(payload.CC, "copy@recipient.test")
				case "bcc":
					payload.BCC = append(payload.BCC, "hidden@recipient.test")
				default:
					t.Fatal("unknown structured recipient category")
				}
			}
			draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: mb.ID, Payload: payload})
			must(t, e)
			created := draftSubmitHTTP(t, h, r3Token(t, f.employee), draft.ID.String(), "bcc-protocol-command", draft.Revision)
			if created.Code != 201 {
				t.Fatalf("actual submit failed: %d %s", created.Code, created.Body.String())
			}
			id := r3Data[struct{ ID uuid.UUID }](t, created).ID
			if id == uuid.Nil {
				t.Fatal("submit id missing")
			}
			j, e := f.st.GetOutboundJob(ctx, id)
			must(t, e)
			if j == nil || !reflect.DeepEqual(j.BCC, payload.BCC) {
				t.Fatal("structured submit dropped BCC before archive")
			}
			wire, e := outbound.Build(outbound.Message{From: j.MailFrom, To: j.To, CC: j.CC, BCC: j.BCC, Subject: j.Subject, TextBody: j.TextBody, Attachments: []outbound.Attachment{{Filename: "bcc-proof.txt", ContentType: "text/plain", Data: []byte("abc")}}})
			must(t, e)
			if strings.Contains(strings.ToLower(string(wire)), "bcc:") || strings.Contains(string(wire), "hidden@recipient.test") {
				t.Fatal("wire MIME leaked BCC")
			}
			if r5Input[bool](t, c, "delete_job_and_ledger") {
				_, e = f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, id)
				must(t, e)
				var count int
				must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=$1)+(SELECT count(*) FROM outbound_recipients WHERE job_id=$1)`, id).Scan(&count))
				if count != 0 {
					t.Fatal("queue cleanup did not remove job and ledger")
				}
			}
			content, e := f.st.GetSubmissionContent(ctx, f.u, id)
			must(t, e)
			if content.TextBody != payload.TextBody || !reflect.DeepEqual(content.To, payload.To) || !reflect.DeepEqual(content.CC, payload.CC) {
				t.Fatal("durable ordinary content lost")
			}
			attachments, e := f.st.ListSubmissionAttachments(ctx, f.u, id)
			must(t, e)
			if len(attachments) != 1 || attachments[0].ID != att.ID {
				t.Fatal("durable attachment lost")
			}
			success := c
			success.Expected.Status = 200
			success.Expected.Wire.Code = ""
			view := r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+id.String()+"/content", nil, success)
			r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+id.String()+"/attachments", nil, success)
			var asset []byte
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, id).Scan(&asset))
			marker := r5Input[string](t, c, "target_failure_marker")
			for _, bcc := range payload.BCC {
				if !strings.Contains(string(asset), bcc) {
					t.Errorf("%s: durable sent asset has no structured BCC snapshot", marker)
				}
				if !strings.Contains(view.Body.String(), bcc) {
					t.Errorf("%s: current eligible sent-content reader cannot see BCC", marker)
				}
			}
		})
	}
	if executed != 3 {
		t.Fatalf("BCC asset coverage drift: %d", executed)
	}
}

func TestR5ProtocolGCBoundedSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "bounded_gc" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			protected := r5Input[int](t, c, "protected_prefix_count")
			orphanIndex := r5Input[int](t, c, "expired_orphan_index")
			if protected < 1 || orphanIndex != protected+1 {
				t.Fatal("invalid bounded GC scenario")
			}
			ids := []uuid.UUID{}
			for i := 1; i <= orphanIndex; i++ {
				id := uuid.MustParse(fmt.Sprintf("10000000-0000-0000-0000-%012x", i))
				ids = append(ids, id)
				_, e := f.pool.Exec(ctx, `INSERT INTO mail_attachments(id,tenant_id,mailbox_id,user_id,object_key,filename,content_type,size,sha256,state) VALUES($1,$2,$3,$4,$5,'protocol.txt','text/plain',1,$6,'ready')`, id, f.tenant.ID, f.personal.ID, f.employee.ID, "r5-protocol-"+id.String(), company.Hash("x"))
				must(t, e)
			}
			for i := 0; i < protected; i += 10 {
				end := i + 10
				if end > protected {
					end = protected
				}
				_, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{AttachmentIDs: ids[i:end]}})
				must(t, e)
			}
			_, e := f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=$1`, f.tenant.ID)
			must(t, e)
			for i := 0; i < 3; i++ {
				must(t, f.st.SweepCompanyMetadata(ctx))
			}
			var kept, orphan int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE id<>$2),count(*) FILTER(WHERE id=$2) FROM mail_attachments WHERE tenant_id=$1`, f.tenant.ID, ids[orphanIndex-1]).Scan(&kept, &orphan))
			if kept != protected {
				t.Fatal("GC deleted a referenced attachment")
			}
			if orphan != 0 {
				t.Error("R5_PROTOCOL_TARGET_GC01: protected prefix starved the next expired orphan")
			}
		})
	}
	if executed != 1 {
		t.Fatalf("bounded GC coverage drift: %d", executed)
	}
}

func TestR5ProtocolOffboardingEligibilitySharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "offboarding_eligibility" {
			continue
		}
		executed++
		for _, v := range r5Input[[]struct {
			Name   string
			Status int
			Code   string
			Stage  string
		}](t, c, "variants") {
			t.Run(c.ID+"/"+v.Name, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				h := r5SharedRouter(t, f)
				successor := f.other.ID
				want := c
				found := false
				for _, expected := range c.Expected.Variants {
					if expected.Name == v.Name {
						want.Expected.Status = expected.Status
						want.Expected.Wire.Code = expected.Code
						found = true
					}
				}
				if !found {
					t.Fatal("missing eligibility expected variant")
				}
				want.Expected.Wire.Reason = "absent"
				path := "/api/v1/company/employees/" + f.employee.ID.String() + "/offboard"
				var plan *company.OffboardingPlan
				if v.Stage == "execute" {
					p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, successor, company.OffboardingOptions{Drafts: "seal"}, "Eligibility must be checked again")
					must(t, e)
					plan = p
				}
				switch v.Name {
				case "frozen":
					_, e := f.pool.Exec(ctx, `UPDATE users SET is_active=false,session_version=session_version+1 WHERE id=$1`, f.other.ID)
					must(t, e)
				case "foreign_tenant":
					tenant := &models.Tenant{Name: "Foreign successor", PlanID: f.tenant.PlanID}
					must(t, f.st.CreateTenant(ctx, tenant))
					u := &models.User{TenantID: tenant.ID, Email: "foreign-successor@fixture.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
					must(t, f.st.CreateUser(ctx, u))
					successor = u.ID
				case "self":
					successor = f.employee.ID
				case "higher_role":
					_, e := f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.employee.ID)
					must(t, e)
				default:
					t.Fatal("unknown offboarding eligibility variant")
				}
				if v.Stage == "execute" {
					r5Wire(t, h, r3Token(t, f.admin), "POST", path, map[string]any{"plan_id": plan.ID}, want)
				} else {
					r5Wire(t, h, r3Token(t, f.admin), "POST", path+"/preview", map[string]any{"successor_user_id": successor, "options": map[string]string{"drafts": "seal"}, "reason": "Synthetic eligibility refusal without effects"}, want)
				}
				var active bool
				var audits int
				must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='employee.offboard' AND resource_id=$1`, f.employee.ID).Scan(&audits))
				if !active || audits != 0 {
					t.Fatal("invalid eligibility caused disposition effects")
				}
				if plan != nil {
					var state string
					must(t, f.pool.QueryRow(ctx, `SELECT state FROM employee_offboarding_plans WHERE id=$1`, plan.ID).Scan(&state))
					if state != "preview" {
						t.Fatal("invalid successor executed plan")
					}
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("eligibility coverage drift: %d", executed)
	}
}

func r5Observed(t *testing.T, h http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, e := json.Marshal(body)
	must(t, e)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func r5ConflictWire(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var env struct {
		Error struct {
			Code, Message string
			Reason        *string
		}
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &env))
	if w.Code != 409 || env.Error.Code != "CONFLICT" || env.Error.Message == "" || env.Error.Reason != nil {
		t.Fatalf("conflict wire mismatch: %d %s", w.Code, w.Body.String())
	}
}
func TestR5ProtocolPermissionStaleSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "permission_stale" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			h := r5SharedRouter(t, f)
			token := r3Token(t, f.admin)
			marker := r5Input[string](t, c, "target_failure_marker")
			if r5Input[string](t, c, "operation") == "profile_patch" {
				profile := r3Data[models.PermissionProfile](t, r3HTTP(t, h, token, "POST", "/api/v1/admin/permissions", map[string]any{"name": "Protocol stale profile", "can_send": true}, 201))
				profile = r5PEProfileSnapshot(t, h, token, profile.ID)
				member := r5PEEditorSnapshot(t, h, token, f.employee.ID)
				r5PEAssignEditor(t, h, token, f.employee.ID, member.Revision, &profile, map[string]any{})
				path := "/api/v1/admin/permissions/" + profile.ID.String()
				r3HTTP(t, h, token, "PATCH", path, map[string]any{"expected_revision": profile.Revision, "can_send": false}, 200)
				revoked, e := f.st.GetPermissionProfile(ctx, profile.ID)
				must(t, e)
				if revoked.CanSend {
					t.Fatal("profile revocation setup failed")
				}
				if !r5Input[bool](t, c, "stale_revision") || !r5Input[bool](t, c, "patch_contains_old_security_fields") {
					t.Fatal("stale profile scenario inputs missing")
				}
				w := r5Observed(t, h, token, "PATCH", path, map[string]any{"expected_revision": profile.Revision, "description": "Stale editor unrelated change", "can_send": profile.CanSend})
				current, e := f.st.GetPermissionProfile(ctx, profile.ID)
				must(t, e)
				if w.Code == 200 && current.CanSend {
					t.Errorf("%s: accepted stale profile restored revoked can_send", marker)
				} else if w.Code == 409 {
					r5ConflictWire(t, w)
					if current.CanSend {
						t.Fatal("rejected profile mutated revoked security")
					}
				} else {
					t.Fatalf("unexpected stale profile response: %d %s", w.Code, w.Body.String())
				}
				return
			}
			path := "/api/v1/admin/users/" + f.employee.ID.String() + "/permission-editor"
			observed := r5PEEditorSnapshot(t, h, token, f.employee.ID)
			old := r5PEEditorResponse(t, r5Observed(t, h, token, "PATCH", path, map[string]any{"expected_revision": observed.Revision, "patch": map[string]any{"can_send": true}}), f.employee.ID)
			if old.Overrides == nil || old.Overrides.CanSend == nil || !*old.Overrides.CanSend || !old.Effective.CanSend {
				t.Fatal("formal PE old-revision positive control did not enable send")
			}
			if !r5Input[bool](t, c, "old_revision") || !r5Input[bool](t, c, "override_recreated_or_profile_reassigned") {
				t.Fatal("ABA scenario inputs missing")
			}
			// The shared input permits profile reassignment. Use that formal ABA
			// path rather than an obsolete DELETE/recreate override-row protocol.
			alternate := r3Data[models.PermissionProfile](t, r3HTTP(t, h, token, "POST", "/api/v1/admin/permissions", map[string]any{"name": "Protocol ABA alternate profile", "can_send": false}, 201))
			alternate = r5PEProfileSnapshot(t, h, token, alternate.ID)
			different := r5PEAssignEditor(t, h, token, f.employee.ID, old.Revision, &alternate, map[string]any{"can_send": false})
			r5PEUserRevisionAdvanced(t, old.Revision, different.Revision)
			originalProfile := old.Profile
			if originalProfile != nil {
				currentProfile := r5PEProfileSnapshot(t, h, token, originalProfile.ID)
				originalProfile = &currentProfile
			}
			fresh := r5PEAssignEditor(t, h, token, f.employee.ID, different.Revision, originalProfile, map[string]any{"can_send": false})
			r5PEUserRevisionAdvanced(t, different.Revision, fresh.Revision)
			if !reflect.DeepEqual(old.Revision.ProfileID, fresh.Revision.ProfileID) || old.Revision.Equal(fresh.Revision) || fresh.Overrides == nil || fresh.Overrides.CanSend == nil || *fresh.Overrides.CanSend || fresh.Effective.CanSend {
				t.Fatal("formal PE ABA restored profile identity but lost stable revision or revoked raw send intent")
			}
			w := r5Observed(t, h, token, "PATCH", path, map[string]any{"expected_revision": old.Revision, "patch": map[string]any{"can_send": *old.Overrides.CanSend}})
			after, e := f.st.EffectivePermission(ctx, f.employee.ID)
			must(t, e)
			if w.Code == 200 && after.CanSend {
				t.Errorf("%s: recreated override accepted obsolete identity/version and restored send", marker)
			} else if w.Code == 409 {
				r5ConflictWire(t, w)
				if after.CanSend {
					t.Fatal("ABA conflict mutated restriction")
				}
			} else {
				t.Fatalf("unexpected ABA response: %d %s", w.Code, w.Body.String())
			}
		})
	}
	if executed != 2 {
		t.Fatalf("stale permission coverage drift: %d", executed)
	}
}

func TestR5ProtocolLegacyReplaySharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "legacy_receipt_replay" {
			continue
		}
		executed++
		f := seedCompany(t)
		ctx := context.Background()
		h := r5SharedRouter(t, f)
		token := r3Token(t, f.employee)
		payload := company.DraftPayload{To: []string{"visible@recipient.test"}, BCC: []string{"hidden@recipient.test"}, Subject: "safe legacy replay", TextBody: "PRIVATE_PROTOCOL_BODY", Headers: map[string]string{"X-Private": "PRIVATE_PROTOCOL_HEADER"}}
		draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: payload})
		must(t, e)
		first := draftSubmitHTTP(t, h, token, draft.ID.String(), "protocol-replay-command", draft.Revision)
		if first.Code != 201 {
			t.Fatalf("submit setup failed: %d %s", first.Code, first.Body.String())
		}
		id := r3Data[struct{ ID uuid.UUID }](t, first).ID
		if !r5Input[bool](t, c, "content_expired") {
			t.Fatal("expired content scenario required")
		}
		_, e = f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, id)
		must(t, e)
		if content, e := f.st.GetSubmissionContent(ctx, f.u, id); e == nil || content != nil {
			t.Fatal("canonical content not expired")
		}
		for _, operation := range r5Input[[]string](t, c, "operation") {
			t.Run(c.ID+"/"+operation, func(t *testing.T) {
				var w *httptest.ResponseRecorder
				want := c
				want.Expected.Status = 200
				want.Expected.Wire.Code = ""
				switch operation {
				case "legacy_outbound_list":
					w = r5Wire(t, h, token, "GET", "/api/v1/outbound", nil, want)
				case "legacy_outbound_detail":
					w = r5Wire(t, h, token, "GET", "/api/v1/outbound/"+id.String(), nil, want)
				case "submit_replay":
					w = draftSubmitHTTP(t, h, token, draft.ID.String(), "protocol-replay-command", draft.Revision)
					if w.Code != 200 {
						t.Fatalf("replay failed: %d %s", w.Code, w.Body.String())
					}
					if r3Data[struct{ ID uuid.UUID }](t, w).ID != id {
						t.Fatal("replay created another job")
					}
				default:
					t.Fatal("unknown legacy compatibility operation")
				}
				for _, private := range []string{payload.TextBody, "PRIVATE_PROTOCOL_HEADER", payload.BCC[0]} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatal("legacy compatibility entrypoint returned expired private content")
					}
				}
				if !strings.Contains(w.Body.String(), payload.Subject) {
					t.Fatal("legitimate safe receipt erased")
				}
				var jobs int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM outbound_jobs WHERE draft_id=$1`, draft.ID).Scan(&jobs))
				if jobs != 1 {
					t.Fatal("compatibility replay duplicated job")
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("legacy replay coverage drift: %d", executed)
	}
}

func TestR5ProtocolProtectedGCSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "protected_gc" {
			continue
		}
		executed++
		for _, kind := range r5Input[[]string](t, c, "reference_kind") {
			t.Run(c.ID+"/"+kind, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				if !r5Input[bool](t, c, "tombstone_exists") {
					t.Fatal("tombstone preservation scenario required")
				}
				if kind == "sealed_draft" {
					att := r5ReadyReference(t, f, ctx)
					draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{ID: uuid.New(), MailboxID: f.personal.ID, Payload: company.DraftPayload{AttachmentIDs: []uuid.UUID{att.ID}}})
					must(t, e)
					p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Protected sealed draft original custody")
					must(t, e)
					_, e = f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID)
					must(t, e)
					var receipt, sealed int
					must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM draft_creation_receipts WHERE id=$1),(SELECT count(*) FROM mail_drafts WHERE id=$1 AND sealed_at IS NOT NULL)`, draft.ID).Scan(&receipt, &sealed))
					if receipt != 1 || sealed != 1 {
						t.Fatal("sealed draft/tombstone setup failed")
					}
					_, e = f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, att.ID)
					must(t, e)
					must(t, f.st.SweepCompanyMetadata(ctx))
					r5ReferenceCounts(t, f, ctx, att, 1, 0)
					return
				}
				if kind != "held_raw" {
					t.Fatal("unknown protected GC reference kind")
				}
				claim, _ := r5ClaimIngress(t, f)
				must(t, f.st.HoldIngressTarget(ctx, claim, f.personal.ID, "Synthetic retained original for held fixed destination"))
				must(t, f.st.FinishIngress(ctx, claim, 3, time.Now()))
				_, e := f.pool.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, f.personal.ID)
				must(t, e)
				targets, e := f.st.ListIngressTargets(ctx, claim.Job.ID)
				must(t, e)
				if len(targets) != 1 || targets[0].State != "held" || targets[0].MailboxID != f.personal.ID {
					t.Fatal("held destination tombstone lost")
				}
				must(t, f.st.EnqueueOrphanRetry(ctx, claim.Job.RawObjectKey))
				must(t, f.st.SweepCompanyMetadata(ctx))
				refs, e := f.st.CountRawObjectReferences(ctx, claim.Job.RawObjectKey)
				must(t, e)
				if refs < 1 {
					t.Fatal("held original lost authoritative reference")
				}
				called := false
				deleted, e := f.st.ReleaseRawObjectIfUnreferenced(ctx, claim.Job.RawObjectKey, func(context.Context) error { called = true; return nil })
				must(t, e)
				if deleted || called {
					t.Fatal("tombstone/orphan registration authorized deletion of held original")
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("protected GC coverage drift: %d", executed)
	}
}

func TestR5ProtocolLogicalExpiryReferenceSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "logical_expiry_reference" {
			continue
		}
		executed++
		for _, kind := range r5Input[[]string](t, c, "reference_kind") {
			t.Run(c.ID+"/"+kind, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				if !r5Input[bool](t, c, "content_expired") {
					t.Fatal("logical content expiry scenario required")
				}
				if kind == "held" {
					claim, m := r5ClaimIngress(t, f)
					must(t, f.st.HoldIngressTarget(ctx, claim, f.personal.ID, "Original protected after normal content expiry"))
					must(t, f.st.FinishIngress(ctx, claim, 3, time.Now()))
					must(t, f.st.CreateMessage(ctx, m))
					_, e := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '1 day',purge_after=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
					must(t, e)
					r5ProtocolExpiredMessageBoundary(t, f, ctx, m)
					_, e = f.pool.Exec(ctx, `DELETE FROM messages WHERE id=$1`, m.ID)
					must(t, e)
					called := false
					deleted, e := f.st.ReleaseRawObjectIfUnreferenced(ctx, claim.Job.RawObjectKey, func(context.Context) error { called = true; return nil })
					must(t, e)
					if deleted || called {
						t.Fatal("logical expiry deleted held original")
					}
					return
				}
				att := r5ReadyReference(t, f, ctx)
				switch kind {
				case "draft":
					_, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{AttachmentIDs: []uuid.UUID{att.ID}}})
					must(t, e)
				case "sent_asset", "queue":
					j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"client@fixture.test"}, RcptTo: []string{"client@fixture.test"}, AttachmentIDs: []uuid.UUID{att.ID}, State: models.OutboundPending}
					must(t, f.st.CreateOutboundJob(ctx, j))
					if kind == "sent_asset" {
						_, e := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
						must(t, e)
					} else {
						_, e := f.pool.Exec(ctx, `DELETE FROM sent_mail_items WHERE asset_id=$1`, j.ID)
						must(t, e)
						_, e = f.pool.Exec(ctx, `DELETE FROM sent_mail_assets WHERE id=$1`, j.ID)
						must(t, e)
					}
				default:
					t.Fatal("unknown durable reference kind")
				}
				// An expired normal item sharing the underlying object is not a deletion
				// authority over another independently-live asset's original bytes.
				m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "client@fixture.test", Recipients: []string{f.personal.FullAddress}, Subject: "expired logical item", RawObjectKey: att.ObjectKey}
				must(t, f.st.CreateMessage(ctx, m))
				_, e := f.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '1 day',purge_after=clock_timestamp()-interval '1 second' WHERE id=$1`, m.ID)
				must(t, e)
				r5ProtocolExpiredMessageBoundary(t, f, ctx, m)
				_, e = f.pool.Exec(ctx, `DELETE FROM messages WHERE id=$1`, m.ID)
				must(t, e)
				_, e = f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, att.ID)
				must(t, e)
				must(t, f.st.SweepCompanyMetadata(ctx))
				r5ReferenceCounts(t, f, ctx, att, 1, 0)
				called := false
				deleted, e := f.st.ReleaseRawObjectIfUnreferenced(ctx, att.ObjectKey, func(context.Context) error { called = true; return nil })
				must(t, e)
				if deleted || called {
					t.Fatal("expired logical item authorized protected object deletion")
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("expiry/reference coverage drift: %d", executed)
	}
}

func TestR5ProtocolLegacyBCCObservationSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "legacy_bcc_observation" {
			continue
		}
		executed++
		variants := []string{"reliable_source"}
		if r5Input[string](t, c, "trusted_source") == "missing_or_unprovable" {
			variants = []string{"missing_source", "foreign_job"}
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				h := r5SharedRouter(t, f)
				j := r5LegacyJob(t, f)
				if variant != "reliable_source" {
					_, e := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
					must(t, e)
				}
				if variant == "foreign_job" {
					tenant := &models.Tenant{Name: "Unprovable legacy source", PlanID: f.tenant.PlanID}
					must(t, f.st.CreateTenant(ctx, tenant))
					zone := &models.DomainZone{TenantID: tenant.ID, Domain: "foreign.test", IsVerified: true, MXVerified: true}
					must(t, f.st.CreateZone(ctx, zone))
					// The actual foreign job has the same historical ID but no sender-mailbox
					// provenance. No archive trigger fabricates an asset for it.
					foreign := &models.OutboundJob{ID: j.ID, TenantID: tenant.ID, ZoneID: zone.ID, MailFrom: "foreign@foreign.test", To: []string{"client@foreign.test"}, BCC: []string{"FOREIGN_PRIVATE_BCC@foreign.test"}, RcptTo: []string{"client@foreign.test", "FOREIGN_PRIVATE_BCC@foreign.test"}, State: models.OutboundSent}
					must(t, f.st.CreateOutboundJob(ctx, foreign))
				}
				var trusted int
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM sent_mail_assets a JOIN outbound_jobs j ON j.id=a.id AND j.tenant_id=a.tenant_id AND j.zone_id=a.zone_id AND j.sender_mailbox_id=a.sender_mailbox_id WHERE a.id=$1`, j.ID).Scan(&trusted))
				if variant == "reliable_source" {
					if !r5Input[bool](t, c, "job_exists") || !r5Input[bool](t, c, "exact_tenant_asset_match") || trusted != 1 {
						t.Fatal("reliable legacy source not exact tenant/asset provenance")
					}
				} else if trusted != 0 {
					t.Fatal("unprovable source incorrectly treated as reliable")
				}
				var before []byte
				must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&before))
				views := 1
				if r5Input[bool](t, c, "repeat") {
					views = 2
				}
				success := c
				success.Expected.Status = 200
				success.Expected.Wire.Code = ""
				var last *httptest.ResponseRecorder
				for i := 0; i < views; i++ {
					content, e := f.st.GetSubmissionContent(ctx, f.u, j.ID)
					must(t, e)
					if content.TextBody != j.TextBody || !reflect.DeepEqual(content.To, j.To) || !reflect.DeepEqual(content.CC, j.CC) {
						t.Fatal("legacy ordinary values changed")
					}
					last = r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, success)
				}
				var after []byte
				must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&after))
				if !bytes.Equal(before, after) {
					t.Fatal("repeat observation overwrote immutable original values")
				}
				if strings.Contains(last.Body.String(), "FOREIGN_PRIVATE_BCC") {
					t.Fatal("foreign job backfilled another tenant asset")
				}
				marker := r5Input[string](t, c, "target_failure_marker")
				if variant == "reliable_source" {
					for _, bcc := range j.BCC {
						if !strings.Contains(string(after), bcc) {
							t.Errorf("%s: exact current job has BCC but durable legacy asset lacks recoverable recipient snapshot", marker)
						}
					}
				} else if !strings.Contains(last.Body.String(), "legacy_unknown") {
					t.Errorf("%s: unavailable/unprovable legacy source has no unknown completeness marker in actual response", marker)
				}
			})
		}
	}
	if executed != 2 {
		t.Fatalf("legacy BCC observation coverage drift: %d", executed)
	}
}

type r5CancelStreamWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *r5CancelStreamWriter) Write(p []byte) (int, error) {
	n, e := w.ResponseRecorder.Write(p)
	if bytes.Contains(p, []byte("event: ready")) {
		w.cancel()
	}
	return n, e
}

// io.WriteString prefers StringWriter; override the promoted recorder method
// so readiness cancellation cannot be bypassed by the production serializer.
func (w *r5CancelStreamWriter) WriteString(value string) (int, error) { return w.Write([]byte(value)) }

func TestR5ProtocolBCCSurfacesSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "bcc_surfaces" {
			continue
		}
		executed++
		f := seedCompany(t)
		ctx := context.Background()
		h := r5SharedRouter(t, f)
		j := r5LegacyJob(t, f)
		token := r3Token(t, f.employee)
		if !r5Input[bool](t, c, "has_bcc") || len(j.BCC) == 0 {
			t.Fatal("BCC surface fixture missing BCC")
		}
		for _, surface := range r5Input[[]string](t, c, "operation") {
			t.Run(c.ID+"/"+surface, func(t *testing.T) {
				var observed []byte
				switch surface {
				case "list", "receipt":
					path := "/api/v1/company/submissions"
					if surface == "receipt" {
						path += "/" + j.ID.String()
					}
					w := r5Observed(t, h, token, "GET", path, nil)
					if w.Code != 200 {
						t.Fatalf("surface HTTP failed: %d %s", w.Code, w.Body.String())
					}
					observed = w.Body.Bytes()
				case "sse":
					streamCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					defer cancel()
					r := httptest.NewRequest("GET", "/api/v1/company/mailboxes/"+f.personal.ID.String()+"/events", nil).WithContext(streamCtx)
					r.Header.Set("Authorization", "Bearer "+token)
					r.Header.Set("Last-Event-ID", "0")
					w := &r5CancelStreamWriter{httptest.NewRecorder(), cancel}
					h.ServeHTTP(w, r)
					if w.Code != 200 || !strings.Contains(w.Body.String(), "event: sent.created") || !strings.Contains(w.Body.String(), "event: ready") || streamCtx.Err() == context.DeadlineExceeded {
						t.Fatalf("actual sent SSE missing: %d %s", w.Code, w.Body.String())
					}
					observed = w.Body.Bytes()
				case "wire_mime":
					wire, e := outbound.Build(outbound.Message{From: j.MailFrom, To: j.To, CC: j.CC, BCC: j.BCC, Subject: j.Subject, TextBody: j.TextBody})
					must(t, e)
					observed = wire
				case "metrics":
					values, e := f.st.CompanyMetrics(ctx)
					must(t, e)
					observed = []byte(metrics.RenderPrometheus(metrics.Snapshot(false, 0), values))
					if len(observed) == 0 {
						t.Fatal("actual metrics renderer empty")
					}
				case "webhook":
					received := make(chan []byte, 32)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						b, e := io.ReadAll(r.Body)
						if e == nil {
							select {
							case received <- b:
							default:
							}
						}
						w.WriteHeader(204)
					}))
					defer server.Close()
					// Produce the real metadata-only webhook event through a mailbox action,
					// then actual outbox/delivery workers POST it to the loopback receiver.
					m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "client@fixture.test", Recipients: []string{f.personal.FullAddress}, Subject: j.Subject, RawObjectKey: "surface-fixture"}
					must(t, f.st.CreateMessage(ctx, m))
					must(t, f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "trash"))
					var eventID uuid.UUID
					var actualEventType string
					must(t, f.pool.QueryRow(ctx, `SELECT id,event_type FROM outbox_events WHERE payload->>'message_id'=$1 ORDER BY created_at DESC LIMIT 1`, m.ID.String()).Scan(&eventID, &actualEventType))
					if actualEventType == "" {
						t.Fatal("actual outbox producer emitted no event type")
					}
					dispatch := hooks.New(hooks.Config{URLs: server.URL, AllowedCIDRs: "127.0.0.1/32,::1/128", Timeout: time.Second, PollInterval: 10 * time.Millisecond, BatchSize: 100}, zerolog.Nop()).BindStore(f.st)
					workerCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
					defer cancel()
					done := make(chan struct{})
					go func() { defer close(done); dispatch.Run(workerCtx) }()
					matched := false
					receivedSummaries := []string{}
					for !matched {
						select {
						case b := <-received:
							for _, bcc := range j.BCC {
								if bytes.Contains(b, []byte(bcc)) {
									t.Fatal("actual webhook leaked BCC")
								}
							}
							var actual hooks.Event
							must(t, json.Unmarshal(b, &actual))
							receivedSummaries = append(receivedSummaries, fmt.Sprintf("type=%s message_id=%s", actual.Type, actual.MessageID))
							if actual.Type == actualEventType && actual.MessageID == m.ID.String() {
								if actual.Mailbox != f.personal.FullAddress || actual.TenantID != f.tenant.ID.String() {
									t.Fatal("actual webhook scope changed")
								}
								observed = b
								matched = true
							}
						case <-workerCtx.Done():
							r5WebhookProtocolDiagnostics(t, f, eventID, actualEventType, server.URL, receivedSummaries)
							t.Fatal("actual webhook worker did not deliver expected event")
						}
					}
					ackPoll := time.NewTicker(10 * time.Millisecond)
					defer ackPoll.Stop()
					for {
						var state string
						must(t, f.pool.QueryRow(workerCtx, `SELECT state FROM webhook_deliveries WHERE event_id=$1 AND url=$2`, eventID, server.URL).Scan(&state))
						if state == "delivered" {
							break
						}
						select {
						case <-workerCtx.Done():
							t.Fatal("actual webhook acknowledgement not persisted")
						case <-ackPoll.C:
						}
					}
					cancel()
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Fatal("webhook workers failed to stop")
					}
				default:
					t.Fatal("unknown BCC public surface")
				}
				for _, bcc := range j.BCC {
					if bytes.Contains(observed, []byte(bcc)) {
						if surface == "list" || surface == "receipt" {
							t.Error("R5_PROTOCOL_TARGET_BC05: ordinary submission surface disclosed BCC address")
						} else {
							t.Fatal("BCC address leaked on actual non-content surface")
						}
					}
				}
			})
		}
	}
	if executed != 1 {
		t.Fatalf("BCC surface coverage drift: %d", executed)
	}
}

func TestR5ProtocolDraftGrantOrderingSharedCases(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if r5Input[string](t, c, "protocol_consumer") != "draft_grant_ordering" {
			continue
		}
		executed++
		t.Run(c.ID, func(t *testing.T) {
			if !r5Input[bool](t, c, "wait_for_draft_lock") || !r5Input[bool](t, c, "send_grant_revoked_during_wait") {
				t.Fatal("draft/grant ordering inputs missing")
			}
			f := seedCompany(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			h := r5SharedRouter(t, f)
			must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
			draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "before revocation"}})
			must(t, e)
			snapshot, e := f.st.GetWorkMailbox(ctx, f.a, f.shared.ID)
			must(t, e)
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, draft.ID)
			must(t, e)
			changed := *draft
			changed.Payload.Subject = "authorized writer at shared seam"
			raw, e := json.Marshal(changed)
			must(t, e)
			req := httptest.NewRequest("PUT", "/api/v1/company/drafts/"+draft.ID.String(), bytes.NewReader(raw)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+r3Token(t, f.employee))
			saved := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, req); saved <- w }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "UPDATE mail_drafts SET mailbox_id")
			grantBody := map[string]any{"user_id": f.employee.ID, "can_read": true, "can_send": false, "revision": snapshot.Revision}
			grantRaw, e := json.Marshal(grantBody)
			must(t, e)
			grantReq := httptest.NewRequest("PUT", "/api/v1/company/mailboxes/"+f.shared.ID.String()+"/grants", bytes.NewReader(grantRaw)).WithContext(ctx)
			grantReq.Header.Set("Content-Type", "application/json")
			grantReq.Header.Set("Authorization", "Bearer "+r3Token(t, f.admin))
			revoked := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, grantReq); revoked <- w }()
			completed := false
			var revokeResponse *httptest.ResponseRecorder
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
		observe:
			for {
				select {
				case revokeResponse = <-revoked:
					completed = true
					break observe
				default:
				}
				var blocked bool
				must(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%UPDATE mailboxes SET lifecycle_revision%')`, int32(writer)).Scan(&blocked))
				if blocked {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("neither real revocation completion nor database lock order observed")
				case <-ticker.C:
				}
			}
			must(t, hold.Rollback(ctx))
			var writeResponse *httptest.ResponseRecorder
			select {
			case writeResponse = <-saved:
			case <-ctx.Done():
				t.Fatal("actual HTTP writer did not finish after draft lock release")
			}
			if !completed {
				select {
				case revokeResponse = <-revoked:
				case <-ctx.Done():
					t.Fatal("actual HTTP revoker did not finish")
				}
			}
			if revokeResponse.Code != 200 {
				t.Fatalf("actual grant revocation failed: %d %s", revokeResponse.Code, revokeResponse.Body.String())
			}
			access, e := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
			must(t, e)
			if access.CanSend {
				t.Fatal("grant revocation did not commit")
			}
			var payloadRaw []byte
			var revision int
			must(t, f.pool.QueryRow(ctx, `SELECT payload,revision FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&payloadRaw, &revision))
			var payload company.DraftPayload
			must(t, json.Unmarshal(payloadRaw, &payload))
			if completed && writeResponse.Code == 200 && revision == draft.Revision+1 && payload.Subject == changed.Payload.Subject {
				t.Error("R5_PROTOCOL_TARGET_PE05: authorized snapshot committed after completed grant revocation")
			} else if completed {
				if writeResponse.Code != 403 || revision != draft.Revision || payload.Subject != draft.Payload.Subject {
					t.Fatalf("revoke-first outcome not precise deny/no-effect: %d %s", writeResponse.Code, writeResponse.Body.String())
				}
			} else if writeResponse.Code != 200 || revision != draft.Revision+1 || payload.Subject != changed.Payload.Subject {
				t.Fatalf("save-first order did not commit before revoke: %d %s", writeResponse.Code, writeResponse.Body.String())
			}
			changed.Revision = revision
			want := c
			want.Expected.Status = 403
			want.Expected.Wire.Code = "FORBIDDEN"
			want.Expected.Wire.Reason = "absent"
			r5Wire(t, h, r3Token(t, f.employee), "PUT", "/api/v1/company/drafts/"+draft.ID.String(), changed, want)
		})
	}
	if executed != 1 {
		t.Fatalf("grant ordering coverage drift: %d", executed)
	}
}

// Evidence separates a real known logical-read gap from GC protection. The
// cutoff is confirmed by SQL before any marker; unexpected errors are fatal.
func r5ProtocolExpiredMessageBoundary(t *testing.T, f *companyFixture, ctx context.Context, m *models.Message) {
	t.Helper()
	var expired bool
	var key string
	must(t, f.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL AND purge_after<=clock_timestamp(),raw_object_key FROM messages WHERE id=$1`, m.ID).Scan(&expired, &key))
	if !expired || key != m.RawObjectKey {
		t.Fatal("logical expiry SQL preconditions not established")
	}
	value, e := f.st.GetWorkMessage(ctx, f.u, f.personal.ID, m.ID)
	if e == nil && value != nil && value.ID == m.ID && value.TenantID == f.tenant.ID && value.RawObjectKey == key {
		t.Error("R5_PROTOCOL_TARGET_RT07_READ_BOUNDARY: actual GetWorkMessage returned content provenance beyond confirmed purge cutoff")
		return // The caller still executes ALL protected-reference/reaper assertions.
	}
	v, ok := app.As(e)
	if value != nil || !ok || v.Kind != app.KindNotFound {
		t.Fatalf("unexpected expired-content boundary result: %v", e)
	}
}

func TestR5ProtocolStreamWriterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &r5CancelStreamWriter{httptest.NewRecorder(), cancel}
	_, e := io.WriteString(w, "event: sent.created\n")
	must(t, e)
	if ctx.Err() != nil {
		t.Fatal("cancelled before actual ready frame")
	}
	_, e = io.WriteString(w, "event: ready\n")
	must(t, e)
	if ctx.Err() != context.Canceled {
		t.Fatal("StringWriter bypassed readiness cancellation")
	}
	if !strings.Contains(w.Body.String(), "event: ready") {
		t.Fatal("cancellation erased observed stream frame")
	}
}

func r5WebhookProtocolDiagnostics(t *testing.T, f *companyFixture, eventID uuid.UUID, eventType, url string, received []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var event []byte
	e := f.pool.QueryRow(ctx, `SELECT jsonb_build_object('id',id,'type',event_type,'state',state,'attempts',attempts,'last_error',left(COALESCE(last_error,'<NULL>'),200),'last_error_is_null',last_error IS NULL) FROM outbox_events WHERE id=$1`, eventID).Scan(&event)
	t.Logf("isolated webhook outbox=%s query_error=%v expected_type=%s received_metadata=%v", event, e, eventType, received)
	var deliveries []byte
	e = f.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('event_id',event_id,'url',url,'state',state,'attempts',attempts,'last_error',left(COALESCE(last_error,'<NULL>'),200),'last_error_is_null',last_error IS NULL)),'[]'::jsonb) FROM webhook_deliveries WHERE event_id=$1`, eventID).Scan(&deliveries)
	t.Logf("isolated webhook deliveries=%s query_error=%v loopback_url=%s", deliveries, e, url)
}
