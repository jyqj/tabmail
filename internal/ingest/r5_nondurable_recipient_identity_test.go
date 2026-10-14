package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

// Use the actual Accept, resolver, raw-object and Redis paths. The metadata
// adapter observes effects; it does not prove PostgreSQL transaction guarantees.
type recipientIdentityStore struct {
	*testutil.FakeStore
	lookups  []string
	writes   []*models.Message
	events   []hooks.Event
	resolve  func(string, int) (*models.Mailbox, error)
	writeErr error
}

func (s *recipientIdentityStore) GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error) {
	s.lookups = append(s.lookups, address)
	if s.resolve != nil {
		return s.resolve(address, len(s.lookups))
	}
	return s.FakeStore.GetMailboxByAddress(ctx, address)
}

func (s *recipientIdentityStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, limit int, ensure func(context.Context) error) (bool, error) {
	cp := *m
	cp.Recipients = append([]string(nil), m.Recipients...)
	s.writes = append(s.writes, &cp)
	if s.writeErr != nil {
		return false, s.writeErr
	}
	ok, err := s.FakeStore.CreateMessageWithQuota(ctx, m, limit, ensure)
	cp.ID = m.ID
	return ok, err
}

func (s *recipientIdentityStore) CreateOutboxEvent(ctx context.Context, event *models.OutboxEvent) error {
	var payload hooks.Event
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	s.events = append(s.events, payload)
	return s.FakeStore.CreateOutboxEvent(ctx, event)
}

type recipientIdentityFixture struct {
	st      *recipientIdentityStore
	svc     *Service
	redis   *miniredis.Miniredis
	events  <-chan realtime.Event
	mailbox *models.Mailbox
	raw     []byte
}

func newRecipientIdentityFixture(t *testing.T, stripPlus bool) *recipientIdentityFixture {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	st := &recipientIdentityStore{FakeStore: testutil.NewFakeStore()}
	plan, tenant, zone := uuid.New(), uuid.New(), uuid.New()
	st.SeedPlan(&models.Plan{ID: plan, MaxMessagesPerMailbox: 100, MaxMessageBytes: 1024, DailyQuota: 100, RetentionHours: 24})
	st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
	st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
	st.SeedMailbox(mb)
	hub := realtime.NewHub(0, nil)
	events, unsubscribe := hub.Subscribe("")
	t.Cleanup(unsubscribe)
	dispatcher := hooks.New(hooks.Config{}, zerolog.Nop()).BindStore(st)
	svc := NewService(st, testutil.NewMemoryObjectStore(), resolver.New(st, policy.NamingFull, stripPlus), hub, dispatcher,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, client, config.Ingest{Durable: false}, zerolog.Nop())
	return &recipientIdentityFixture{st: st, svc: svc, redis: server, events: events, mailbox: mb, raw: []byte("Subject: one envelope\r\n\r\nsynthetic duplicate recipients")}
}

func (f *recipientIdentityFixture) accept(addresses []string, opts ...AcceptOption) (AcceptResult, error) {
	return f.svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: addresses}, f.raw, opts...)
}

func (f *recipientIdentityFixture) assertEffects(t *testing.T, wanted map[uuid.UUID]string, total int) {
	t.Helper()
	if len(f.st.writes) != total || len(f.st.events) != total {
		t.Errorf("metadata writes=%d webhook publications=%d, want %d each", len(f.st.writes), len(f.st.events), total)
	}
	for mailbox, address := range wanted {
		messages, count, err := f.st.ListMessages(context.Background(), mailbox, models.Page{Page: 1, PerPage: 100})
		if err != nil || count != 1 || len(messages) != 1 {
			t.Errorf("mailbox %s messages=%d/%d err=%v, want one", mailbox, count, len(messages), err)
			continue
		}
		if !reflect.DeepEqual(messages[0].Recipients, []string{address}) {
			t.Errorf("first recipient identity changed: %v, want %q", messages[0].Recipients, address)
		}
	}
	key := fmt.Sprintf("smtp:quota:tenant:%s:%s", f.mailbox.TenantID, time.Now().UTC().Format("20060102"))
	quota, err := f.redis.Get(key)
	if err != nil || quota != fmt.Sprint(total) {
		t.Errorf("daily quota=%q err=%v, want %d", quota, err, total)
	}
	// Publish is synchronous and Accept has returned, so draining the buffered
	// channel checks the exact count without a sleep or a negative network wait.
	var gotEvents []realtime.Event
	for len(f.events) > 0 {
		gotEvents = append(gotEvents, <-f.events)
	}
	if len(gotEvents) != total {
		t.Errorf("realtime events=%d, want %d", len(gotEvents), total)
	}
	for _, event := range gotEvents {
		id, err := uuid.Parse(event.MessageID)
		if err != nil {
			t.Fatal(err)
		}
		m, err := f.st.GetMessage(context.Background(), id)
		if err != nil || m == nil || m.Subject != "one envelope" {
			t.Errorf("realtime event did not identify the persisted message: %+v %v", event, err)
		}
	}
}

func TestR5NonDurableRecipientIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recipients []string
		lookups    int
		first      string
	}{
		{"exact repeat", []string{"reader@mail.test", "reader@mail.test"}, 1, "reader@mail.test"},
		{"canonical repeat", []string{" Reader@MAIL.TEST ", "reader@mail.test", "<reader@mail.test>"}, 1, "reader@mail.test"},
		{"plus aliases", []string{"reader@mail.test", "reader+one@mail.test", "reader+two@mail.test"}, 3, "reader@mail.test"},
		{"alias first", []string{"reader+one@mail.test", "reader@mail.test", "reader+one@mail.test"}, 2, "reader+one@mail.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecipientIdentityFixture(t, true)
			input := append([]string(nil), tc.recipients...)
			result, err := f.accept(input)
			if err != nil || result.Queued || result.Delivered != 1 {
				t.Errorf("Accept=%+v err=%v, want one stored", result, err)
			}
			if len(f.st.lookups) != tc.lookups {
				t.Errorf("resolver calls=%v, want %d", f.st.lookups, tc.lookups)
			}
			if !reflect.DeepEqual(input, tc.recipients) {
				t.Error("Accept mutated caller recipients")
			}
			f.assertEffects(t, map[uuid.UUID]string{f.mailbox.ID: tc.first}, 1)
		})
	}
	t.Run("distinct mailboxes retain separate deliveries", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		other := *f.mailbox
		other.ID, other.LocalPart, other.FullAddress = uuid.New(), "other", "other@mail.test"
		f.st.SeedMailbox(&other)
		result, err := f.accept([]string{f.mailbox.FullAddress, other.FullAddress})
		if err != nil || result.Delivered != 2 {
			t.Fatalf("Accept=%+v err=%v", result, err)
		}
		f.assertEffects(t, map[uuid.UUID]string{f.mailbox.ID: f.mailbox.FullAddress, other.ID: other.FullAddress}, 2)
	})
	t.Run("plus stripping disabled preserves separate mailboxes", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, false)
		other := *f.mailbox
		other.ID, other.LocalPart, other.FullAddress = uuid.New(), "reader+one", "reader+one@mail.test"
		f.st.SeedMailbox(&other)
		result, err := f.accept([]string{f.mailbox.FullAddress, other.FullAddress})
		if err != nil || result.Delivered != 2 {
			t.Fatalf("Accept=%+v err=%v", result, err)
		}
		f.assertEffects(t, map[uuid.UUID]string{f.mailbox.ID: f.mailbox.FullAddress, other.ID: other.FullAddress}, 2)
	})
	t.Run("canonical repeat cannot observe another destination", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		other := *f.mailbox
		other.ID, other.FullAddress = uuid.New(), "replacement@mail.test"
		f.st.SeedMailbox(&other)
		f.st.resolve = func(_ string, call int) (*models.Mailbox, error) {
			if call == 1 {
				return f.mailbox, nil
			}
			return &other, nil
		}
		result, err := f.accept([]string{f.mailbox.FullAddress, f.mailbox.FullAddress})
		if err != nil || result.Delivered != 1 || len(f.st.lookups) != 1 {
			t.Errorf("repeated address resolved anew: %+v %v lookups=%v", result, err, f.st.lookups)
		}
		_, count, _ := f.st.ListMessages(context.Background(), other.ID, models.Page{Page: 1, PerPage: 100})
		if count != 0 {
			t.Errorf("replacement mailbox received %d copies", count)
		}
		f.assertEffects(t, map[uuid.UUID]string{f.mailbox.ID: f.mailbox.FullAddress}, 1)
	})
	t.Run("SMTP RCPT cached aliases share mailbox identity", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		zone, _ := f.st.GetZone(context.Background(), f.mailbox.ZoneID)
		resolved := &resolver.Result{Zone: zone, Mailbox: f.mailbox}
		addresses := []string{"reader+first@mail.test", f.mailbox.FullAddress}
		result, err := f.accept(addresses, WithResolved(addresses[0], resolved), WithResolved(addresses[1], resolved))
		if err != nil || result.Delivered != 1 || len(f.st.lookups) != 0 {
			t.Errorf("cached Accept=%+v %v lookups=%v", result, err, f.st.lookups)
		}
		f.assertEffects(t, map[uuid.UUID]string{f.mailbox.ID: addresses[0]}, 1)
	})
	t.Run("failed persistence is not retried through another alias", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		f.st.writeErr = errors.New("synthetic metadata fault")
		result, err := f.accept([]string{f.mailbox.FullAddress, "reader+retry@mail.test"})
		if err == nil || result.Delivered != 0 || len(f.st.writes) != 1 {
			t.Errorf("failure crossed persistence twice: %+v %v writes=%d", result, err, len(f.st.writes))
		}
		if len(f.st.events) != 0 || len(f.events) != 0 || len(f.redis.Keys()) != 0 {
			t.Errorf("failed identity left side effects: webhook=%d realtime=%d redis=%v", len(f.st.events), len(f.events), f.redis.Keys())
		}
	})
	t.Run("failed canonical resolution is not retried", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		f.st.resolve = func(string, int) (*models.Mailbox, error) { return nil, errors.New("synthetic resolver fault") }
		result, err := f.accept([]string{f.mailbox.FullAddress, f.mailbox.FullAddress})
		if err == nil || result.Delivered != 0 || len(f.st.lookups) != 1 || len(f.st.writes) != 0 {
			t.Errorf("resolve failure changed: %+v %v lookups=%v writes=%d", result, err, f.st.lookups, len(f.st.writes))
		}
	})
	t.Run("separate envelopes remain separate deliveries", func(t *testing.T) {
		f := newRecipientIdentityFixture(t, true)
		for i := 0; i < 2; i++ {
			result, err := f.accept([]string{f.mailbox.FullAddress})
			if err != nil || result.Delivered != 1 {
				t.Fatalf("Accept=%+v err=%v", result, err)
			}
		}
		_, count, err := f.st.ListMessages(context.Background(), f.mailbox.ID, models.Page{Page: 1, PerPage: 100})
		if err != nil || count != 2 {
			t.Errorf("cross-envelope dedup: count=%d err=%v", count, err)
		}
		f.assertEffects(t, nil, 2)
	})
}

func TestR5NonDurableConfigurationRemainsAvailable(t *testing.T) {
	t.Setenv("TABMAIL_JWT_SECRET", "test-only-long-jwt-secret")
	t.Setenv("TABMAIL_MAILBOX_TOKEN_SECRET", "test-only-long-mailbox-secret")
	t.Setenv("TABMAIL_COMPANY_ONLY", "false")
	t.Setenv("TABMAIL_INGEST_DURABLE", "false")
	cfg, err := config.Load()
	if err != nil || cfg.CompanyOnly || cfg.Ingest.Durable {
		t.Fatalf("supported compatibility configuration unavailable: %+v %v", cfg, err)
	}
}
