package submissions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type receiptProjectionProbe struct {
	*testutil.FakeStore
	rows   []store.OutboundReceipt
	err    error
	calls  int
	scopes []string
}

func (p *receiptProjectionProbe) ListOutboundReceipts(context.Context, authz.Actor, models.Page) ([]store.OutboundReceipt, int, error) {
	p.calls++
	return p.rows, len(p.rows), p.err
}
func (p *receiptProjectionProbe) GetOutboundReceipt(_ context.Context, _ authz.Actor, _ uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	p.scopes = append(p.scopes, scope)
	if len(p.rows) == 0 {
		return nil, p.err
	}
	return &p.rows[0], p.err
}
func (p *receiptProjectionProbe) CanReadOutboundContent(context.Context, authz.Actor, *models.OutboundJob) (bool, error) {
	panic("receipt projection must not perform per-row authority I/O")
}

func TestOutboundReceiptProjectionUsesOneAuthorizedPage(t *testing.T) {
	tenant := &models.Tenant{ID: uuid.New()}
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID}
	p := &receiptProjectionProbe{FakeStore: testutil.NewFakeStore()}
	for i := 0; i < 50; i++ {
		p.rows = append(p.rows, store.OutboundReceipt{Job: &models.OutboundJob{ID: uuid.New(), TenantID: tenant.ID, Subject: "receipt", State: models.OutboundSent, RecipientLedger: true, TextBody: "private", BCC: []string{"hidden@fixture.test"}, RawMIME: []byte("private raw"), To: []string{"client@fixture.test"}}, ContentAllowed: i%2 == 0, RecipientStates: []string{"accepted", "permanent"}, LedgerKnown: true})
	}
	svc := NewService(nil, p, nil, zerolog.Nop())
	rows, total, e := svc.ListOutboundReceiptViews(context.Background(), tenant, a, models.Page{})
	if e != nil || total != 50 || len(rows) != 50 || p.calls != 1 {
		t.Fatalf("page assembly: count=%d calls=%d err=%v", total, p.calls, e)
	}
	for i, row := range rows {
		requireOrdinaryReceiptWhitelist(t, row)
		if row.Capabilities == nil || row.Capabilities.ViewContent != (i%2 == 0) {
			t.Fatal("actual content authority hint lost")
		}
		if row.Status != "partially_accepted" || row.Progress.Counts == nil || row.Progress.Counts.Total != 2 || row.Progress.Counts.Permanent != 1 {
			t.Fatal("complete ledger aggregation lost")
		}
		if p.rows[i].Job.TextBody != "private" || len(p.rows[i].Job.BCC) != 1 {
			t.Fatal("projection mutated shared data")
		}
	}
}

func TestOutboundReceiptProjectionPreservesScopeAndFailsClosed(t *testing.T) {
	tenant := &models.Tenant{ID: uuid.New()}
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID}
	id := uuid.New()
	p := &receiptProjectionProbe{FakeStore: testutil.NewFakeStore(), rows: []store.OutboundReceipt{{Job: &models.OutboundJob{ID: id, TenantID: tenant.ID}}}}
	svc := NewService(nil, p, nil, zerolog.Nop())
	ctx := context.Background()
	if _, e := svc.AccessibleOutboundJob(ctx, tenant, a, id); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.AccessibleOutboundJobForRetry(ctx, tenant, a, id); e != nil {
		t.Fatal(e)
	}
	if len(p.scopes) != 2 || p.scopes[0] != "send:read" || p.scopes[1] != "send:write" {
		t.Fatalf("scope conflated: %v", p.scopes)
	}
	failure := errors.New("fixture storage failure")
	p.err = failure
	if rows, total, e := svc.ListOutboundReceiptViews(ctx, tenant, a, models.Page{}); !errors.Is(e, failure) || rows != nil || total != 0 {
		t.Fatal("partial failed page returned")
	}
	if v, e := svc.OutboundReceiptView(ctx, tenant, a, id); !errors.Is(e, failure) || v != nil {
		t.Fatal("partial failed detail returned")
	}
	p.err = nil
	p.rows = nil
	rows, total, e := svc.ListOutboundReceiptViews(ctx, tenant, a, models.Page{})
	if e != nil || rows == nil || len(rows) != 0 || total != 0 {
		t.Fatal("empty page must be an empty array")
	}
}
