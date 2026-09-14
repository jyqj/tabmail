package outbound

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
	"testing"
	"time"
)

type domainFixtureAdapter struct {
	calls     map[string]int
	failOnce  bool
	uncertain bool
}

func (*domainFixtureAdapter) Name() string { return "loopback-fixture" }
func (a *domainFixtureAdapter) Deliver(_ context.Context, j *models.OutboundJob, _ []byte) (*DeliveryResult, error) {
	groups := groupByDomain(j.RcptTo)
	if len(groups) != 1 {
		return nil, errors.New("worker must isolate recipient domains")
	}
	var domain string
	for d := range groups {
		domain = d
	}
	a.calls[domain]++
	result := &DeliveryResult{Adapter: a.Name(), RemoteHost: domain, StartedAt: time.Now(), FinishedAt: time.Now()}
	if a.uncertain {
		return result, fmt.Errorf("%w: simulated lost DATA reply", store.ErrOutboundUncertain)
	}
	if a.failOnce && domain == "b.test" && a.calls[domain] == 1 {
		return result, errors.New("temporary relay failure")
	}
	result.SMTPCode = 250
	return result, nil
}
func recoverySendFixture(t *testing.T) (*testutil.FakeStore, *Service, SendRequest, *domainFixtureAdapter) {
	t.Helper()
	st := testutil.NewFakeStore()
	req := quotaTestSendRequest(uuid.New(), uuid.New())
	seedQuotaSender(t, st, &req)
	req.To = []string{"alice@a.test", "bob@b.test"}
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 5, RetryDelay: time.Nanosecond}, st, zerolog.Nop())
	adapter := &domainFixtureAdapter{calls: map[string]int{}}
	svc.adapter = adapter
	return st, svc, req, adapter
}
func TestP0OutboundPartialDomainRecoverySkipsAcceptedDomain(t *testing.T) {
	st, svc, req, a := recoverySendFixture(t)
	a.failOnce = true
	ctx := context.Background()
	job, err := svc.Submit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	stored, _ := st.GetOutboundJob(ctx, job.ID)
	if stored.State != models.OutboundRetry || len(stored.DeliveredDomains) != 1 || stored.DeliveredDomains[0] != "a.test" {
		t.Fatalf("missing durable progress: %+v", stored)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	stored, _ = st.GetOutboundJob(ctx, job.ID)
	if stored.State != models.OutboundSent || len(stored.DeliveredDomains) != 2 || a.calls["a.test"] != 1 || a.calls["b.test"] != 2 {
		t.Fatalf("duplicate or lost delivery: job=%+v calls=%v", stored, a.calls)
	}
}
func TestP0OutboundUncertainAcceptanceCannotBeBlindlyRetried(t *testing.T) {
	st, svc, req, a := recoverySendFixture(t)
	a.uncertain = true
	ctx := context.Background()
	job, err := svc.Submit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	stored, _ := st.GetOutboundJob(ctx, job.ID)
	if stored.State != models.OutboundFailed || stored.InFlightDomain != "a.test" {
		t.Fatalf("uncertainty lost: %+v", stored)
	}
	if !errors.Is(svc.ValidateJobAuthorization(ctx, stored), store.ErrOutboundUncertain) {
		t.Fatal("retry validation lost uncertainty")
	}
	if !errors.Is(st.RequeueOutboundJob(ctx, job.ID), store.ErrOutboundNotRetryable) {
		t.Fatal("manual retry permitted")
	}
	svc.ensureWorker().ProcessBatch(ctx)
	if a.calls["a.test"] != 1 {
		t.Fatal("unknown acceptance resent")
	}
}

type checkpointFailure struct{ *testutil.FakeStore }

func (s checkpointFailure) CompleteOutboundDomain(context.Context, uuid.UUID, *uuid.UUID, string, bool) error {
	return errors.New("checkpoint database failure")
}
func TestP0OutboundCheckpointFailureRetainsUncertainMarker(t *testing.T) {
	st, svc, req, a := recoverySendFixture(t)
	svc.store = checkpointFailure{st}
	ctx := context.Background()
	job, err := svc.Submit(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	stored, _ := st.GetOutboundJob(ctx, job.ID)
	if stored.InFlightDomain == "" || stored.State != models.OutboundFailed {
		t.Fatalf("lost accepted-but-uncheckpointed state: %+v", stored)
	}
	svc.ensureWorker().ProcessBatch(ctx)
	if a.calls["a.test"] != 1 {
		t.Fatal("checkpoint failure resent accepted domain")
	}
}
func TestP0OutboundRevokedGrantAndInactiveOwnerBlockQueuedSend(t *testing.T) {
	for _, mode := range []string{"grant", "inactive", "deleted-key"} {
		t.Run(mode, func(t *testing.T) {
			st, svc, req, a := recoverySendFixture(t)
			ctx := context.Background()
			var grant *models.MailboxGrant
			if mode == "grant" {
				mb := &models.Mailbox{ID: uuid.New(), TenantID: req.TenantID, ZoneID: req.ZoneID, FullAddress: "shared@example.test", AccessMode: models.AccessAPIKey}
				st.SeedMailbox(mb)
				req.From = mb.FullAddress
				req.SenderMailboxID = &mb.ID
				grant = &models.MailboxGrant{TenantID: req.TenantID, MailboxID: mb.ID, UserID: *req.UserID, CanSend: true}
				if err := st.SetMailboxGrant(ctx, grant); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "deleted-key" {
				key := &models.TenantAPIKey{ID: uuid.New(), TenantID: req.TenantID, OwnerUserID: req.UserID, Scopes: []string{"send:write"}}
				if err := st.CreateAPIKey(ctx, key); err != nil {
					t.Fatal(err)
				}
				req.APIKeyID = &key.ID
			}
			job, err := svc.Submit(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "grant":
				grant.CanSend = false
				if err := st.SetMailboxGrant(ctx, grant); err != nil {
					t.Fatal(err)
				}
			case "inactive":
				u, _ := st.GetUser(ctx, *req.UserID)
				u.IsActive = false
				if err := st.UpdateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
			case "deleted-key":
				if err := st.DeleteAPIKey(ctx, *req.APIKeyID); err != nil {
					t.Fatal(err)
				}
			}
			svc.ensureWorker().ProcessBatch(ctx)
			stored, _ := st.GetOutboundJob(ctx, job.ID)
			if stored.State != models.OutboundFailed || len(a.calls) != 0 {
				t.Fatalf("revoked sender delivered: %+v calls=%v", stored, a.calls)
			}
		})
	}
}
