package configcache_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

type delayedRouteStore struct {
	*testutil.FakeStore
	started, release chan struct{}
	calls            atomic.Int32
}

func (s *delayedRouteStore) ListRoutes(ctx context.Context, zone uuid.UUID) ([]*models.DomainRoute, error) {
	// The adapter has already obtained the old snapshot before the write commits.
	routes, err := s.FakeStore.ListRoutes(ctx, zone)
	if s.calls.Add(1) == 1 {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return routes, err
}

func TestR5ResolverRouteInvalidationSurvivesLateSnapshot(t *testing.T) {
	for _, initiallyMissing := range []bool{false, true} {
		for _, newerFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("negative=%v/newer-first=%v", initiallyMissing, newerFirst), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				st := &delayedRouteStore{FakeStore: testutil.NewFakeStore(), started: make(chan struct{}), release: make(chan struct{})}
				zone := &models.DomainZone{ID: uuid.New(), TenantID: uuid.New(), Domain: "mail.test", IsVerified: true, MXVerified: true}
				st.SeedZone(zone)
				route := &models.DomainRoute{ID: uuid.New(), ZoneID: zone.ID, RouteType: models.RouteExact, MatchValue: zone.Domain, AutoCreateMailbox: true}
				if !initiallyMissing {
					st.SeedRoute(route)
				}
				rv := resolver.New(st, policy.NamingFull, true)
				check := func() cacheReadResult {
					result, err := rv.Check(ctx, "employee@mail.test")
					allowed := 0
					if result != nil && result.Route != nil && result.Route.ID == route.ID {
						allowed = 1
					}
					return cacheReadResult{allowed, err}
				}
				oldResult := make(chan cacheReadResult, 1)
				go func() { oldResult <- check() }()
				receiveCacheResult(t, ctx, st.started)
				updated := *route
				updated.AutoCreateMailbox = initiallyMissing
				if err := st.CreateRoute(ctx, &updated); err != nil {
					t.Fatal(err)
				}
				rv.InvalidateRoutes(zone.ID)
				wantNew := 0
				if initiallyMissing {
					wantNew = 1
				}
				if newerFirst {
					if fresh := check(); fresh.err != nil || fresh.value != wantNew {
						t.Fatalf("fresh committed route was not visible: %+v", fresh)
					}
				}
				close(st.release)
				if old := receiveCacheResult(t, ctx, oldResult); old.err != nil || old.value != 1-wantNew {
					t.Fatalf("old caller did not complete its own snapshot: %+v", old)
				}
				if fresh := check(); fresh.err != nil || fresh.value != wantNew {
					t.Fatalf("late route snapshot reversed an invalidated setting: %+v", fresh)
				}
			})
		}
	}
}

type delayedPolicyStore struct {
	*testutil.FakeStore
	started, release chan struct{}
	calls            atomic.Int32
}

func (s *delayedPolicyStore) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	value, err := s.FakeStore.GetSMTPPolicy(ctx)
	if s.calls.Add(1) == 1 {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return value, err
}

func TestR5IngestPolicyInvalidationSurvivesLateSnapshot(t *testing.T) {
	for _, newerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("newer-first=%v", newerFirst), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			st := &delayedPolicyStore{FakeStore: testutil.NewFakeStore(), started: make(chan struct{}), release: make(chan struct{})}
			if err := st.UpsertSMTPPolicy(ctx, &models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}); err != nil {
				t.Fatal(err)
			}
			svc := ingest.NewService(st, testutil.NewMemoryObjectStore(), nil, nil, nil, models.SMTPPolicy{}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
			read := func() cacheReadResult {
				policy, err := svc.CurrentPolicy(ctx)
				allowed := 0
				if policy != nil && policy.DefaultAccept && policy.DefaultStore {
					allowed = 1
				}
				return cacheReadResult{allowed, err}
			}
			oldResult := make(chan cacheReadResult, 1)
			go func() { oldResult <- read() }()
			receiveCacheResult(t, ctx, st.started)
			if err := st.UpsertSMTPPolicy(ctx, &models.SMTPPolicy{DefaultAccept: false, DefaultStore: false}); err != nil {
				t.Fatal(err)
			}
			svc.InvalidateSMTPPolicy()
			if newerFirst {
				if fresh := read(); fresh.err != nil || fresh.value != 0 {
					t.Fatalf("fresh policy was not visible: %+v", fresh)
				}
			}
			close(st.release)
			if old := receiveCacheResult(t, ctx, oldResult); old.err != nil || old.value != 1 {
				t.Fatalf("old caller did not complete its own policy snapshot: %+v", old)
			}
			if fresh := read(); fresh.err != nil || fresh.value != 0 {
				t.Fatalf("late policy load restored the revoked acceptance policy: %+v", fresh)
			}
		})
	}
}
