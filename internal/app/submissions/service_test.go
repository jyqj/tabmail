package submissions

import (
	"context"
	"errors"
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

type submissionFixture struct {
	st     *testutil.FakeStore
	svc    *Service
	tenant *models.Tenant
	owner  *models.User
	other  *models.User
	zone   *models.DomainZone
	mb     *models.Mailbox
}

func newSubmissionFixture(t *testing.T) submissionFixture {
	t.Helper()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant-sub"}
	st.SeedTenant(tenant)
	owner := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "owner@example.test", Role: models.RoleUser, IsActive: true}
	other := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "other@example.test", Role: models.RoleUser, IsActive: true}
	for _, u := range []*models.User{owner, other} {
		if err := st.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "send.test", IsVerified: true, MXVerified: true}
	st.SeedZone(zone)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant.ID, ZoneID: zone.ID, FullAddress: "owner@send.test", LocalPart: "owner", ResolvedDomain: zone.Domain, OwnerUserID: &owner.ID, AccessMode: models.AccessAPIKey}
	st.SeedMailbox(mb)
	out := outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	return submissionFixture{st: st, svc: NewService(nil, st, out, zerolog.Nop()), tenant: tenant, owner: owner, other: other, zone: zone, mb: mb}
}

func userActor(u *models.User) authz.Actor {
	return authz.Actor{
		Type:       authz.PrincipalUser,
		ID:         u.ID,
		TenantID:   u.TenantID,
		Role:       u.Role,
		Permission: &models.EffectivePermission{CanSend: true, DailySendQuota: 100},
	}
}

func TestSubmitAuthorizedEnqueuesJobForMailboxOwner(t *testing.T) {
	f := newSubmissionFixture(t)
	job, replayed, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{
		From:     f.mb.FullAddress,
		To:       []string{"Dest@Example.test"},
		Subject:  "hello",
		TextBody: "body",
	})
	if failure != nil {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if replayed {
		t.Fatal("fresh submission must not be reported as replay")
	}
	if job == nil || job.State != models.OutboundPending {
		t.Fatalf("expected pending job, got %+v", job)
	}
	if job.SenderMailboxID == nil || *job.SenderMailboxID != f.mb.ID {
		t.Fatalf("expected sender mailbox attribution, got %+v", job.SenderMailboxID)
	}
	if job.SenderUserID == nil || *job.SenderUserID != f.owner.ID {
		t.Fatalf("expected sender user attribution, got %+v", job.SenderUserID)
	}
	if _, err := f.st.GetOutboundJob(context.Background(), job.ID); err != nil {
		t.Fatalf("job not persisted: %v", err)
	}
}

func TestSubmitAuthorizedClassifiesFailures(t *testing.T) {
	f := newSubmissionFixture(t)
	if _, _, failure := f.svc.SubmitAuthorized(context.Background(), nil, userActor(f.owner), SubmitInput{From: f.mb.FullAddress, To: []string{"a@b.test"}, Subject: "s", TextBody: "b"}); failure == nil || failure.Kind != FailureAuthRequired {
		t.Fatalf("missing tenant: expected auth-required failure, got %+v", failure)
	}
	if _, _, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{From: "x@unknown.test", To: []string{"a@b.test"}, Subject: "s", TextBody: "b"}); failure == nil || failure.Kind != FailureBadRequest || failure.Message != "from domain is not registered" {
		t.Fatalf("unknown domain: expected 400 not registered, got %+v", failure)
	}
	if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{ID: uuid.New(), TenantID: f.tenant.ID, Address: "blocked@example.test", Reason: "hard_bounce"}); err != nil {
		t.Fatal(err)
	}
	if _, _, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{From: f.mb.FullAddress, To: []string{"blocked@example.test"}, Subject: "s", TextBody: "b"}); failure == nil || failure.Kind != FailureBadRequest || failure.Message != "recipient blocked@example.test is suppressed (hard bounce); remove from suppression list to retry" {
		t.Fatalf("suppressed recipient: got %+v", failure)
	}
}

func TestSubmitDraftRevisionConflictCarriesCurrentRevision(t *testing.T) {
	f := newSubmissionFixture(t)
	draftID := uuid.New()
	f.svc.repo = draftRepoStub{draft: &company.Draft{ID: draftID, MailboxID: f.mb.ID, Revision: 5}}
	_, _, failure := f.svc.SubmitDraft(context.Background(), f.tenant, userActor(f.owner), draftID, 4, "key-1")
	if failure == nil || failure.Kind != FailureRevisionConflict || failure.Revision != 5 {
		t.Fatalf("expected revision conflict carrying revision 5, got %+v", failure)
	}
}

func TestSubmitDraftConflictsWhenMailboxDisappeared(t *testing.T) {
	f := newSubmissionFixture(t)
	draftID := uuid.New()
	f.svc.repo = draftRepoStub{draft: &company.Draft{ID: draftID, MailboxID: uuid.New(), Revision: 3}}
	_, _, failure := f.svc.SubmitDraft(context.Background(), f.tenant, userActor(f.owner), draftID, 3, "key-1")
	if failure == nil || failure.Kind != FailureConflict || failure.Message != "draft mailbox is no longer available" {
		t.Fatalf("expected mailbox-gone conflict, got %+v", failure)
	}
}

func TestSubmitDraftWithoutOutboundIsDisabled(t *testing.T) {
	f := newSubmissionFixture(t)
	disabled := NewService(nil, f.st, nil, zerolog.Nop())
	_, _, failure := disabled.SubmitDraft(context.Background(), f.tenant, userActor(f.owner), uuid.New(), 1, "key-1")
	if failure == nil || failure.Kind != FailurePlain || failure.Message != "outbound sending is disabled" {
		t.Fatalf("expected disabled failure, got %+v", failure)
	}
	job, replay, err := disabled.ConsumedDraftSubmission(context.Background(), f.tenant.ID, uuid.New(), nil, "key")
	if job != nil || replay || err != nil {
		t.Fatal("consumed lookup must be a no-op triple when outbound is disabled")
	}
}

// draftRepoStub stands in for the company repository; only GetMailDraft is
// exercised by these tests.
type draftRepoStub struct {
	company.Repository
	draft *company.Draft
}

func (r draftRepoStub) GetMailDraft(context.Context, authz.Actor, uuid.UUID) (*company.Draft, error) {
	return r.draft, nil
}

func TestConsumedDraftSubmissionWithoutCapabilityYieldsNothing(t *testing.T) {
	f := newSubmissionFixture(t)
	job, replay, err := f.svc.ConsumedDraftSubmission(context.Background(), f.tenant.ID, uuid.New(), &f.owner.ID, "key-1")
	if job != nil || replay || err != nil {
		t.Fatalf("expected (nil,false,nil) on a store without draft lookup, got (%v,%v,%v)", job, replay, err)
	}
}

func TestAccessibleOutboundJobVisibility(t *testing.T) {
	f := newSubmissionFixture(t)
	jobID := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: jobID, TenantID: f.tenant.ID, ZoneID: f.zone.ID, MailFrom: f.mb.FullAddress,
		UserID: &f.owner.ID, SenderUserID: &f.owner.ID, SenderMailboxID: &f.mb.ID,
		State: models.OutboundSent,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.AccessibleOutboundJob(context.Background(), nil, userActor(f.owner), jobID); !errors.Is(err, ErrOutboundJobAuthRequired) {
		t.Fatalf("expected auth-required sentinel, got %v", err)
	}
	job, err := f.svc.AccessibleOutboundJob(context.Background(), f.tenant, userActor(f.owner), jobID)
	if err != nil || job == nil || job.ID != jobID {
		t.Fatalf("owner should see the job, got job=%v err=%v", job, err)
	}
	if _, err := f.svc.AccessibleOutboundJob(context.Background(), f.tenant, userActor(f.other), jobID); !errors.Is(err, ErrOutboundJobNotFound) {
		t.Fatalf("unrelated tenant user should get not-found, got %v", err)
	}
	restricted := userActor(f.owner)
	restricted.Permission = &models.EffectivePermission{CanSend: true, AllowedZoneIDs: []uuid.UUID{uuid.New()}}
	if _, err := f.svc.AccessibleOutboundJob(context.Background(), f.tenant, restricted, jobID); !errors.Is(err, ErrOutboundJobNotFound) {
		t.Fatalf("zone-restricted owner should get not-found, got %v", err)
	}
}

func TestRedactOutboundJobByAuthority(t *testing.T) {
	f := newSubmissionFixture(t)
	job := &models.OutboundJob{
		ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, MailFrom: f.mb.FullAddress,
		UserID: &f.owner.ID, SenderUserID: &f.owner.ID, SenderMailboxID: &f.mb.ID,
		State: models.OutboundSent, Subject: "s", TextBody: "secret", HTMLBody: "<b>secret</b>",
		To: []string{"to@example.test"}, CC: []string{"cc@example.test"}, BCC: []string{"bcc@example.test"},
		RcptTo:        []string{"to@example.test", "cc@example.test", "bcc@example.test"},
		DeliveryToken: ptrUUID(uuid.New()), LastError: "542 policy", SMTPResponse: "550 bounce",
		InFlightDomain: "pending.test",
	}

	ownerView, err := f.svc.RedactOutboundJob(context.Background(), userActor(f.owner), job)
	if err != nil {
		t.Fatal(err)
	}
	if ownerView.ContentRedacted {
		t.Fatal("owner content must not be redacted")
	}
	if ownerView.TextBody != "secret" || len(ownerView.BCC) != 1 {
		t.Fatalf("owner view lost content: %+v", ownerView)
	}
	if ownerView.DeliveryToken != nil || ownerView.RawMIME != nil {
		t.Fatal("delivery token and raw MIME must always be stripped")
	}
	if !ownerView.DeliveryUncertain {
		t.Fatal("in-flight domain with non-processing state must surface delivery uncertainty")
	}

	otherView, err := f.svc.RedactOutboundJob(context.Background(), userActor(f.other), job)
	if err != nil {
		t.Fatal(err)
	}
	if !otherView.ContentRedacted {
		t.Fatal("unrelated user content must be redacted")
	}
	if otherView.TextBody != "" || otherView.HTMLBody != "" || otherView.BCC != nil || otherView.AttachmentIDs != nil {
		t.Fatalf("redacted view leaked content: %+v", otherView)
	}
	if len(otherView.RcptTo) != 2 || otherView.RcptTo[0] != "to@example.test" || otherView.RcptTo[1] != "cc@example.test" {
		t.Fatalf("redacted RcptTo must be To+CC, got %v", otherView.RcptTo)
	}
	if otherView.LastError != "Delivery details restricted; inspect status and SMTP code" || otherView.SMTPResponse != "Protocol response restricted" {
		t.Fatalf("redacted protocol details wrong: %q / %q", otherView.LastError, otherView.SMTPResponse)
	}
	if otherView.InFlightDomain != "" {
		t.Fatal("redacted view must hide in-flight domain")
	}
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
