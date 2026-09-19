package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// capabilitiesSubmissionStub is a minimal mailWorkspace: only GetSubmission
// answers; every other surface is out of scope for these tests.
type capabilitiesSubmissionStub struct{}

func (s *capabilitiesSubmissionStub) GetSubmission(context.Context, authz.Actor, uuid.UUID) (*company.Submission, error) {
	return &company.Submission{Status: company.SubmissionNeedsAttention}, nil
}
func (s *capabilitiesSubmissionStub) ListSubmissions(context.Context, authz.Actor, models.Page) ([]company.Submission, int, error) {
	return nil, 0, nil
}
func (s *capabilitiesSubmissionStub) ListMailDrafts(context.Context, authz.Actor) ([]company.Draft, error) {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) GetMailDraft(context.Context, authz.Actor, uuid.UUID) (*company.Draft, error) {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) SaveMailDraft(context.Context, authz.Actor, company.Draft) (*company.Draft, error) {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) DeleteMailDraft(context.Context, authz.Actor, uuid.UUID, int) error {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) GetWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*models.Message, error) {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) ListWorkMessages(context.Context, authz.Actor, uuid.UUID, string, string, models.Page) ([]*models.Message, int, error) {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) MutateWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID, string) error {
	panic("not used in capabilities tests")
}
func (s *capabilitiesSubmissionStub) ListMailboxEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]company.MailEvent, int64, error) {
	panic("not used in capabilities tests")
}

// notRetryableStore forces the store-side not-safely-retryable conflict that
// the FakeStore cannot reach through handler-visible state alone.
type notRetryableStore struct{ *testutil.FakeStore }

func (s *notRetryableStore) RequeueOutboundJob(context.Context, uuid.UUID) error {
	return store.ErrOutboundNotRetryable
}

func newSubmissionCapabilitiesHandler(t *testing.T, f outboundAccessFixture) *CompanyMailHandler {
	t.Helper()
	outSvc := outbound.NewService(config.Outbound{Enabled: true}, f.st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	svc := submissions.NewService(nil, f.st, outSvc, zerolog.Nop())
	return NewCompanyMailHandler(&capabilitiesSubmissionStub{}, nil, svc, zerolog.Nop())
}

func fetchSubmissionCapabilities(t *testing.T, h *CompanyMailHandler, f outboundAccessFixture, jobID uuid.UUID, user *models.User) company.SubmissionCapabilities {
	t.Helper()
	rr := doOutboundHandlerRequest(t, f.st, h.Submission, http.MethodGet, "/api/v1/company/submissions/"+jobID.String(), map[string]string{"id": jobID.String()}, outboundUserHeaders(t, user))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected receipt 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data struct {
			Capabilities *company.SubmissionCapabilities `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Capabilities == nil {
		t.Fatalf("capabilities block missing in %s", rr.Body.String())
	}
	return *body.Data.Capabilities
}

func TestSubmissionReceiptCapabilities(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := newSubmissionCapabilitiesHandler(t, f)
	zone := &models.DomainZone{ID: uuid.New(), TenantID: f.tenantID, Domain: "caps.test", IsVerified: true, MXVerified: true}
	f.st.SeedZone(zone)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, FullAddress: "caps@caps.test", OwnerUserID: &f.userA.ID}
	f.st.SeedMailbox(mb)
	// userB holds a current read grant but no send right on the sender mailbox.
	if err := f.st.SetMailboxGrant(context.Background(), &models.MailboxGrant{
		TenantID: f.tenantID, MailboxID: mb.ID, UserID: f.userB.ID, CanRead: true,
	}); err != nil {
		t.Fatal(err)
	}

	readableJob := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: readableJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		SenderMailboxID: &mb.ID, ZoneID: zone.ID, State: models.OutboundDead, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	ownJob := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: ownJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		ZoneID: zone.ID, State: models.OutboundFailed, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	uncertainJob := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: uncertainJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		ZoneID: zone.ID, State: models.OutboundDead, InFlightDomain: "smtp.example.test", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	sentJob := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: sentJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		ZoneID: zone.ID, State: models.OutboundSent, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		job  uuid.UUID
		user *models.User
		want company.SubmissionCapabilities
	}{
		{"read grant without send authority", readableJob, f.userB, company.SubmissionCapabilities{ViewContent: true, RetryBlockReason: submissions.CapabilitySenderAuthority}},
		{"send authority without content right", ownJob, f.userA, company.SubmissionCapabilities{Retry: true}},
		{"delivery uncertain", uncertainJob, f.userA, company.SubmissionCapabilities{RetryBlockReason: submissions.CapabilityDeliveryUncertain}},
		{"state not retryable", sentJob, f.userA, company.SubmissionCapabilities{RetryBlockReason: submissions.CapabilityStateNotRetryable}},
	} {
		if got := fetchSubmissionCapabilities(t, h, f, tc.job, tc.user); got != tc.want {
			t.Fatalf("%s: capabilities = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func conflictReason(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error *struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == nil || body.Error.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT error, got %s", rr.Body.String())
	}
	return body.Error.Reason
}

func TestRetryJobConflictReasonMapping(t *testing.T) {
	f := newOutboundAccessFixture(t)
	outSvc := outbound.NewService(config.Outbound{Enabled: true}, f.st, testutil.DeniedTemplateGovernance{}, zerolog.Nop())
	h := NewOutboundHandler(outSvc, f.st, zerolog.Nop())

	uncertainJob := uuid.New()
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: uncertainJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		State: models.OutboundDead, InFlightDomain: "smtp.example.test", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	rr := doOutboundHandlerRequest(t, f.st, h.RetryJob, http.MethodPost, "/api/v1/outbound/"+uncertainJob.String()+"/retry", map[string]string{"id": uncertainJob.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusConflict {
		t.Fatalf("uncertain retry expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	if reason := conflictReason(t, rr); reason != "delivery_uncertain" {
		t.Fatalf("uncertain retry reason = %q, want delivery_uncertain", reason)
	}

	stateJob := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: f.tenantID, Domain: "retry.test", IsVerified: true, MXVerified: true}
	f.st.SeedZone(zone)
	mb := &models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, ZoneID: zone.ID, FullAddress: "a@retry.test", OwnerUserID: &f.userA.ID}
	f.st.SeedMailbox(mb)
	if err := f.st.CreateOutboundJob(context.Background(), &models.OutboundJob{
		ID: stateJob, TenantID: f.tenantID, UserID: &f.userA.ID, SenderUserID: &f.userA.ID,
		SenderMailboxID: &mb.ID, ZoneID: zone.ID, MailFrom: mb.FullAddress,
		State: models.OutboundDead, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	rejecting := NewOutboundHandler(outSvc, &notRetryableStore{f.st}, zerolog.Nop())
	rr = doOutboundHandlerRequest(t, f.st, rejecting.RetryJob, http.MethodPost, "/api/v1/outbound/"+stateJob.String()+"/retry", map[string]string{"id": stateJob.String()}, outboundUserHeaders(t, f.userA))
	if rr.Code != http.StatusConflict {
		t.Fatalf("not-retryable retry expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	if reason := conflictReason(t, rr); reason != "state_changed" {
		t.Fatalf("not-retryable retry reason = %q, want state_changed", reason)
	}
}
