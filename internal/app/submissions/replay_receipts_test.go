package submissions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// This is an explicit receipt-port stub, not a PG concurrency simulation.
// It verifies the application consumes exactly the current authorized snapshot
// and does not call a separate content predicate or new-send authorization.
type replayReceiptPort struct {
	*testutil.FakeStore
	result *store.OutboundReceipt
	err    error
	calls  int
	id     uuid.UUID
	scope  string
	actor  authz.Actor
}

func (p *replayReceiptPort) GetOutboundReceipt(_ context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	p.calls++
	p.actor, p.id, p.scope = a, id, scope
	return p.result, p.err
}
func (p *replayReceiptPort) CanReadOutboundContent(context.Context, authz.Actor, *models.OutboundJob) (bool, error) {
	panic("replay must use the current receipt snapshot, not separate content I/O")
}
func (p *replayReceiptPort) EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error) {
	panic("replay must not reapply new-send permission outside the receipt port")
}

func replayReceiptFixture() (*replayReceiptPort, *Service, authz.Actor, *models.OutboundJob) {
	v := int64(0)
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), SessionVersion: &v, Permission: &models.EffectivePermission{CanSend: false}}
	j := &models.OutboundJob{ID: uuid.New(), TenantID: a.TenantID, UserID: &a.ID, SubmitActor: outbound.SubmissionActor(&a.ID, nil), State: models.OutboundSent, RecipientLedger: true, TextBody: "STALE_ORIGINAL_BODY", Subject: "historical receipt"}
	current := *j
	current.TextBody = "CURRENT_SNAPSHOT_BODY"
	current.HTMLBody = "<b>CURRENT_SNAPSHOT_BODY</b>"
	current.BCC = []string{"private@fixture.test"}
	current.RawMIME = []byte("transport private")
	current.DeliveryToken = &current.ID
	p := &replayReceiptPort{FakeStore: testutil.NewFakeStore(), result: &store.OutboundReceipt{Job: &current, ContentAllowed: true, RecipientStates: []string{"accepted", "permanent"}, LedgerKnown: true}}
	return p, NewService(nil, p, nil, zerolog.Nop()), a, j
}

func requireReplayForbidden(t *testing.T, v *company.OutboundReceipt, e error) {
	t.Helper()
	a, ok := app.As(app.FromAuthz(e))
	if v != nil || !ok || a.Kind != app.KindForbidden {
		t.Fatalf("expected explicit POST forbidden without job, got %v %v", v, e)
	}
}

func TestReplayReceiptUsesCurrentSnapshotWithoutNewSendPolicy(t *testing.T) {
	for _, body := range []bool{true, false} {
		p, s, a, j := replayReceiptFixture()
		p.result.ContentAllowed = body
		v, e := s.ReplayReceiptView(context.Background(), a, j)
		if e != nil || v == nil {
			t.Fatalf("current historical submitter denied after CanSend revoked: %v", e)
		}
		if p.calls != 1 || p.id != j.ID || p.scope != "send:write" || p.actor.SessionVersion != a.SessionVersion {
			t.Fatalf("current credential/command scope lost: %+v", p)
		}
		requireOrdinaryReceiptWhitelist(t, v)
		if v.Capabilities == nil || v.Capabilities.ViewContent != body {
			t.Fatal("ordinary projection changed actual current-read hint")
		}
		if v.Status != "partially_accepted" || v.Progress.Counts == nil || v.Progress.Counts.Total != 2 {
			t.Fatal("replay did not consume full current snapshot ledger")
		}
		if p.result.Job.TextBody != "CURRENT_SNAPSHOT_BODY" || len(p.result.Job.BCC) != 1 {
			t.Fatal("replay mutated shared receipt snapshot")
		}
	}
}

func TestReplayReceiptRejectsInvalidOriginalBinding(t *testing.T) {
	for _, mode := range []string{"nil", "tenant", "principal", "actor", "empty-command"} {
		t.Run(mode, func(t *testing.T) {
			p, s, a, j := replayReceiptFixture()
			switch mode {
			case "nil":
				j = nil
			case "tenant":
				j.TenantID = uuid.New()
			case "principal":
				a.Type = "unknown"
			case "actor":
				j.SubmitActor = "user:" + uuid.NewString()
			case "empty-command":
				j.SubmitActor = ""
			}
			v, e := s.ReplayReceiptView(context.Background(), a, j)
			requireReplayForbidden(t, v, e)
			if p.calls != 0 {
				t.Fatal("invalid source binding reached receipt adapter")
			}
		})
	}
}

func TestReplayReceiptRejectsInvalidCurrentBinding(t *testing.T) {
	for _, mode := range []string{"nil-receipt", "nil-job", "tenant", "actor", "job-id"} {
		t.Run(mode, func(t *testing.T) {
			p, s, a, j := replayReceiptFixture()
			switch mode {
			case "nil-receipt":
				p.result = nil
			case "nil-job":
				p.result.Job = nil
			case "tenant":
				p.result.Job.TenantID = uuid.New()
			case "actor":
				p.result.Job.SubmitActor = "user:" + uuid.NewString()
			case "job-id":
				p.result.Job.ID = uuid.New()
			}
			v, e := s.ReplayReceiptView(context.Background(), a, j)
			requireReplayForbidden(t, v, e)
		})
	}
}

func TestReplayReceiptPreservesErrorsAndMapsCurrentDenial(t *testing.T) {
	storage := errors.New("controlled receipt storage failure")
	for _, e := range []error{app.Forbidden("current JWT revoked"), app.NotFound("current receipt unavailable"), authz.ErrForbidden("scope revoked"), storage, context.Canceled} {
		p, s, a, j := replayReceiptFixture()
		p.err = e
		v, got := s.ReplayReceiptView(context.Background(), a, j)
		if errors.Is(e, storage) || errors.Is(e, context.Canceled) {
			if v != nil || !errors.Is(got, e) {
				t.Fatalf("storage/cancel error flattened: %v %v", v, got)
			}
		} else {
			requireReplayForbidden(t, v, got)
		}
	}
}

func TestReplayAndReadReceiptScopesRemainDistinctForKey(t *testing.T) {
	p, s, a, j := replayReceiptFixture()
	a.Type = authz.PrincipalAPIKey
	a.OwnerUserID = j.UserID
	a.SessionVersion = nil
	j.APIKeyID = &a.ID
	j.SubmitActor = outbound.SubmissionActor(a.OwnerUserID, &a.ID)
	p.result.Job.SubmitActor = j.SubmitActor
	p.result.Job.APIKeyID = &a.ID
	if _, e := s.ReplayReceiptView(context.Background(), a, j); e != nil {
		t.Fatal(e)
	}
	if p.scope != "send:write" || p.actor.Type != authz.PrincipalAPIKey {
		t.Fatal("Key replay changed principal or used GET scope")
	}
	if _, e := s.OutboundReceiptView(context.Background(), &models.Tenant{ID: a.TenantID}, a, j.ID); e != nil {
		t.Fatal(e)
	}
	if p.scope != "send:read" || p.calls != 2 {
		t.Fatal("GET and POST receipt scopes conflated")
	}
}

// The actual FakeStore (not the projection stub) enforces the current Key
// scopes. This remains a mutex-backed unit test, not a PostgreSQL lock test.
func TestReplayReceiptFakeStoreEnforcesKeyWriteNotReadScope(t *testing.T) {
	for _, scope := range []string{"send:read", "send:write"} {
		t.Run(scope, func(t *testing.T) {
			f := newSubmissionFixture(t)
			ctx := context.Background()
			key := &models.TenantAPIKey{ID: uuid.New(), TenantID: f.tenant.ID, OwnerUserID: &f.owner.ID, Scopes: []string{scope}}
			if e := f.st.CreateAPIKey(ctx, key); e != nil {
				t.Fatal(e)
			}
			a := authz.Actor{Type: authz.PrincipalAPIKey, ID: key.ID, TenantID: key.TenantID, OwnerUserID: key.OwnerUserID, Permission: &models.EffectivePermission{CanSend: false}}
			j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.owner.ID, APIKeyID: &key.ID, SenderUserID: &f.owner.ID, SenderKeyID: &key.ID, SenderMailboxID: &f.mb.ID, MailFrom: f.mb.FullAddress, Subject: "historical receipt", TextBody: "synthetic", SubmitActor: outbound.SubmissionActor(&f.owner.ID, &key.ID)}
			if e := f.st.CreateOutboundJob(ctx, j); e != nil {
				t.Fatal(e)
			}
			post, postErr := f.svc.ReplayReceiptView(ctx, a, j)
			get, getErr := f.svc.OutboundReceiptView(ctx, f.tenant, a, j.ID)
			if scope == "send:read" {
				requireReplayForbidden(t, post, postErr)
				if getErr != nil || get == nil {
					t.Fatalf("read-only Key lost GET receipt: %v", getErr)
				}
			} else {
				if postErr != nil || post == nil {
					t.Fatalf("write-only historical Key cannot POST replay: %v", postErr)
				}
				if get != nil || !errors.Is(getErr, ErrOutboundJobNotFound) {
					t.Fatalf("write scope widened into GET read: %v %v", get, getErr)
				}
			}
		})
	}
}
