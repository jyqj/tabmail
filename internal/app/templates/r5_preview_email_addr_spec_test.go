package templates

import (
	"context"
	"html"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type advanceTemplateEmailStore struct {
	*testutil.FakeStore
	governance *previewBoundaryRepo
}

type advanceTemplateEmailEnqueueReader struct {
	store.OutboundRetryReader
	governance *previewBoundaryRepo
}

func (r advanceTemplateEmailEnqueueReader) TemplateForSend(ctx context.Context, tenant uuid.UUID, user, key *uuid.UUID, mailbox, version uuid.UUID) (*company.TemplateVersion, string, string, error) {
	return r.governance.TemplateForSend(ctx, tenant, user, key, mailbox, version)
}

func (s *advanceTemplateEmailStore) CreateOutboundJobAuthorized(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation, draft *store.DraftConsumption, validate store.OutboundEnqueueValidator) (bool, error) {
	// Keep the fake's complete mutex-protected enqueue validator; supply only
	// this fixture's published snapshot in place of its default deny stub.
	return s.FakeStore.CreateOutboundJobAuthorized(ctx, job, quota, draft, func(ctx context.Context, reader store.OutboundRetryReader, job *models.OutboundJob, quota store.OutboundQuotaReservation) (store.OutboundQuotaReservation, error) {
		return validate(ctx, advanceTemplateEmailEnqueueReader{reader, s.governance}, job, quota)
	})
}

// The shipping Preview and template SubmitWithReplay paths consume the same
// published snapshot and values. Persistence is the existing in-memory store;
// these tests do not claim PostgreSQL authorization or SMTP delivery.
func TestAdvanceTemplateEmailPreviewMatchesPublishedSubmission(t *testing.T) {
	for _, published := range []bool{false, true} {
		mode := "management"
		if published {
			mode = "published"
		}
		for _, tc := range []struct {
			name, value string
			valid       bool
		}{
			{"ordinary", "contact@example.test", true},
			{"quoted_comma", `"ops,team"@example.test`, true},
			{"quoted_at", `"ops@team"@example.test`, true},
			{"quoted_html", `"<script>"@example.test`, true},
			{"display_name", "Contact <contact@example.test>", false},
			{"domain_space", "contact@ example.test", false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := newPreviewBoundaryFixture(published)
				draft := company.TemplateDraft{
					Subject: "Contact {{.contact}}", TextBody: "{{.contact}}", HTMLBody: "<p>{{.contact}}</p>",
					Variables: []company.Variable{{Name: "contact", Type: "email", Required: true, MaxLength: 100}},
				}
				vars := map[string]string{"contact": tc.value}
				r5SetPreviewContent(f, draft, vars)
				got, previewErr := f.svc.Preview(context.Background(), f.actor, f.input)
				if tc.valid {
					if previewErr != nil || got == nil || got.Subject != "Contact "+tc.value || got.TextBody != tc.value || got.HTMLBody != "<p>"+html.EscapeString(tc.value)+"</p>" {
						t.Errorf("valid mailbox variable preview failed: got=%+v err=%v", got, previewErr)
					}
				} else if problem, ok := app.As(previewErr); !ok || problem.Kind != app.KindBadRequest || got != nil {
					t.Errorf("invalid mailbox preview released output: got=%+v err=%v", got, previewErr)
				}

				st := &advanceTemplateEmailStore{testutil.NewFakeStore(), f.repo}
				st.SeedTenant(&models.Tenant{ID: f.actor.TenantID})
				u := *f.repo.user
				u.Role = models.RoleUser
				if err := st.CreateUser(t.Context(), &u); err != nil {
					t.Fatal(err)
				}
				zone := models.DomainZone{ID: uuid.New(), TenantID: f.actor.TenantID, Domain: "company.test", IsVerified: true, MXVerified: true}
				st.SeedZone(&zone)
				mailbox := f.repo.mailbox.Mailbox
				mailbox.ZoneID, mailbox.OwnerUserID = zone.ID, &f.actor.ID
				st.SeedMailbox(&mailbox)
				send := outbound.NewService(config.Outbound{Enabled: true}, st, f.repo, zerolog.Nop())
				job, replayed, sendErr := send.SubmitWithReplay(t.Context(), outbound.SendRequest{
					TenantID: f.actor.TenantID, UserID: &f.actor.ID, ZoneID: zone.ID,
					SenderMailboxID: &mailbox.ID, TemplateVersionID: &f.repo.versionID,
					From: mailbox.FullAddress, To: []string{"recipient@example.test"}, TemplateVars: vars,
				})
				_, count, countErr := st.ListOutboundJobsScoped(t.Context(), authz.OwnerListFilter{TenantID: f.actor.TenantID, AllInTenant: true}, models.Page{Page: 1, PerPage: 10})
				if countErr != nil {
					t.Fatal(countErr)
				}
				if !tc.valid {
					if problem, ok := app.As(sendErr); !ok || problem.Kind != app.KindBadRequest || job != nil || replayed || count != 0 {
						t.Fatalf("invalid email template reached queue: job=%+v err=%v count=%d", job, sendErr, count)
					}
					return
				}
				if sendErr != nil || replayed || job == nil || count != 1 {
					t.Fatalf("valid email template could not queue: job=%+v err=%v count=%d", job, sendErr, count)
				}
				stored, err := st.GetOutboundJob(t.Context(), job.ID)
				if err != nil || stored == nil || got == nil || stored.Subject != got.Subject || stored.TextBody != got.TextBody || stored.HTMLBody != got.HTMLBody || stored.TemplateVersionID == nil || *stored.TemplateVersionID != f.repo.versionID {
					t.Fatalf("published submission lost preview bytes or provenance: job=%+v preview=%+v err=%v", stored, got, err)
				}
			})
		}
	}
}
