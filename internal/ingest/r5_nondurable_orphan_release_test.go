package ingest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/retention"
	"tabmail/internal/testutil"
)

// The real Accept/resolver/rawobject chain owns the decision. These adapters
// expose commit ambiguity and handoff failures; they are not PostgreSQL runtime
// evidence. FakeStore's release locks its message/job references and callback.
type orphanAcceptStore struct {
	*testutil.FakeStore
	cfg                 models.EffectiveConfig
	configErr, writeErr error
	persistBeforeError  bool
	quotaDenied         bool
	dailyCount          int
	releases, writes    int
	queued              []string
	release             func(context.Context, string) error
	beforeRelease       func()
	afterRelease        func()
	enqueue             func(context.Context, string) error
	assertHandoff       func(context.Context)
}

func (s *orphanAcceptStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &s.cfg, s.configErr
}
func (s *orphanAcceptStore) CountTenantMessagesSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return s.dailyCount, nil
}
func (s *orphanAcceptStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, limit int, ensure func(context.Context) error) (bool, error) {
	s.writes++
	if s.quotaDenied {
		return false, nil
	}
	if s.writeErr != nil && !s.persistBeforeError {
		return false, s.writeErr
	}
	ok, err := s.FakeStore.CreateMessageWithQuota(ctx, m, limit, ensure)
	if err != nil {
		return ok, err
	}
	return ok, s.writeErr
}
func (s *orphanAcceptStore) ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error) {
	s.releases++
	if s.beforeRelease != nil {
		s.beforeRelease()
	}
	if s.release != nil {
		if err := s.release(ctx, key); err != nil {
			return false, err
		}
	}
	deleted, err := s.FakeStore.ReleaseRawObjectIfUnreferenced(ctx, key, del)
	if s.afterRelease != nil {
		s.afterRelease()
	}
	return deleted, err
}
func (s *orphanAcceptStore) EnqueueOrphanRetry(ctx context.Context, key string) error {
	s.queued = append(s.queued, key)
	if s.assertHandoff != nil {
		s.assertHandoff(ctx)
	}
	if s.enqueue != nil {
		if err := s.enqueue(ctx, key); err != nil {
			return err
		}
	}
	return s.FakeStore.EnqueueOrphanRetry(ctx, key)
}

type orphanAcceptObjects struct {
	*testutil.MemoryObjectStore
	deletes  int
	delete   func(context.Context) error
	putErr   error
	afterPut func()
}

func (o *orphanAcceptObjects) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := o.MemoryObjectStore.Put(ctx, key, r, size); err != nil {
		return err
	}
	if o.afterPut != nil {
		o.afterPut()
	}
	return o.putErr
}
func (o *orphanAcceptObjects) Delete(ctx context.Context, key string) error {
	o.deletes++
	if o.delete != nil {
		if err := o.delete(ctx); err != nil {
			return err
		}
	}
	return o.MemoryObjectStore.Delete(ctx, key)
}

type orphanAcceptFixture struct {
	svc *Service
	st  *orphanAcceptStore
	obj *orphanAcceptObjects
	mb  *models.Mailbox
	raw []byte
	log bytes.Buffer
}

func newOrphanAcceptFixture(t *testing.T) *orphanAcceptFixture {
	t.Helper()
	f := &orphanAcceptFixture{raw: []byte("Subject: release ownership\r\n\r\nsynthetic unused raw")}
	f.st = &orphanAcceptStore{FakeStore: testutil.NewFakeStore(), cfg: models.EffectiveConfig{
		MaxMessageBytes: 1024, MaxMessagesPerMailbox: 100, DailyQuota: 100, RetentionHours: 24,
	}}
	f.obj = &orphanAcceptObjects{MemoryObjectStore: testutil.NewMemoryObjectStore()}
	tenant, zone := uuid.New(), uuid.New()
	f.st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
	f.mb = &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
	f.st.SeedMailbox(f.mb)
	f.svc = NewService(f.st, f.obj, resolver.New(f.st, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{}, zerolog.New(&f.log))
	return f
}
func (f *orphanAcceptFixture) accept(ctx context.Context, addresses ...string) (AcceptResult, error) {
	if len(addresses) == 0 {
		addresses = []string{f.mb.FullAddress}
	}
	return f.svc.Accept(ctx, Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: addresses}, f.raw)
}
func (f *orphanAcceptFixture) assertRejected(t *testing.T, ctx context.Context, addresses ...string) {
	t.Helper()
	result, err := f.accept(ctx, addresses...)
	if err == nil || result.Queued || result.Delivered != 0 {
		t.Errorf("cleanup changed failed acceptance: result=%+v error=%v", result, err)
	}
}
func (f *orphanAcceptFixture) assertState(t *testing.T, releases, deletes, objects int, queued bool) {
	t.Helper()
	if f.st.releases != releases || f.obj.deletes != deletes || f.obj.Count() != objects {
		t.Errorf("release/delete/objects=%d/%d/%d, want %d/%d/%d", f.st.releases, f.obj.deletes, f.obj.Count(), releases, deletes, objects)
	}
	var want []string
	if queued {
		want = []string{rawobject.Key(f.raw)}
	}
	keys, err := f.st.ListPendingOrphanRetries(context.Background(), 10)
	if err != nil || len(keys) != len(want) || (queued && !reflect.DeepEqual(keys, want)) {
		t.Errorf("durable retry keys=%v error=%v, want %v", keys, err, want)
	}
}

func TestR5NonDurableOrphanRelease(t *testing.T) {
	for _, name := range []string{"store policy", "unknown route", "unverified zone", "size", "config error", "daily quota", "mailbox quota", "metadata error"} {
		t.Run(name, func(t *testing.T) {
			f := newOrphanAcceptFixture(t)
			address := f.mb.FullAddress
			switch name {
			case "store policy":
				f.svc.defaultPolicy.DefaultStore = false
			case "unknown route":
				address = "missing@unknown.test"
			case "unverified zone":
				f.st.SeedZone(&models.DomainZone{ID: f.mb.ZoneID, TenantID: f.mb.TenantID, Domain: "mail.test"})
			case "size":
				f.st.cfg.MaxMessageBytes = 1
			case "config error":
				f.st.configErr = errors.New("synthetic configuration read failure")
			case "daily quota":
				f.st.dailyCount = f.st.cfg.DailyQuota
			case "mailbox quota":
				f.st.quotaDenied = true
			case "metadata error":
				f.st.writeErr = errors.New("synthetic metadata write failure")
			}
			f.assertRejected(t, context.Background(), address)
			f.assertState(t, 1, 1, 0, false)
			if len(f.st.queued) != 0 {
				t.Error("successful release unnecessarily enqueued a retry")
			}
		})
	}
}

func TestR5NonDurableOrphanReferences(t *testing.T) {
	for _, name := range []string{"existing message", "existing ingest job", "committed but error", "concurrent message before reference lock"} {
		t.Run(name, func(t *testing.T) {
			f := newOrphanAcceptFixture(t)
			f.svc.defaultPolicy.DefaultStore = false
			message := func() *models.Message {
				return &models.Message{ID: uuid.New(), MailboxID: f.mb.ID, RawObjectKey: rawobject.Key(f.raw)}
			}
			switch name {
			case "existing message":
				f.st.SeedMessage(message())
			case "existing ingest job":
				if err := f.st.CreateIngestJob(context.Background(), &models.IngestJob{ID: uuid.New(), RawObjectKey: rawobject.Key(f.raw), State: "pending"}, nil); err != nil {
					t.Fatal(err)
				}
			case "committed but error":
				f.svc.defaultPolicy.DefaultStore = true
				f.st.persistBeforeError, f.st.writeErr = true, errors.New("synthetic uncertain commit response")
			case "concurrent message before reference lock":
				start, done := make(chan struct{}), make(chan struct{})
				started := false
				go func() { <-start; f.st.SeedMessage(message()); close(done) }()
				f.st.beforeRelease = func() { started = true; close(start); <-done }
				t.Cleanup(func() {
					if !started {
						close(start)
					}
					<-done
				})
			}
			f.assertRejected(t, context.Background())
			f.assertState(t, 1, 0, 1, false)
			if name == "committed but error" {
				messages, count, err := f.st.ListMessages(context.Background(), f.mb.ID, models.Page{Page: 1, PerPage: 10})
				if err != nil || count != 1 || len(messages) != 1 || messages[0].RawObjectKey != rawobject.Key(f.raw) {
					t.Errorf("fixture did not commit the uncertain reference: %v/%d/%v", messages, count, err)
				}
			}
		})
	}
}

type orphanContextKey struct{}

func TestR5NonDurableOrphanHandoff(t *testing.T) {
	for _, name := range []string{"reference check failure", "delete failure", "cancel after put", "cancel during count", "cancel during delete", "cancel after release"} {
		t.Run(name, func(t *testing.T) {
			f := newOrphanAcceptFixture(t)
			f.svc.defaultPolicy.DefaultStore = false
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), orphanContextKey{}, "request-value"))
			defer cancel()
			releases, deletes, objects := 1, 0, 1
			switch name {
			case "reference check failure":
				f.st.release = func(context.Context, string) error { return errors.New("synthetic count failure") }
			case "delete failure":
				deletes = 1
				f.obj.delete = func(context.Context) error { return errors.New("synthetic delete failure") }
			case "cancel after put":
				releases = 0
				f.obj.afterPut = cancel
			case "cancel during count":
				f.st.release = func(ctx context.Context, _ string) error { cancel(); return ctx.Err() }
			case "cancel during delete":
				deletes = 1
				f.obj.delete = func(ctx context.Context) error { cancel(); return ctx.Err() }
			case "cancel after release":
				deletes, objects = 1, 0
				f.st.afterRelease = cancel
			}
			f.st.assertHandoff = func(handoff context.Context) {
				t.Helper()
				deadline, ok := handoff.Deadline()
				if handoff.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || handoff.Value(orphanContextKey{}) != "request-value" {
					t.Errorf("handoff lost bounded live context: error=%v deadline=%v value=%v", handoff.Err(), deadline, handoff.Value(orphanContextKey{}))
				}
			}
			f.assertRejected(t, ctx)
			f.assertState(t, releases, deletes, objects, true)
			if !reflect.DeepEqual(f.st.queued, []string{rawobject.Key(f.raw)}) {
				t.Errorf("handoff calls=%v, want exact one content key", f.st.queued)
			}
		})
	}
}

func TestR5NonDurableOrphanBudgets(t *testing.T) {
	for _, phase := range []string{"release", "handoff"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newOrphanAcceptFixture(t)
				f.svc.defaultPolicy.DefaultStore = false
				bounded := func(ctx context.Context) error {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) != 5*time.Second {
						t.Errorf("missing exact 5-second budget: %v %v", deadline, ok)
						return errors.New("unbounded operation rejected by fixture")
					}
					<-ctx.Done()
					return ctx.Err()
				}
				f.st.release = func(ctx context.Context, _ string) error {
					if phase == "release" {
						return bounded(ctx)
					}
					return errors.New("synthetic count failure")
				}
				if phase == "handoff" {
					f.st.enqueue = func(ctx context.Context, _ string) error { return bounded(ctx) }
				}
				start := time.Now()
				f.assertRejected(t, context.Background())
				if time.Since(start) != 5*time.Second || len(f.st.queued) != 1 {
					t.Errorf("bounded handoff did not finish once: elapsed=%s queued=%v", time.Since(start), f.st.queued)
				}
				f.assertState(t, 1, 0, 1, phase == "release")
				if phase == "handoff" && !strings.Contains(f.log.String(), "context deadline exceeded") {
					t.Error("failed handoff was not visible in log")
				}
			})
		})
	}
}

func TestR5NonDurableOrphanHandoffFailureVisible(t *testing.T) {
	f := newOrphanAcceptFixture(t)
	f.svc.defaultPolicy.DefaultStore = false
	f.st.release = func(context.Context, string) error { return errors.New("synthetic release failure") }
	f.st.enqueue = func(context.Context, string) error { return errors.New("synthetic retry persistence failure") }
	f.assertRejected(t, context.Background())
	f.assertState(t, 1, 0, 1, false)
	if len(f.st.queued) != 1 || !strings.Contains(f.log.String(), "synthetic retry persistence failure") {
		t.Errorf("handoff failure was silently lost: queued=%v log=%s", f.st.queued, f.log.String())
	}
}

func TestR5NonDurableOrphanRetryResumesInScanner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newOrphanAcceptFixture(t)
		f.svc.defaultPolicy.DefaultStore = false
		f.obj.delete = func(context.Context) error { return errors.New("synthetic transient blob failure") }
		f.assertRejected(t, context.Background())
		f.assertState(t, 1, 1, 1, true)
		// A new scanner owns the persisted candidate after the acceptance call
		// has returned; no in-memory retry loop or original request is reused.
		f.obj.delete = nil
		scanner := retention.New(rawobject.NewStore(f.obj, f.st), f.st,
			config.Storage{RetentionScanInterval: time.Second, RetentionBatchSize: 10}, zerolog.Nop())
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { scanner.Run(ctx); close(done) }()
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		cancel()
		<-done
		f.assertState(t, 2, 2, 0, false)
	})
}

func TestR5NonDurableOrphanControls(t *testing.T) {
	for _, name := range []string{"stored", "mixed stored and rejected", "uncertain initial put", "durable accepted"} {
		t.Run(name, func(t *testing.T) {
			f := newOrphanAcceptFixture(t)
			addresses := []string{f.mb.FullAddress}
			switch name {
			case "mixed stored and rejected":
				addresses = append(addresses, "missing@unknown.test")
			case "uncertain initial put":
				f.obj.putErr = errors.New("synthetic uncertain blob publication")
			case "durable accepted":
				f.svc.durable = true
			}
			result, err := f.accept(context.Background(), addresses...)
			switch name {
			case "uncertain initial put":
				if err == nil || result.Queued || result.Delivered != 0 {
					t.Errorf("uncertain put acknowledged: %+v %v", result, err)
				}
			case "durable accepted":
				if err != nil || !result.Queued || result.Delivered != 0 {
					t.Errorf("durable acceptance changed: %+v %v", result, err)
				}
			default:
				if err != nil || result.Queued || result.Delivered != 1 {
					t.Errorf("real delivery changed: %+v %v", result, err)
				}
			}
			f.assertState(t, 0, 0, 1, false)
		})
	}
}
