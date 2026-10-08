package ingest

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// Real Accept and durable ProcessBatch use the final synthetic store's normal
// receipt/target implementation. Counters observe attempted side effects; this
// does not claim PostgreSQL transactions, rollback, or production persistence.
type r5RetentionDeliveryStore struct {
	*testutil.FakeStore
	quota, creates, deliveries, holds, failures int
}

func (s *r5RetentionDeliveryStore) CountTenantMessagesSince(ctx context.Context, tenant uuid.UUID, since time.Time) (int, error) {
	s.quota++
	return s.FakeStore.CountTenantMessagesSince(ctx, tenant, since)
}
func (s *r5RetentionDeliveryStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, max int, ensure func(context.Context) error) (bool, error) {
	s.creates++
	return s.FakeStore.CreateMessageWithQuota(ctx, m, max, ensure)
}
func (s *r5RetentionDeliveryStore) DeliverIngress(ctx context.Context, c *store.IngressClaim, m *models.Message, max, daily int) (bool, error) {
	s.deliveries++
	return s.FakeStore.DeliverIngress(ctx, c, m, max, daily)
}
func (s *r5RetentionDeliveryStore) HoldIngressTarget(ctx context.Context, c *store.IngressClaim, mailbox uuid.UUID, reason string) error {
	s.holds++
	return s.FakeStore.HoldIngressTarget(ctx, c, mailbox, reason)
}
func (s *r5RetentionDeliveryStore) FailIngressTarget(ctx context.Context, c *store.IngressClaim, mailbox uuid.UUID, reason string) error {
	s.failures++
	return s.FakeStore.FailIngressTarget(ctx, c, mailbox, reason)
}

func TestR5RetentionExpiryIngestBothPaths(t *testing.T) {
	for _, durable := range []bool{false, true} {
		path := "immediate"
		if durable {
			path = "durable"
		}
		for _, tc := range []struct {
			name, source, policy string
			hours                int
			invalid              bool
		}{
			{"tenant-three-million", "tenant", "", 3000000, false},
			{"mailbox-three-million", "mailbox", "", 3000000, false},
			{"route-three-million", "route", "", 3000000, false},
			{"negative-three-million", "tenant", "", -3000000, false},
			{"ordinary", "tenant", "", 24, false},
			{"zero", "tenant", "", 0, false},
			{"owned-ignores-invalid-plan", "tenant", "owned", math.MaxInt32, false},
			{"shared-inherited-ignores-invalid-plan", "tenant", "shared", math.MaxInt32, false},
			{"shared-explicit-finite", "mailbox", "shared", 3000000, false},
			{"mailbox-over-invalid-plan", "mailbox-over-plan", "", 24, false},
			{"tenant-over-upper", "tenant", "", math.MaxInt32, true},
			{"tenant-under-lower", "tenant", "", math.MinInt32, true},
			{"mailbox-over-upper", "mailbox", "", math.MaxInt32, true},
			{"route-over-upper", "route", "", math.MaxInt32, true},
			{"native-int-over-upper", "tenant", "", int(^uint(0) >> 1), true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				planHours := 24
				if tc.source == "tenant" {
					planHours = tc.hours
				}
				if tc.source == "mailbox-over-plan" {
					planHours = math.MaxInt32
				}
				base, objects, _, _, mailbox, _ := newCoreFixture(t, func(p *models.Plan) { p.RetentionHours = planHours }, true, nil)
				if tc.source == "mailbox" || tc.source == "mailbox-over-plan" {
					hours := tc.hours
					mailbox.RetentionHoursOverride = &hours
				}
				if tc.source == "route" {
					routes, err := base.ListRoutes(context.Background(), mailbox.ZoneID)
					if err != nil || len(routes) != 1 {
						t.Fatalf("route fixture: %v %v", routes, err)
					}
					hours := tc.hours
					routes[0].RetentionHoursOverride = &hours
					base.SeedRoute(routes[0])
					mailbox.RouteID = &routes[0].ID
				}
				if tc.policy == "owned" {
					owner := uuid.New()
					mailbox.OwnerUserID = &owner
				}
				if tc.policy == "shared" {
					mailbox.Kind = "shared"
				}
				r5ReplaceRetentionMailbox(t, base, mailbox)
				if tc.source == "route" && !durable {
					// Immediate routing materializes a route's override onto a
					// newly created mailbox; existing-mailbox Resolve has no Route.
					if err := base.DeleteMailbox(context.Background(), mailbox.ID); err != nil {
						t.Fatal(err)
					}
				}
				st := &r5RetentionDeliveryStore{FakeStore: base}
				hub := realtime.NewHub(1, nil)
				events, unsubscribe := hub.Subscribe("")
				defer unsubscribe()
				svc := NewService(st, objects, resolver.New(base, policy.NamingFull, true), hub, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: durable, BatchSize: 10, MaxRetries: 2}, zerolog.Nop())
				ctx := context.Background()
				before := time.Now()
				result, err := svc.Accept(ctx, Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{mailbox.FullAddress}}, []byte(coreRaw))
				var receipt *models.IngestJob
				if durable {
					if err != nil || !result.Queued {
						t.Fatalf("original must be durably accepted before current policy replay: %+v %v", result, err)
					}
					jobs, _, e := base.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
					if e != nil || len(jobs) != 1 {
						t.Fatalf("receipt missing: %v %v", jobs, e)
					}
					receipt = jobs[0]
					svc.ProcessBatch(ctx)
				} else if tc.invalid {
					if err == nil || !strings.Contains(err.Error(), "retention_config") || result.Delivered != 0 {
						t.Errorf("invalid finite retention falsely acknowledged: %+v %v", result, err)
					}
				} else if err != nil || result.Delivered != 1 {
					t.Fatalf("valid retention rejected: %+v %v", result, err)
				}
				after := time.Now()
				if tc.invalid {
					if st.quota != 0 || st.creates != 0 || st.deliveries != 0 || mailboxMessageCount(t, base, mailbox.FullAddress) != 0 {
						t.Errorf("invalid expiry crossed quota/message write: quota=%d creates=%d deliveries=%d", st.quota, st.creates, st.deliveries)
					}
					audits, e := base.ListAuditEntries(ctx, 100)
					if e != nil || len(audits) != 0 {
						t.Errorf("invalid expiry wrote delivery audit: %v %v", audits, e)
					}
					outbox, e := base.ClaimOutboxEvents(ctx, time.Now(), 100)
					if e != nil || len(outbox) != 0 {
						t.Errorf("invalid expiry wrote outbox: %v %v", outbox, e)
					}
					select {
					case event := <-events:
						t.Errorf("invalid expiry published success: %+v", event)
					default:
					}
					if durable {
						targets, e := base.ListIngressTargets(ctx, receipt.ID)
						if e != nil || len(targets) != 1 || targets[0].State != "held" || !strings.Contains(targets[0].LastError, "retention") || st.holds != 1 || st.failures != 0 {
							t.Errorf("invalid policy not retained for review: %+v %v holds=%d failures=%d", targets, e, st.holds, st.failures)
						}
						jobs, _, e := base.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
						if e != nil || len(jobs) != 1 || jobs[0].State != "dead" {
							t.Errorf("held receipt finalized incorrectly: %+v %v", jobs, e)
						}
						exists, e := objects.Exists(ctx, receipt.RawObjectKey)
						if e != nil || !exists {
							t.Errorf("accepted original lost: %v %v", exists, e)
						}
						refs, e := base.CountRawObjectReferences(ctx, receipt.RawObjectKey)
						if e != nil || refs == 0 {
							t.Errorf("held original lost reference: %d %v", refs, e)
						}
					}
					return
				}
				msg := fetchSingleMessage(t, base, mailbox.FullAddress)
				permanent := tc.hours == 0 || tc.policy == "owned" || tc.policy == "shared" && tc.source == "tenant"
				if permanent {
					if msg.ExpiresAt != nil {
						t.Errorf("permanent policy changed: %v", msg.ExpiresAt)
					}
				} else {
					lo := time.Unix(before.Unix()+int64(tc.hours)*3600, 0)
					hi := time.Unix(after.Unix()+int64(tc.hours)*3600+1, 0)
					if msg.ExpiresAt == nil || msg.ExpiresAt.Before(lo) || !msg.ExpiresAt.Before(hi) {
						t.Errorf("stored wrong finite expiry: got=%v expected between %v and %v", msg.ExpiresAt, lo, hi)
					}
					if durable && (msg.ExpiresAt == nil || !msg.ExpiresAt.Equal(time.Unix(receipt.CreatedAt.Unix()+int64(tc.hours)*3600, int64(receipt.CreatedAt.Nanosecond())))) {
						t.Errorf("replay changed original acceptance-time expiry: %v", msg.ExpiresAt)
					}
				}
				if _, err := json.Marshal(msg); err != nil {
					t.Errorf("stored message cannot be returned as JSON: %v", err)
				}
			})
		}
	}
}

func TestR5RetentionExpiryImmediateMixedRecipientsCannotFalseAck(t *testing.T) {
	base, objects, _, _, bad, good := newCoreFixture(t, func(p *models.Plan) { p.RetentionHours = math.MaxInt32 }, true, nil)
	hours := 24
	good.RetentionHoursOverride = &hours
	r5ReplaceRetentionMailbox(t, base, good)
	st := &r5RetentionDeliveryStore{FakeStore: base}
	svc := NewService(st, objects, resolver.New(base, policy.NamingFull, true), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{}, zerolog.Nop())
	result, err := svc.Accept(context.Background(), Envelope{Source: "smtp", Recipients: []string{bad.FullAddress, good.FullAddress}}, []byte(coreRaw))
	if err == nil || !strings.Contains(err.Error(), "retention_config") || result.Delivered != 0 {
		t.Errorf("mixed invalid config was treated as a terminal rejection/successful envelope: %+v %v", result, err)
	}
	if mailboxMessageCount(t, base, bad.FullAddress) != 0 || mailboxMessageCount(t, base, good.FullAddress) != 1 || st.quota != 1 || st.creates != 1 {
		t.Errorf("per-recipient validation changed: quota=%d creates=%d", st.quota, st.creates)
	}
}

// SeedMailbox silently ignores duplicate-address errors. Replace the initial
// empty fixture row explicitly, before admission, and require both operations.
func r5ReplaceRetentionMailbox(t *testing.T, st *testutil.FakeStore, mailbox *models.Mailbox) {
	t.Helper()
	if err := st.DeleteMailbox(context.Background(), mailbox.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMailbox(context.Background(), mailbox); err != nil {
		t.Fatal(err)
	}
}
