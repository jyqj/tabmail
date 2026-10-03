package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// Prepared for the sole owned PostgreSQL executor. Missing infrastructure is
// not a passing/skipped acceptance result.
func r5SubmissionPrivacyFixture(t *testing.T, shared ...bool) (*companyFixture, *models.OutboundJob) {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("submission receipt privacy acceptance requires owned TABMAIL_TEST_DB_DSN")
	}
	f := seedCompany(t)
	mailbox := f.personal
	if len(shared) > 0 && shared[0] {
		mailbox = f.shared
	}
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &mailbox.ID, MailFrom: mailbox.FullAddress, To: []string{"to@fixture.test"}, CC: []string{"cc@fixture.test"}, BCC: []string{"hidden@fixture.test"}, RcptTo: []string{"to@fixture.test", "cc@fixture.test", "hidden@fixture.test"}, Subject: "safe receipt", TextBody: "PRIVATE_LEGACY_BODY", HTMLBody: "<b>PRIVATE_LEGACY_BODY</b>", HeadersJSON: json.RawMessage(`{"X-Private":"PRIVATE_LEGACY_HEADER"}`), LastError: "PRIVATE_LEGACY_DIAGNOSTIC", SMTPResponse: "PRIVATE_LEGACY_DIAGNOSTIC", State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(context.Background(), j))
	_, err := f.pool.Exec(context.Background(), `UPDATE outbound_recipients SET state='accepted',diagnostic='PRIVATE_LEGACY_DIAGNOSTIC' WHERE job_id=$1`, j.ID)
	must(t, err)
	return f, j
}

func r5SubmissionPrivacyJSON(t *testing.T, value any, hidden ...string) {
	t.Helper()
	raw, err := json.Marshal(value)
	must(t, err)
	for _, private := range append([]string{"PRIVATE_LEGACY", "PRIVATE_DISPLAY", "PRIVATE_INVALID_STATE"}, hidden...) {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(private)) {
			t.Fatalf("ordinary receipt contains private value %q: %s", private, raw)
		}
	}
	var wire any
	must(t, json.Unmarshal(raw, &wire))
	forbidden := map[string]bool{"bcc": true, "bcc_addrs": true, "text_body": true, "html_body": true, "headers": true, "headers_json": true, "smtp_response": true, "last_error": true, "diagnostic": true, "raw_mime": true, "object_key": true, "delivery_token": true, "rcpt_to": true, "subject": true, "from": true, "mail_from": true, "to": true, "cc": true, "address": true, "recipients": true, "attachment_count": true, "smtp_code": true, "user_id": true, "api_key_id": true, "lease_until": true, "claimed_at": true}
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for name, child := range x {
				if forbidden[strings.ToLower(name)] {
					t.Fatalf("ordinary receipt contains forbidden field %q", name)
				}
				visit(child)
			}
		case []any:
			for _, child := range x {
				visit(child)
			}
		}
	}
	visit(wire)
}

func r5SubmissionPrivacyViews(t *testing.T, f *companyFixture, actor authz.Actor, id uuid.UUID) []company.Submission {
	t.Helper()
	one, err := f.st.GetSubmission(context.Background(), actor, id)
	must(t, err)
	list, total, err := f.st.ListSubmissions(context.Background(), actor, models.Page{Page: 1, PerPage: 100})
	must(t, err)
	if one == nil || total != 1 || len(list) != 1 || list[0].ID != id {
		t.Fatalf("receipt/list identity contradiction: detail=%+v list=%+v total=%d", one, list, total)
	}
	if !reflect.DeepEqual(*one, list[0]) {
		t.Fatal("ordinary list/detail projection contradiction")
	}
	return []company.Submission{*one, list[0]}
}

func TestR5SubmissionReceiptPrivacyPGCompleteLedger(t *testing.T) {
	cases := []struct {
		name                     string
		job                      models.OutboundState
		hidden, inFlight, status string
		uncertain                bool
	}{
		{"all-next-hop-accepted", models.OutboundSent, "accepted", "", company.SubmissionAccepted, false},
		{"private-permanent-failure", models.OutboundSent, "permanent", "", company.SubmissionPartiallyAccepted, false},
		{"private-temporary-failure", models.OutboundRetry, "temporary", "", company.SubmissionPartiallyAccepted, false},
		{"private-uncertain", models.OutboundSent, "uncertain", "", company.SubmissionNeedsAttention, true},
		{"private-processing-uncertain", models.OutboundProcessing, "uncertain", "", company.SubmissionNeedsAttention, true},
		{"in-flight-ambiguity", models.OutboundFailed, "accepted", "fixture.test", company.SubmissionNeedsAttention, true},
		{"cancelled-with-private-acceptance", models.OutboundCancelled, "accepted", "", company.SubmissionPartiallyAccepted, false},
		{"empty-ledger", models.OutboundSent, "empty", "", company.SubmissionNeedsAttention, false},
		{"malformed-ledger", models.OutboundSent, "invalid", "", company.SubmissionNeedsAttention, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			_, err := f.pool.Exec(ctx, `UPDATE outbound_jobs SET state=$2,in_flight_domain=$3 WHERE id=$1`, j.ID, tc.job, tc.inFlight)
			must(t, err)
			if tc.name == "cancelled-with-private-acceptance" {
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='permanent' WHERE job_id=$1 AND address<>$2`, j.ID, j.BCC[0])
				must(t, err)
			}
			switch tc.hidden {
			case "empty":
				_, err = f.pool.Exec(ctx, `DELETE FROM outbound_recipients WHERE job_id=$1`, j.ID)
			case "invalid":
				_, err = f.pool.Exec(ctx, `ALTER TABLE outbound_recipients DROP CONSTRAINT outbound_recipients_state_check`)
				must(t, err)
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='PRIVATE_INVALID_STATE' WHERE job_id=$1`, j.ID)
			default:
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state=$3 WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0], tc.hidden)
			}
			must(t, err)
			for _, view := range r5SubmissionPrivacyViews(t, f, f.u, j.ID) {
				r5SubmissionPrivacyJSON(t, view, j.BCC...)
				if view.Status != tc.status || view.DeliveryUncertain != tc.uncertain {
					t.Fatalf("complete-ledger result=%+v want=%s uncertain=%v", view, tc.status, tc.uncertain)
				}
				if tc.hidden == "empty" || tc.hidden == "invalid" {
					if view.Progress.Completeness != "unknown" || view.Progress.Counts != nil {
						t.Fatal("empty/malformed ledger fabricated counts")
					}
				} else if view.Progress.Completeness != "known" || view.Progress.Counts == nil || view.Progress.Counts.Total != 3 {
					t.Fatal("private recipient removed from full aggregate")
				}
			}
		})
	}
}

func TestR5SubmissionReceiptPrivacyPGClassification(t *testing.T) {
	cases := []struct {
		name                      string
		to, cc, bcc, ledger, want []string
	}{
		{"public-also-bcc", []string{"both@fixture.test"}, nil, []string{"both@fixture.test", "hidden@fixture.test"}, []string{"both@fixture.test", "hidden@fixture.test"}, []string{"both@fixture.test"}},
		{"cc-also-bcc", nil, []string{"both@fixture.test"}, []string{"both@fixture.test", "hidden@fixture.test"}, []string{"both@fixture.test", "hidden@fixture.test"}, []string{"both@fixture.test"}},
		{"case-display-normalization", []string{"Public <PUBLIC@Fixture.Test>"}, nil, []string{"hidden@fixture.test"}, []string{"PUBLIC@FIXTURE.TEST", "hidden@fixture.test"}, []string{"public@fixture.test"}},
		{"historical-display-not-echoed", []string{"public@fixture.test"}, nil, []string{"hidden@fixture.test"}, []string{"PRIVATE_DISPLAY <public@fixture.test>", "hidden@fixture.test"}, []string{"public@fixture.test"}},
		{"plus-tag-is-not-public-base", []string{"public@fixture.test"}, nil, []string{"public+private@fixture.test"}, []string{"public@fixture.test", "public+private@fixture.test"}, []string{"public@fixture.test"}},
		{"dot-is-not-public-alias", []string{"public@fixture.test"}, nil, []string{"pub.lic@fixture.test"}, []string{"public@fixture.test", "pub.lic@fixture.test"}, []string{"public@fixture.test"}},
		{"unknown-envelope-category", []string{"public@fixture.test"}, nil, nil, []string{"public@fixture.test", "unclassified@fixture.test"}, []string{"public@fixture.test"}},
		{"bcc-only", nil, nil, []string{"hidden@fixture.test"}, []string{"hidden@fixture.test"}, []string{}},
		{"invalid-public-address", []string{"not-an-address"}, nil, []string{"hidden@fixture.test"}, []string{"not-an-address", "hidden@fixture.test"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
				t.Fatal("submission receipt privacy acceptance requires owned TABMAIL_TEST_DB_DSN")
			}
			f := seedCompany(t)
			j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, State: models.OutboundSent, To: tc.to, CC: tc.cc, BCC: tc.bcc, RcptTo: tc.ledger, Subject: "safe receipt", TextBody: "PRIVATE_LEGACY_BODY"}
			must(t, f.st.CreateOutboundJob(context.Background(), j))
			_, err := f.pool.Exec(context.Background(), `UPDATE outbound_recipients SET state='accepted' WHERE job_id=$1`, j.ID)
			must(t, err)
			for _, view := range r5SubmissionPrivacyViews(t, f, f.u, j.ID) {
				if view.Progress.Completeness != "known" || view.Progress.Counts == nil || view.Progress.Counts.Total != len(tc.ledger) {
					t.Fatal("classification affected complete aggregate")
				}
				r5SubmissionPrivacyJSON(t, view)
				if view.Status != company.SubmissionAccepted {
					t.Fatal("classification changed complete ledger acceptance")
				}
			}
		})
	}
}

func TestR5SubmissionReceiptPrivacyPGUnknownProvenance(t *testing.T) {
	for _, mode := range []string{"missing-asset", "different-mailbox", "different-zone", "live-job-public-tampering"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			switch mode {
			case "missing-asset":
				_, err := f.pool.Exec(ctx, `DELETE FROM sent_mail_items WHERE asset_id=$1`, j.ID)
				must(t, err)
				_, err = f.pool.Exec(ctx, `DELETE FROM sent_mail_assets WHERE id=$1`, j.ID)
				must(t, err)
			case "different-mailbox":
				_, err := f.pool.Exec(ctx, `UPDATE outbound_jobs SET sender_mailbox_id=$2 WHERE id=$1`, j.ID, f.shared.ID)
				must(t, err)
			case "different-zone":
				z := &models.DomainZone{TenantID: f.tenant.ID, Domain: "second.test", IsVerified: true, MXVerified: true}
				must(t, f.st.CreateZone(ctx, z))
				_, err := f.pool.Exec(ctx, `UPDATE outbound_jobs SET zone_id=$2 WHERE id=$1`, j.ID, z.ID)
				must(t, err)
			case "live-job-public-tampering":
				_, err := f.pool.Exec(ctx, `UPDATE outbound_jobs SET to_addrs=$2,cc_addrs=$2 WHERE id=$1`, j.ID, j.BCC)
				must(t, err)
			}
			for _, view := range r5SubmissionPrivacyViews(t, f, f.u, j.ID) {
				if view.Status != company.SubmissionAccepted || view.Progress.Counts == nil || view.Progress.Counts.Total != 3 {
					t.Fatalf("unknown/classification handling=%+v", view)
				}
				r5SubmissionPrivacyJSON(t, view, j.BCC...)
			}
		})
	}
}

func TestR5SubmissionReceiptPrivacyPGPrincipalAndLifetime(t *testing.T) {
	for _, mode := range []string{"owner", "shared-current-reader", "tenant-admin-no-grant", "tenant-admin-current-reader", "historical-submitter-no-read", "expired-content"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t, mode == "shared-current-reader")
			ctx := context.Background()
			a := f.u
			allowed, contentAllowed := true, true
			switch mode {
			case "shared-current-reader":
				a = authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: *j.SenderMailboxID, UserID: f.other.ID, CanRead: true}))
			case "tenant-admin-no-grant":
				a = f.a
				allowed = false
				contentAllowed = false
			case "tenant-admin-current-reader":
				a = f.a
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.admin.ID, CanRead: true}))
			case "historical-submitter-no-read":
				// Handover changes present read authority, not the recorded submitter.
				_, err := f.pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2 WHERE id=$1`, f.personal.ID, f.other.ID)
				must(t, err)
				contentAllowed = false
			case "expired-content":
				_, err := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
				must(t, err)
				contentAllowed = false
			}
			content, err := f.st.GetSubmissionContent(ctx, a, j.ID)
			if contentAllowed {
				must(t, err)
				if content == nil {
					t.Fatal("eligible content missing")
				}
			} else if err == nil || content != nil {
				t.Fatal("receipt identity or role granted body")
			}
			if allowed {
				for _, view := range r5SubmissionPrivacyViews(t, f, a, j.ID) {
					r5SubmissionPrivacyJSON(t, view, j.BCC...)
					if view.Progress.Counts == nil || view.Progress.Counts.Total != 3 {
						t.Fatal("safe aggregate disappeared with body rights")
					}
				}
			} else {
				view, err := f.st.GetSubmission(ctx, a, j.ID)
				if err == nil || view != nil {
					t.Fatal("tenant administration bypassed ordinary visibility")
				}
				list, total, err := f.st.ListSubmissions(ctx, a, models.Page{})
				must(t, err)
				if total != 0 || len(list) != 0 {
					t.Fatal("tenant administration listed another user's receipt")
				}
			}
		})
	}
}

func TestR5SubmissionReceiptPrivacyHTTPShippingProjection(t *testing.T) {
	for _, mode := range []string{"live-owner", "expired-content", "historical-submitter-no-read", "private-uncertain", "unknown-asset"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			contentAllowed := true
			status := company.SubmissionPartiallyAccepted
			uncertain := false
			_, err := f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='permanent' WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0])
			must(t, err)
			switch mode {
			case "expired-content":
				_, err = f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
				contentAllowed = false
			case "historical-submitter-no-read":
				_, err = f.pool.Exec(ctx, `UPDATE mailboxes SET owner_user_id=$2 WHERE id=$1`, f.personal.ID, f.other.ID)
				contentAllowed = false
			case "private-uncertain":
				_, err = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain' WHERE job_id=$1 AND address=$2`, j.ID, j.BCC[0])
				status = company.SubmissionNeedsAttention
				uncertain = true
			case "unknown-asset":
				_, err = f.pool.Exec(ctx, `DELETE FROM sent_mail_items WHERE asset_id=$1`, j.ID)
				must(t, err)
				_, err = f.pool.Exec(ctx, `DELETE FROM sent_mail_assets WHERE id=$1`, j.ID)
				contentAllowed = false
			}
			must(t, err)
			// Compose the default-build shipping router, not the r5protocol-tagged
			// adapter. No worker starts and no SMTP/network operation is invoked.
			obj := testutil.NewMemoryObjectStore()
			svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: 1}, f.st, f.st, zerolog.Nop())
			svc.SetObjectStore(obj)
			h := companyRouter(t, f, obj, svc)
			token := r3Token(t, f.employee)
			for _, endpoint := range []string{"/api/v1/company/submissions", "/api/v1/company/submissions/" + j.ID.String()} {
				w := r3HTTP(t, h, token, "GET", endpoint, nil, 200)
				r5SubmissionPrivacyJSON(t, json.RawMessage(w.Body.Bytes()), j.BCC...)
				var view company.Submission
				if endpoint == "/api/v1/company/submissions" {
					data := r3Data[[]company.Submission](t, w)
					if len(data) != 1 {
						t.Fatalf("shipping list length=%d", len(data))
					}
					view = data[0]
				} else {
					view = r3Data[company.Submission](t, w)
				}
				if view.Status != status || view.DeliveryUncertain != uncertain {
					t.Fatalf("shipping projection=%+v", view)
				}
				// The shipping list has no capability block; only detail computes hints.
				if endpoint != "/api/v1/company/submissions" && (view.Capabilities == nil || view.Capabilities.ViewContent != contentAllowed) {
					t.Fatalf("shipping content capability=%+v", view.Capabilities)
				}
				if view.Progress.Counts == nil || view.Progress.Counts.Total != 3 {
					t.Fatal("shipping complete ledger lost private targets")
				}
			}
			bodyStatus := 200
			if !contentAllowed {
				bodyStatus = 404
			}
			r3HTTP(t, h, token, "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, bodyStatus)
		})
	}
}

func TestR5SubmissionReceiptPrivacyPGFailClosedReadError(t *testing.T) {
	for _, mode := range []string{"ledger-unavailable", "asset-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			table := "outbound_recipients"
			if mode == "asset-unavailable" {
				table = "sent_mail_assets"
			}
			_, err := f.pool.Exec(context.Background(), `ALTER TABLE `+table+` RENAME TO privacy_test_unavailable`)
			must(t, err)
			one, err := f.st.GetSubmission(context.Background(), f.u, j.ID)
			list, total, listErr := f.st.ListSubmissions(context.Background(), f.u, models.Page{})
			if mode == "asset-unavailable" {
				must(t, err)
				must(t, listErr)
				if one == nil || one.Progress.Counts == nil || one.Progress.Counts.Total != 3 || total != 1 || len(list) != 1 {
					t.Fatal("lost content infrastructure erased truthful operation receipt")
				}
				r5SubmissionPrivacyJSON(t, one, j.BCC...)
			} else if err == nil || one != nil || listErr == nil || list != nil || total != 0 {
				t.Fatal("ledger read failure returned partial/fabricated receipt")
			}
		})
	}
}

func TestR5SubmissionReceiptPrivacyPGTenantIsolation(t *testing.T) {
	for _, mode := range []string{"current-foreign-member", "forged-tenant-on-author"} {
		t.Run(mode, func(t *testing.T) {
			f, j := r5SubmissionPrivacyFixture(t)
			ctx := context.Background()
			tenant := &models.Tenant{Name: "Privacy foreign company", PlanID: f.tenant.PlanID}
			must(t, f.st.CreateTenant(ctx, tenant))
			user := &models.User{TenantID: tenant.ID, Email: "privacy-foreign@fixture.test", DisplayName: "Foreign", Role: models.RoleUser, IsActive: true, PasswordHash: "not-a-production-password"}
			must(t, f.st.CreateUser(ctx, user))
			a := authz.Actor{Type: authz.PrincipalUser, ID: user.ID, TenantID: tenant.ID, Role: models.RoleUser}
			if mode == "forged-tenant-on-author" {
				a.ID = f.employee.ID
			}
			one, err := f.st.GetSubmission(ctx, a, j.ID)
			if err == nil || one != nil {
				t.Fatal("cross-tenant receipt payload returned")
			}
			list, total, err := f.st.ListSubmissions(ctx, a, models.Page{})
			if mode == "current-foreign-member" {
				must(t, err)
			} else if err == nil {
				t.Fatal("forged author tenant accepted")
			}
			if total != 0 || len(list) != 0 {
				t.Fatal("cross-tenant list leaked receipt metadata")
			}
		})
	}
}
