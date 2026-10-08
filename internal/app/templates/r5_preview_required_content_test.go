package templates

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

func r5RequiredContentDraft(subject, text, html string) company.TemplateDraft {
	return company.TemplateDraft{
		Subject: subject, TextBody: text, HTMLBody: html,
		Variables: []company.Variable{{Name: "value", Type: "text", MaxLength: 200}},
	}
}

func r5SetPreviewContent(f *previewBoundaryFixture, draft company.TemplateDraft, vars map[string]string) {
	if f.input.Draft != nil {
		f.input.Draft = &draft
	}
	f.input.Vars = vars
	f.repo.version.Snapshot = draft
	f.repo.version.ContentHash = company.Digest(draft)
}

func TestR5PreviewRequiredContentMatchesSubmission(t *testing.T) {
	for _, published := range []bool{false, true} {
		mode := map[bool]string{false: "management", true: "published"}[published]
		for _, tc := range []struct {
			name, subject, text, html, message string
			vars                               map[string]string
			blankEmployee                      bool
		}{
			{"subject_omitted", "{{.value}}", "Body", "", "subject is required", nil, false},
			{"subject_explicit_empty", "{{.value}}", "Body", "", "subject is required", map[string]string{"value": ""}, false},
			{"subject_empty_identity", "{{.employee_name}}", "Body", "", "subject is required", nil, true},
			{"text_omitted", "Subject", "{{.value}}", "", "text_body or html_body required", nil, false},
			{"html_omitted", "Subject", "", "{{.value}}", "text_body or html_body required", nil, false},
			{"both_bodies_omitted", "Subject", "{{.value}}", "{{.value}}", "text_body or html_body required", nil, false},
			{"html_sanitized_away", "Subject", "", "<script>alert(1)</script>", "text_body or html_body required", nil, false},
			{"subject_and_body_omitted", "{{.value}}", "{{.value}}", "", "subject is required", nil, false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := newPreviewBoundaryFixture(published)
				draft := r5RequiredContentDraft(tc.subject, tc.text, tc.html)
				r5SetPreviewContent(f, draft, tc.vars)
				if tc.blankEmployee {
					f.repo.user.DisplayName, f.repo.employee = "", ""
				}
				// The generic renderer intentionally supports omission. Enforce
				// this requirement at the application preview/send boundaries.
				subject, text, html, err := company.Render(draft, tc.vars, f.repo.employee, f.repo.company, f.repo.mailbox.Mailbox.FullAddress)
				if err != nil {
					t.Fatalf("generic Render lost optional omission: %v", err)
				}
				if tc.message == "subject is required" && subject != "" || tc.message == "text_body or html_body required" && (text != "" || html != "") {
					t.Fatalf("fixture did not produce final empty content: %q %q %q", subject, text, html)
				}

				// Run the shipping submission method against the same published
				// snapshot and variable values. Its existing rejection is the oracle;
				// no queue, PostgreSQL transaction or delivery is simulated here.
				st := testutil.NewFakeStore()
				send := outbound.NewService(config.Outbound{Enabled: true}, st, f.repo, zerolog.Nop())
				job, replayed, sendErr := send.SubmitWithReplay(context.Background(), outbound.SendRequest{
					TenantID: f.actor.TenantID, UserID: &f.actor.ID,
					SenderMailboxID: &f.repo.mailboxID, TemplateVersionID: &f.repo.versionID,
					From: f.repo.mailbox.Mailbox.FullAddress, To: []string{"recipient@example.test"}, TemplateVars: tc.vars,
				})
				sendProblem, ok := app.As(sendErr)
				if !ok || sendProblem.Kind != app.KindBadRequest || sendProblem.Message != tc.message || job != nil || replayed {
					t.Fatalf("submission rejection changed: job=%+v replay=%t error=%v", job, replayed, sendErr)
				}
				_, count, err := st.ListOutboundJobsScoped(context.Background(), authz.OwnerListFilter{TenantID: f.actor.TenantID, AllInTenant: true}, models.Page{Page: 1, PerPage: 10})
				if err != nil || count != 0 {
					t.Fatalf("invalid rendered content queued jobs=%d error=%v", count, err)
				}
				got, previewErr := f.svc.Preview(context.Background(), f.actor, f.input)
				previewProblem, ok := app.As(previewErr)
				if !ok || previewProblem.Kind != sendProblem.Kind || previewProblem.Message != sendProblem.Message || got != nil {
					t.Fatalf("preview accepted content submission rejects: render=%+v error=%v; submission=%v", got, previewErr, sendErr)
				}
			})
		}
	}
}

func TestR5PreviewRequiredContentPreservesUsableOutput(t *testing.T) {
	for _, published := range []bool{false, true} {
		mode := map[bool]string{false: "management", true: "published"}[published]
		for _, tc := range []struct {
			name, subject, text, html string
			vars                      map[string]string
			want                      Rendered
		}{
			{"text_only", "Subject", "Hello {{.value}}", "", map[string]string{"value": "世界"}, Rendered{"Subject", "Hello 世界", ""}},
			{"html_only", "Subject", "", "<p>{{.value}}</p>", map[string]string{"value": "世界 & <b>text</b>"}, Rendered{"Subject", "", "<p>世界 &amp; &lt;b&gt;text&lt;/b&gt;</p>"}},
			{"optional_text_with_html", "Subject", "{{.value}}", "<p>Fallback</p>", nil, Rendered{"Subject", "", "<p>Fallback</p>"}},
			{"optional_html_with_text", "Subject", "Fallback", "{{.value}}", nil, Rendered{"Subject", "Fallback", ""}},
			{"whitespace_subject", " \t\u3000", "Body", "", nil, Rendered{" \t\u3000", "Body", ""}},
			{"whitespace_text", "Subject", " \t\n", "", nil, Rendered{"Subject", " \t\n", ""}},
			{"sanitized_html_with_text", "Subject", "Fallback", "<script>alert(1)</script>", nil, Rendered{"Subject", "Fallback", ""}},
			{"server_identity", "Hello {{.employee_name}}", "{{.company_name}} / {{.sender_address}}", "", nil, Rendered{"Hello Employee", "Company / sender@company.test", ""}},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := newPreviewBoundaryFixture(published)
				r5SetPreviewContent(f, r5RequiredContentDraft(tc.subject, tc.text, tc.html), tc.vars)
				got, err := f.svc.Preview(context.Background(), f.actor, f.input)
				if err != nil || !reflect.DeepEqual(got, &tc.want) {
					t.Fatalf("usable preview changed: got=%+v want=%+v error=%v", got, tc.want, err)
				}
				wantMailboxReads := 3
				if published {
					wantMailboxReads = 2
					if f.repo.counts["version"] != 2 {
						t.Fatal("published preview omitted its current version/grant recheck")
					}
				} else if f.repo.counts["user"] != 2 || f.repo.counts["settings"] != 2 {
					t.Fatal("management preview omitted its current identity/settings recheck")
				}
				if f.repo.counts["mailbox"] != wantMailboxReads {
					t.Fatal("usable preview omitted its final authority checks")
				}
				// Submit the returned content through the actual free-form path.
				// This proves single-format bodies and whitespace retain the same
				// content rules; the backing queue here is explicitly in-memory.
				r5SubmitPreviewContent(t, f, *got)
			})
		}
	}
}

func r5SubmitPreviewContent(t *testing.T, f *previewBoundaryFixture, rendered Rendered) {
	t.Helper()
	ctx := context.Background()
	st := testutil.NewFakeStore()
	st.SeedTenant(&models.Tenant{ID: f.actor.TenantID})
	u := *f.repo.user
	u.Role = models.RoleUser
	if err := st.CreateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	zone := models.DomainZone{ID: uuid.New(), TenantID: f.actor.TenantID, Domain: "company.test", IsVerified: true, MXVerified: true}
	st.SeedZone(&zone)
	mailbox := f.repo.mailbox.Mailbox
	mailbox.ZoneID, mailbox.OwnerUserID = zone.ID, &f.actor.ID
	st.SeedMailbox(&mailbox)
	send := outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	job, replayed, err := send.SubmitWithReplay(ctx, outbound.SendRequest{
		TenantID: f.actor.TenantID, UserID: &f.actor.ID, ZoneID: zone.ID, SenderMailboxID: &mailbox.ID,
		From: mailbox.FullAddress, To: []string{"recipient@example.test"},
		Subject: rendered.Subject, TextBody: rendered.TextBody, HTMLBody: rendered.HTMLBody,
	})
	if err != nil || replayed || job == nil {
		t.Fatalf("usable preview cannot enter normal submission: job=%+v replay=%t error=%v", job, replayed, err)
	}
	stored, err := st.GetOutboundJob(ctx, job.ID)
	if err != nil || stored == nil || stored.Subject != rendered.Subject || stored.TextBody != rendered.TextBody || stored.HTMLBody != rendered.HTMLBody {
		t.Fatalf("submission changed rendered bytes: job=%+v error=%v", stored, err)
	}
}
