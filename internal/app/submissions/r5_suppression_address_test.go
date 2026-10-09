package submissions

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type r5SuppressionAddressStore struct {
	*testutil.FakeStore
	lookups      []string
	enqueueCalls int
	failAddress  string
	lookupError  error
}

func (s *r5SuppressionAddressStore) IsSuppressed(ctx context.Context, tenant uuid.UUID, address string) (bool, error) {
	s.lookups = append(s.lookups, address)
	if s.lookupError != nil && (s.failAddress == "" || address == s.failAddress) {
		return false, s.lookupError
	}
	return s.FakeStore.IsSuppressed(ctx, tenant, address)
}

func (s *r5SuppressionAddressStore) CreateOutboundJobAuthorized(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation, draft *store.DraftConsumption, validate store.OutboundEnqueueValidator) (bool, error) {
	s.enqueueCalls++
	return s.FakeStore.CreateOutboundJobAuthorized(ctx, job, quota, draft, validate)
}

// Every fixture starts without a previous command. Expose the fresh-command
// lookup so a draft reaches the real enqueue boundary on the unfixed source.
func (s *r5SuppressionAddressStore) FindOutboundSubmission(context.Context, uuid.UUID, string, string, string) (*models.OutboundJob, error) {
	return nil, nil
}

func r5SuppressionAddressFixture(t *testing.T) (submissionFixture, *r5SuppressionAddressStore) {
	t.Helper()
	f := newSubmissionFixture(t)
	st := &r5SuppressionAddressStore{FakeStore: f.st}
	out := outbound.NewService(config.Outbound{Enabled: true}, st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	f.svc = NewService(nil, st, out, zerolog.Nop())
	return f, st
}

func r5SuppressionAddressInput(f submissionFixture, role int, address string) (SubmitInput, []string) {
	backing := []string{"to@example.test", "unused capacity", "copy@example.test", "hidden@example.test"}
	backing[[]int{0, 2, 3}[role]] = address
	return SubmitInput{
		From: f.mb.FullAddress, To: backing[:1], CC: backing[2:3], BCC: backing[3:4],
		Subject: "hello", TextBody: "body",
	}, backing
}

func r5RequireNoSubmissionEnqueue(t *testing.T, f submissionFixture, st *r5SuppressionAddressStore) {
	t.Helper()
	// This is the atomic job/quota/draft-consumption boundary. The fake does
	// not implement draft deletion; absence of a call is the application claim.
	if st.enqueueCalls != 0 {
		t.Errorf("rejected submission reached atomic enqueue %d times", st.enqueueCalls)
	}
	count, err := f.st.CountOutboundSince(context.Background(), f.tenant.ID, &f.owner.ID, time.Time{})
	if err != nil || count != 0 {
		t.Errorf("rejected submission persisted %d jobs: %v", count, err)
	}
}

func TestR5SubmissionSuppressionCanonicalAddressRejected(t *testing.T) {
	spellings := []struct{ name, address string }{
		{"bare_control", "blocked@example.test"},
		{"display_name", "Blocked Person <Blocked@Example.test>"},
		{"quoted_display_name", `"Blocked, Person" <blocked@example.test>`},
		{"surrounding_whitespace", " \tblocked@example.test\t "},
	}
	for role, name := range []string{"to", "cc", "bcc"} {
		for _, spelling := range spellings {
			t.Run(name+"/"+spelling.name, func(t *testing.T) {
				f, st := r5SuppressionAddressFixture(t)
				if err := st.AddSuppression(context.Background(), &models.SuppressionEntry{
					TenantID: f.tenant.ID, Address: "blocked@example.test", Reason: "hard_bounce",
				}); err != nil {
					t.Fatal(err)
				}
				in, backing := r5SuppressionAddressInput(f, role, spelling.address)
				before := append([]string(nil), backing...)
				job, replay, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), in)
				if job != nil || replay || failure == nil || failure.Kind != FailureBadRequest || !strings.Contains(failure.Message, "is suppressed") {
					t.Errorf("suppressed recipient was not rejected synchronously: job=%v replay=%v failure=%+v", job != nil, replay, failure)
				}
				want := []string{"to@example.test", "copy@example.test", "hidden@example.test"}
				want[role] = "blocked@example.test"
				if !reflect.DeepEqual(st.lookups, want[:role+1]) {
					t.Errorf("suppression addresses = %q, want %q", st.lookups, want[:role+1])
				}
				if !reflect.DeepEqual(backing, before) {
					t.Errorf("caller recipient slices changed: got %q want %q", backing, before)
				}
				r5RequireNoSubmissionEnqueue(t, f, st)
			})
		}
	}
}

func TestR5SubmissionSuppressionCanonicalLookupFailure(t *testing.T) {
	for role, name := range []string{"to", "cc", "bcc"} {
		t.Run(name, func(t *testing.T) {
			f, st := r5SuppressionAddressFixture(t)
			st.failAddress = "blocked@example.test"
			st.lookupError = errors.New("controlled canonical suppression lookup failure")
			in, backing := r5SuppressionAddressInput(f, role, "Blocked <Blocked@Example.test>")
			before := append([]string(nil), backing...)
			job, replay, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), in)
			if job != nil || replay || failure == nil || failure.Kind != FailureInternal {
				t.Errorf("lookup failure = (job=%v replay=%v failure=%+v), want internal failure", job != nil, replay, failure)
			}
			if !reflect.DeepEqual(backing, before) {
				t.Errorf("failed lookup changed caller recipients: got %q want %q", backing, before)
			}
			r5RequireNoSubmissionEnqueue(t, f, st)
		})
	}
}

func TestR5SubmissionSuppressionRejectsMalformedInputBeforeLookup(t *testing.T) {
	for role, name := range []string{"to", "cc", "bcc"} {
		t.Run(name, func(t *testing.T) {
			f, st := r5SuppressionAddressFixture(t)
			st.lookupError = errors.New("suppression storage is unavailable")
			in, backing := r5SuppressionAddressInput(f, role, "not an address")
			before := append([]string(nil), backing...)
			job, replay, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), in)
			if job != nil || replay || failure == nil || failure.Kind != FailureBadRequest {
				t.Errorf("malformed recipient = (job=%v replay=%v failure=%+v), want bad request", job != nil, replay, failure)
			}
			if len(st.lookups) != 0 {
				t.Errorf("malformed recipient consulted suppression storage: %q", st.lookups)
			}
			if !reflect.DeepEqual(backing, before) {
				t.Errorf("malformed recipient changed caller slices: got %q want %q", backing, before)
			}
			r5RequireNoSubmissionEnqueue(t, f, st)
		})
	}
}

func TestR5SubmissionSuppressionAllowedRecipientsKeepRoles(t *testing.T) {
	for _, foreignSuppression := range []bool{false, true} {
		name := "unsuppressed"
		if foreignSuppression {
			name = "other_tenant_suppression"
		}
		t.Run(name, func(t *testing.T) {
			f, st := r5SuppressionAddressFixture(t)
			if foreignSuppression {
				if err := st.AddSuppression(context.Background(), &models.SuppressionEntry{
					TenantID: uuid.New(), Address: "to@example.test", Reason: "hard_bounce",
				}); err != nil {
					t.Fatal(err)
				}
			}
			backing := []string{"To Person <To@Example.test>", "unused capacity", `"Copy, Person" <copy@example.test>`, " \thidden@example.test\t "}
			before := append([]string(nil), backing...)
			job, replay, failure := f.svc.SubmitAuthorized(context.Background(), f.tenant, userActor(f.owner), SubmitInput{
				From: f.mb.FullAddress, To: backing[:1], CC: backing[2:3], BCC: backing[3:4], Subject: "hello", TextBody: "body",
			})
			if job == nil || replay || failure != nil {
				t.Fatalf("allowed recipients = (job=%v replay=%v failure=%+v)", job != nil, replay, failure)
			}
			if st.enqueueCalls != 1 {
				t.Errorf("allowed submission reached atomic enqueue %d times", st.enqueueCalls)
			}
			persisted, err := f.st.GetOutboundJob(context.Background(), job.ID)
			if err != nil || persisted == nil {
				t.Fatalf("persisted job = (%v, %v)", persisted, err)
			}
			if !reflect.DeepEqual(persisted.To, []string{"to@example.test"}) || !reflect.DeepEqual(persisted.CC, []string{"copy@example.test"}) || !reflect.DeepEqual(persisted.BCC, []string{"hidden@example.test"}) || !reflect.DeepEqual(persisted.RcptTo, []string{"to@example.test", "copy@example.test", "hidden@example.test"}) {
				t.Errorf("allowed recipient roles changed: To=%q Cc=%q Bcc=%q envelope=%q", persisted.To, persisted.CC, persisted.BCC, persisted.RcptTo)
			}
			if !reflect.DeepEqual(backing, before) {
				t.Errorf("allowed submission changed caller slices: got %q want %q", backing, before)
			}
		})
	}
}

func TestR5SubmissionSuppressionDraftRejectsBeforeConsumption(t *testing.T) {
	f, st := r5SuppressionAddressFixture(t)
	if err := st.AddSuppression(context.Background(), &models.SuppressionEntry{
		TenantID: f.tenant.ID, Address: "blocked@example.test", Reason: "hard_bounce",
	}); err != nil {
		t.Fatal(err)
	}
	draft := &company.Draft{ID: uuid.New(), MailboxID: f.mb.ID, Revision: 3,
		Payload: company.DraftPayload{To: []string{"Blocked Person <Blocked@Example.test>"}, Subject: "hello", TextBody: "body"},
	}
	f.svc.repo = draftRepoStub{draft: draft}
	job, replay, failure := f.svc.SubmitDraft(context.Background(), f.tenant, userActor(f.owner), draft.ID, draft.Revision, "suppressed-draft")
	if job != nil || replay || failure == nil || failure.Kind != FailureBadRequest || !strings.Contains(failure.Message, "is suppressed") {
		t.Errorf("suppressed draft was not rejected before consumption: job=%v replay=%v failure=%+v", job != nil, replay, failure)
	}
	r5RequireNoSubmissionEnqueue(t, f, st)
	if draft.Revision != 3 || !reflect.DeepEqual(draft.Payload.To, []string{"Blocked Person <Blocked@Example.test>"}) {
		t.Errorf("rejected draft input changed: %+v", draft)
	}
}
