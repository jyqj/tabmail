package ingest

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
)

type r5NextQuotaContextKey struct{}

type r5NextQuotaHook struct {
	process func(context.Context, redis.Cmder, redis.ProcessHook) error
}

func (h r5NextQuotaHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h r5NextQuotaHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h r5NextQuotaHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { return h.process(ctx, cmd, next) }
}

func r5NextQuotaAccept(ctx context.Context, svc *Service) (AcceptResult, error) {
	return svc.Accept(ctx, Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{"reader@mail.test"}}, []byte("Subject: canceled quota reservation\r\n\r\nowned message"))
}

// Cancellation occurs only after the real Redis increment is observed and at
// the metadata boundary. A failed/rejected write must return exactly its unit,
// even though the original request context has expired.
func TestR5NextDailyQuotaCancellationCompensation(t *testing.T) {
	for _, outcome := range []string{"metadata failure", "mailbox quota"} {
		for _, phase := range []string{"cancel", "deadline"} {
			for _, initial := range []int{0, 4} {
				t.Run(outcome+"/"+phase+"/initial-"+strconv.Itoa(initial), func(t *testing.T) {
					server, client := dailyQuotaRedis(t)
					st, svc, tenant := dailyQuotaService(t, client, 10, outcome)
					key := dailyQuotaKey(tenant, time.Now())
					if initial > 0 {
						if err := server.Set(key, strconv.Itoa(initial)); err != nil {
							t.Fatal(err)
						}
					}
					parent := context.WithValue(context.Background(), r5NextQuotaContextKey{}, "owned-accept")
					ctx, cancel := context.WithCancel(parent)
					if phase == "deadline" {
						cancel()
						ctx, cancel = context.WithTimeout(parent, 25*time.Millisecond)
					}
					defer cancel()
					compensating, releases := false, 0
					client.AddHook(r5NextQuotaHook{process: func(releaseCtx context.Context, cmd redis.Cmder, next redis.ProcessHook) error {
						if compensating && cmd.Name() == "eval" {
							releases++
							deadline, bounded := releaseCtx.Deadline()
							if releaseCtx.Err() != nil || !bounded || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || releaseCtx.Value(r5NextQuotaContextKey{}) != "owned-accept" {
								t.Errorf("compensation lost bounded live context or request value: err=%v deadline=%v", releaseCtx.Err(), deadline)
							}
						}
						return next(releaseCtx, cmd)
					}})
					st.beforeWrite = func() {
						assertDailyQuota(t, server, key, strconv.Itoa(initial+1))
						if phase == "deadline" {
							<-ctx.Done()
						} else {
							cancel()
						}
						compensating = true
					}
					result, err := r5NextQuotaAccept(ctx, svc)
					if err == nil || result.Delivered != 0 || st.writes != 1 || releases != 1 {
						t.Fatalf("failed message outcome changed: result=%+v err=%v writes=%d releases=%d", result, err, st.writes, releases)
					}
					if initial == 0 {
						if server.Exists(key) {
							t.Error("sole canceled reservation was not deleted")
						}
					} else {
						assertDailyQuota(t, server, key, strconv.Itoa(initial))
					}
				})
			}
		}
	}
}

func TestR5NextDailyQuotaCanceledRollbackKeepsObservedDay(t *testing.T) {
	for _, outcome := range []string{"metadata failure", "mailbox quota"} {
		t.Run(outcome, func(t *testing.T) {
			server, client := dailyQuotaRedis(t)
			synctest.Test(t, func(t *testing.T) {
				st, svc, tenant := dailyQuotaService(t, client, 10, outcome)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				now := time.Now().UTC()
				midnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
				time.Sleep(midnight.Add(-time.Second).Sub(now))
				oldKey, newKey := dailyQuotaKey(tenant, time.Now()), dailyQuotaKey(tenant, midnight)
				if err := server.Set(oldKey, "4"); err != nil {
					t.Fatal(err)
				}
				st.beforeWrite = func() {
					assertDailyQuota(t, server, oldKey, "5")
					time.Sleep(2 * time.Second)
					if err := server.Set(newKey, "7"); err != nil {
						t.Fatal(err)
					}
					cancel()
				}
				result, err := r5NextQuotaAccept(ctx, svc)
				if err == nil || result.Delivered != 0 || st.writes != 1 {
					t.Fatalf("expected canceled failure: %+v %v", result, err)
				}
				assertDailyQuota(t, server, oldKey, "4")
				assertDailyQuota(t, server, newKey, "7")
			})
		})
	}
}

type r5NextQuotaCommitStore struct {
	*dailyQuotaBoundaryStore
	afterCommit func()
}

func (s *r5NextQuotaCommitStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, maximum int, ensure func(context.Context) error) (bool, error) {
	ok, err := s.dailyQuotaBoundaryStore.CreateMessageWithQuota(ctx, m, maximum, ensure)
	if ok && err == nil {
		s.afterCommit()
	}
	return ok, err
}

func TestR5NextDailyQuotaAcceptedAndUnownedControls(t *testing.T) {
	for _, phase := range []string{"cancel", "deadline"} {
		t.Run("accepted/"+phase, func(t *testing.T) {
			server, client := dailyQuotaRedis(t)
			base, svc, tenant := dailyQuotaService(t, client, 10, "success")
			key := dailyQuotaKey(tenant, time.Now())
			if err := server.Set(key, "4"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if phase == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
			}
			defer cancel()
			st := &r5NextQuotaCommitStore{dailyQuotaBoundaryStore: base, afterCommit: func() {
				if phase == "deadline" {
					<-ctx.Done()
				} else {
					cancel()
				}
			}}
			svc.store, svc.objects = st, rawobject.NewStore(svc.obj, st)
			result, err := r5NextQuotaAccept(ctx, svc)
			if err != nil || result.Delivered != 1 || base.writes != 1 {
				t.Fatalf("committed message was undone: %+v %v", result, err)
			}
			assertDailyQuota(t, server, key, "5")
			mailbox, err := st.GetMailboxByAddress(context.Background(), "reader@mail.test")
			if err != nil {
				t.Fatal(err)
			}
			messages, total, err := st.ListMessages(context.Background(), mailbox.ID, models.Page{Page: 1, PerPage: 10})
			if err != nil || total != 1 || len(messages) != 1 {
				t.Fatalf("committed message missing: total=%d err=%v", total, err)
			}
		})
	}
	for _, outcome := range []string{"metadata failure", "mailbox quota"} {
		t.Run("unlimited/"+outcome, func(t *testing.T) {
			server, client := dailyQuotaRedis(t)
			st, svc, tenant := dailyQuotaService(t, client, 0, outcome)
			key := dailyQuotaKey(tenant, time.Now())
			if err := server.Set(key, "4"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			st.beforeWrite = cancel
			evals := 0
			client.AddHook(r5NextQuotaHook{process: func(ctx context.Context, cmd redis.Cmder, next redis.ProcessHook) error {
				if cmd.Name() == "eval" {
					evals++
				}
				return next(ctx, cmd)
			}})
			result, err := r5NextQuotaAccept(ctx, svc)
			if err == nil || result.Delivered != 0 || evals != 0 {
				t.Fatalf("unowned rollback changed Redis: %+v %v evals=%d", result, err, evals)
			}
			assertDailyQuota(t, server, key, "4")
		})
	}
}

func TestR5NextDailyQuotaCompensationFailureIsVisibleAndBounded(t *testing.T) {
	t.Run("Redis failure retains original delivery outcome and logs rollback", func(t *testing.T) {
		server, client := dailyQuotaRedis(t)
		st, svc, _ := dailyQuotaService(t, client, 10, "metadata failure")
		var output bytes.Buffer
		svc.logger = zerolog.New(&output)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		st.beforeWrite = func() { cancel(); server.Close() }
		result, err := r5NextQuotaAccept(ctx, svc)
		if err == nil || result.Delivered != 0 || !strings.Contains(output.String(), "synthetic message persistence failure") {
			t.Fatalf("rollback failure replaced metadata result: %+v %v", result, err)
		}
		if !strings.Contains(output.String(), "release tenant daily quota") {
			t.Fatal("failed compensation was silently discarded")
		}
	})
	t.Run("compensation has a fresh finite deadline", func(t *testing.T) {
		server, client := dailyQuotaRedis(t)
		synctest.Test(t, func(t *testing.T) {
			st, svc, tenant := dailyQuotaService(t, client, 10, "metadata failure")
			key := dailyQuotaKey(tenant, time.Now())
			if err := server.Set(key, "4"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			compensating := false
			st.beforeWrite = func() { cancel(); compensating = true }
			client.AddHook(r5NextQuotaHook{process: func(releaseCtx context.Context, cmd redis.Cmder, next redis.ProcessHook) error {
				if !compensating || cmd.Name() != "eval" {
					return next(releaseCtx, cmd)
				}
				deadline, ok := releaseCtx.Deadline()
				if !ok || releaseCtx.Err() != nil || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
					t.Error("rollback context is canceled or unbounded")
					return errors.New("invalid cleanup context")
				}
				<-releaseCtx.Done()
				if !errors.Is(releaseCtx.Err(), context.DeadlineExceeded) {
					t.Error("wrong compensation expiry")
				}
				return releaseCtx.Err()
			}})
			started := time.Now()
			result, err := r5NextQuotaAccept(ctx, svc)
			if err == nil || result.Delivered != 0 || time.Since(started) > 5*time.Second {
				t.Fatalf("compensation stalled or changed acceptance: elapsed=%v result=%+v err=%v", time.Since(started), result, err)
			}
			// The fixture proves a failure before Redis execution: do not invent
			// a successful release or debit some other request after timeout.
			assertDailyQuota(t, server, key, "5")
		})
	})
}
