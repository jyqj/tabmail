package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"tabmail/internal/autocreate"
)

type windowConsumer struct {
	name string
	key  string
	call func(context.Context) (bool, error)
}

func windowConsumers(client *redis.Client, limit int) []windowConsumer {
	tenant := uuid.MustParse("a4aa4393-6974-40c8-9e20-ad00a681c0f3")
	route := uuid.MustParse("049e1cff-5d6d-4a4a-af04-1512c1576c9a")
	routeLimiter := autocreate.NewLimiter(client, limit, 0)
	tenantLimiter := autocreate.NewLimiter(client, 0, limit)
	apiLimiter := NewRateLimiter(client, nil, limit, nil)
	return []windowConsumer{
		{"api address", "rate:token:person@example.test", func(ctx context.Context) (bool, error) {
			return apiLimiter.CheckAddressRateLimit(ctx, " Person@Example.test ", limit, time.Minute)
		}},
		{"autocreate route", "autocreate:route:" + route.String(), func(ctx context.Context) (bool, error) {
			return routeLimiter.Allow(ctx, tenant, route)
		}},
		{"autocreate tenant", "autocreate:tenant:" + tenant.String(), func(ctx context.Context) (bool, error) {
			return tenantLimiter.Allow(ctx, tenant, route)
		}},
	}
}

func windowRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, PoolSize: 16})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestR5SlidingWindowCountsSameMillisecondAttempts(t *testing.T) {
	for i, name := range []string{"api address", "autocreate route", "autocreate tenant"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				server, client := windowRedis(t)
				consumer := windowConsumers(client, 3)[i]
				instant := time.Now().UnixMilli()
				for attempt := 0; attempt < 6; attempt++ {
					allowed, err := consumer.call(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if allowed != (attempt < 3) {
						t.Errorf("attempt %d allowed=%v, limit must count each attempt", attempt+1, allowed)
					}
				}
				if time.Now().UnixMilli() != instant {
					t.Fatal("test did not hold all attempts in one millisecond")
				}
				members, err := server.ZMembers(consumer.key)
				if err != nil || len(members) != 6 {
					t.Errorf("attempt history collapsed or dropped denied requests: members=%d err=%v", len(members), err)
				}
				if ttl := server.TTL(consumer.key); ttl != time.Minute+time.Second {
					t.Errorf("window expiry changed: %v", ttl)
				}
			})
		})
	}
}

// Ordinary Redis pipelines provide no inter-client isolation. Split only the
// limiter pipeline and pause after its real ZCARD reply, making a valid server
// interleaving deterministic. Atomic Lua commands pass through untouched.
type windowReadBarrier struct {
	read    chan struct{}
	release chan struct{}
}

func (*windowReadBarrier) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (*windowReadBarrier) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (b *windowReadBarrier) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		target := false
		for _, cmd := range cmds {
			target = target || cmd.Name() == "zcard"
		}
		if !target {
			return next(ctx, cmds)
		}
		for _, cmd := range cmds {
			if err := next(ctx, []redis.Cmder{cmd}); err != nil {
				return err
			}
			if cmd.Name() == "zcard" {
				b.read <- struct{}{}
				select {
				case <-b.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return nil
	}
}

func TestR5SlidingWindowConcurrentAdmission(t *testing.T) {
	for i, name := range []string{"api address", "autocreate route", "autocreate tenant"} {
		t.Run(name, func(t *testing.T) {
			const attempts = 8
			server, client := windowRedis(t)
			barrier := &windowReadBarrier{read: make(chan struct{}, attempts), release: make(chan struct{})}
			client.AddHook(barrier)
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
			defer release()
			consumer := windowConsumers(client, 3)[i]
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type outcome struct {
				allowed bool
				err     error
			}
			done := make(chan outcome, attempts)
			for n := 0; n < attempts; n++ {
				go func() { allowed, err := consumer.call(ctx); done <- outcome{allowed, err} }()
			}
			var outcomes []outcome
			// Before the fix each request reaches the barrier. With one atomic
			// operation each request completes directly, without test hooks.
			for seen := 0; seen < attempts; seen++ {
				select {
				case <-barrier.read:
				case result := <-done:
					outcomes = append(outcomes, result)
				case <-ctx.Done():
					t.Fatal("requests did not reach their admission boundary")
				}
			}
			release()
			for len(outcomes) < attempts {
				select {
				case result := <-done:
					outcomes = append(outcomes, result)
				case <-ctx.Done():
					t.Fatal("requests did not finish")
				}
			}
			accepted := 0
			for _, result := range outcomes {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.allowed {
					accepted++
				}
			}
			if accepted != 3 {
				t.Errorf("concurrent admissions=%d, want exactly 3", accepted)
			}
			members, err := server.ZMembers(consumer.key)
			if err != nil || len(members) != attempts {
				t.Errorf("attempt count=%d err=%v", len(members), err)
			}
		})
	}
}

func TestR5SlidingWindowExpiryBoundaryAndDeniedHistory(t *testing.T) {
	for i, name := range []string{"api address", "autocreate route", "autocreate tenant"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				server, client := windowRedis(t)
				consumer := windowConsumers(client, 1)[i]
				now := time.Now().UnixMilli()
				if err := client.ZAdd(context.Background(), consumer.key,
					redis.Z{Score: float64(now - time.Minute.Milliseconds()), Member: "expired-exactly-at-cutoff"},
					redis.Z{Score: float64(now - 1), Member: "recent-attempt"},
				).Err(); err != nil {
					t.Fatal(err)
				}
				allowed, err := consumer.call(context.Background())
				if err != nil || allowed {
					t.Fatalf("recent request must retain its limit: allowed=%v err=%v", allowed, err)
				}
				members, err := server.ZMembers(consumer.key)
				if err != nil || len(members) != 2 {
					t.Fatalf("expiry/denied history changed: %v %v", members, err)
				}
				for _, member := range members {
					if member == "expired-exactly-at-cutoff" {
						t.Fatal("inclusive cutoff was not pruned")
					}
				}
			})
		})
	}
}

func TestR5SlidingWindowAutocreatePreservesTwoLevelAccounting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, client := windowRedis(t)
		tenant, routeA, routeB := uuid.New(), uuid.New(), uuid.New()
		limiter := autocreate.NewLimiter(client, 1, 1)
		for i, route := range []uuid.UUID{routeA, routeA, routeB} {
			allowed, err := limiter.Allow(context.Background(), tenant, route)
			if err != nil || allowed != (i == 0) {
				t.Errorf("attempt=%d allowed=%v err=%v", i+1, allowed, err)
			}
		}
		for key, want := range map[string]int{
			"autocreate:route:" + routeA.String():  2,
			"autocreate:route:" + routeB.String():  1,
			"autocreate:tenant:" + tenant.String(): 2,
		} {
			members, err := server.ZMembers(key)
			if err != nil || len(members) != want {
				t.Errorf("key=%s attempts=%d want=%d err=%v", key, len(members), want, err)
			}
		}
	})
}

func TestR5SlidingWindowPreservesRedisFailurePolicies(t *testing.T) {
	server, client := windowRedis(t)
	for _, consumer := range windowConsumers(client, 1) {
		t.Run(consumer.name, func(t *testing.T) {
			if err := server.Set(consumer.key, "wrong Redis value type"); err != nil {
				t.Fatal(err)
			}
			if allowed, err := consumer.call(context.Background()); allowed || err == nil {
				t.Fatalf("storage failure lost: allowed=%v err=%v", allowed, err)
			}
		})
	}
	t.Run("HTTP retains its existing Redis failure fallback", func(t *testing.T) {
		const ip = "198.51.100.17"
		if err := server.Set("rate:ip:"+ip, "wrong Redis value type"); err != nil {
			t.Fatal(err)
		}
		called := 0
		handler := NewRateLimiter(client, nil, 1, nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called++; w.WriteHeader(http.StatusNoContent) }))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = fmt.Sprintf("%s:51234", ip)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if called != 1 || recorder.Code != http.StatusNoContent {
			t.Fatalf("existing HTTP failure policy changed: calls=%d status=%d", called, recorder.Code)
		}
	})
}
