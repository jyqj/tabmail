package ingest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

type dailyQuotaBoundaryStore struct {
	*testutil.FakeStore
	limit       int
	outcome     string
	beforeWrite func()
	writes      int
}

func (s *dailyQuotaBoundaryStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &models.EffectiveConfig{DailyQuota: s.limit, MaxMessagesPerMailbox: 100, MaxMessageBytes: 1024, RetentionHours: 24}, nil
}

func (s *dailyQuotaBoundaryStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, limit int, ensure func(context.Context) error) (bool, error) {
	s.writes++
	if s.beforeWrite != nil {
		s.beforeWrite()
	}
	switch s.outcome {
	case "metadata failure":
		return false, errors.New("synthetic message persistence failure")
	case "mailbox quota":
		return false, nil
	default:
		return s.FakeStore.CreateMessageWithQuota(ctx, m, limit, ensure)
	}
}

func dailyQuotaRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: server.Addr(), MaxRetries: -1, PoolSize: 1,
		// The midnight case advances only the application's synctest clock.
		// Real socket deadlines must not use that synthetic calendar date.
		ReadTimeout: -1, WriteTimeout: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return server, client
}

func dailyQuotaService(t *testing.T, client *redis.Client, limit int, outcome string) (*dailyQuotaBoundaryStore, *Service, uuid.UUID) {
	t.Helper()
	st := &dailyQuotaBoundaryStore{FakeStore: testutil.NewFakeStore(), limit: limit, outcome: outcome}
	tenant, zone, mailbox := uuid.New(), uuid.New(), uuid.New()
	st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedMailbox(&models.Mailbox{ID: mailbox, TenantID: tenant, ZoneID: zone, LocalPart: "reader", ResolvedDomain: "mail.test", FullAddress: "reader@mail.test", AccessMode: models.AccessPublic})
	svc := NewService(st, testutil.NewMemoryObjectStore(), resolver.New(st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, client, config.Ingest{Durable: false}, zerolog.Nop())
	return st, svc, tenant
}

func dailyQuotaKey(tenant uuid.UUID, at time.Time) string {
	return fmt.Sprintf("smtp:quota:tenant:%s:%s", tenant, at.UTC().Format("20060102"))
}

func dailyQuotaAccept(svc *Service) (AcceptResult, error) {
	return svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"reader@mail.test"}}, []byte("Subject: quota ownership\r\n\r\nsynthetic body"))
}

func assertDailyQuota(t *testing.T, server *miniredis.Miniredis, key, want string) {
	t.Helper()
	got, err := server.Get(key)
	if err != nil || got != want {
		t.Errorf("daily count = %q (%v), want %q", got, err, want)
	}
}

func TestR5DailyQuotaReservationOwnership(t *testing.T) {
	for _, outcome := range []string{"metadata failure", "mailbox quota"} {
		for _, initial := range []string{"1", "4"} {
			t.Run("unlimited "+outcome+" existing count "+initial, func(t *testing.T) {
				server, client := dailyQuotaRedis(t)
				st, svc, tenant := dailyQuotaService(t, client, 0, outcome)
				key := dailyQuotaKey(tenant, time.Now())
				if err := server.Set(key, initial); err != nil {
					t.Fatal(err)
				}
				st.beforeWrite = func() { assertDailyQuota(t, server, key, initial) }
				result, err := dailyQuotaAccept(svc)
				if err == nil || result.Delivered != 0 || st.writes != 1 {
					t.Fatalf("expected one failed persistence: result=%+v err=%v writes=%d", result, err, st.writes)
				}
				assertDailyQuota(t, server, key, initial)
			})
		}
	}
	for _, outcome := range []string{"metadata failure", "mailbox quota", "success"} {
		t.Run("finite "+outcome, func(t *testing.T) {
			server, client := dailyQuotaRedis(t)
			st, svc, tenant := dailyQuotaService(t, client, 10, outcome)
			key := dailyQuotaKey(tenant, time.Now())
			if err := server.Set(key, "4"); err != nil {
				t.Fatal(err)
			}
			st.beforeWrite = func() { assertDailyQuota(t, server, key, "5") }
			result, err := dailyQuotaAccept(svc)
			if st.writes != 1 || (err == nil) != (outcome == "success") {
				t.Fatalf("wrong persistence outcome: result=%+v err=%v writes=%d", result, err, st.writes)
			}
			want := "4"
			if outcome == "success" {
				want = "5"
				if result.Delivered != 1 {
					t.Fatalf("successful message was not delivered: %+v", result)
				}
			}
			assertDailyQuota(t, server, key, want)
		})
	}
	t.Run("denied reservation never attempts persistence", func(t *testing.T) {
		server, client := dailyQuotaRedis(t)
		st, svc, tenant := dailyQuotaService(t, client, 4, "success")
		key := dailyQuotaKey(tenant, time.Now())
		if err := server.Set(key, "4"); err != nil {
			t.Fatal(err)
		}
		if result, err := dailyQuotaAccept(svc); err == nil || result.Delivered != 0 || st.writes != 0 {
			t.Fatalf("daily limit bypass: result=%+v err=%v writes=%d", result, err, st.writes)
		}
		assertDailyQuota(t, server, key, "4")
	})
}

func TestR5DailyQuotaRollbackAcrossMidnight(t *testing.T) {
	for _, outcome := range []string{"metadata failure", "mailbox quota"} {
		t.Run(outcome, func(t *testing.T) {
			// Keep the real Redis endpoint and connection outside the clock bubble.
			// Only the service's calendar advances; no 24-hour wall wait is used.
			server, client := dailyQuotaRedis(t)
			synctest.Test(t, func(t *testing.T) {
				st, svc, tenant := dailyQuotaService(t, client, 10, outcome)
				now := time.Now().UTC()
				midnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
				time.Sleep(midnight.Add(-time.Second).Sub(now))
				oldKey, newKey := dailyQuotaKey(tenant, time.Now()), dailyQuotaKey(tenant, midnight)
				if oldKey == newKey {
					t.Fatal("midnight fixture did not select different days")
				}
				if err := server.Set(oldKey, "4"); err != nil {
					t.Fatal(err)
				}
				st.beforeWrite = func() {
					assertDailyQuota(t, server, oldKey, "5")
					time.Sleep(2 * time.Second)
					if dailyQuotaKey(tenant, time.Now()) != newKey {
						t.Fatal("failed persistence did not cross UTC midnight")
					}
					// Other accepts now own seven units in the new day.
					if err := server.Set(newKey, "7"); err != nil {
						t.Fatal(err)
					}
				}
				if result, err := dailyQuotaAccept(svc); err == nil || result.Delivered != 0 || st.writes != 1 {
					t.Fatalf("expected failed persistence across midnight: result=%+v err=%v writes=%d", result, err, st.writes)
				}
				assertDailyQuota(t, server, oldKey, "4")
				assertDailyQuota(t, server, newKey, "7")
			})
		})
	}
}
