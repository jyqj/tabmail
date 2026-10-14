package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

// These wrappers observe the formal Accept/resolver/object/ledger path. The
// embedded fake is not evidence for PostgreSQL transaction/commit guarantees.
type r5BoundaryStore struct {
	*testutil.FakeStore
	calls              *[]string
	resolveErr         error
	resolveHook        func(string, int) (*models.Mailbox, error)
	resolveCalls       int
	createErr          error
	persistBeforeError bool
	beforeCreate       func()
	job                *models.IngestJob
	targets            []store.IngressTarget
	hash               string
	size               int64
}

func (s *r5BoundaryStore) GetMailboxByAddress(ctx context.Context, addr string) (*models.Mailbox, error) {
	*s.calls = append(*s.calls, "resolve:"+addr)
	s.resolveCalls++
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	if s.resolveHook != nil {
		return s.resolveHook(addr, s.resolveCalls)
	}
	return s.FakeStore.GetMailboxByAddress(ctx, addr)
}
func (s *r5BoundaryStore) CreateIngress(ctx context.Context, j *models.IngestJob, targets []store.IngressTarget, hash string, size int64) error {
	*s.calls = append(*s.calls, "ledger")
	cp := *j
	cp.Recipients = append([]string(nil), j.Recipients...)
	cp.Metadata = append([]byte(nil), j.Metadata...)
	s.job, s.targets, s.hash, s.size = &cp, append([]store.IngressTarget(nil), targets...), hash, size
	if s.beforeCreate != nil {
		s.beforeCreate()
	}
	if s.createErr != nil && !s.persistBeforeError {
		return s.createErr
	}
	if err := s.FakeStore.CreateIngress(ctx, j, targets, hash, size); err != nil {
		return err
	}
	return s.createErr
}

type r5BoundaryObjects struct {
	*testutil.MemoryObjectStore
	calls    *[]string
	putErr   error
	afterPut func()
	deletes  int
}

func (o *r5BoundaryObjects) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	*o.calls = append(*o.calls, "put")
	if o.putErr != nil {
		return o.putErr
	}
	if err := o.MemoryObjectStore.Put(ctx, key, r, size); err != nil {
		return err
	}
	if o.afterPut != nil {
		o.afterPut()
	}
	return nil
}
func (o *r5BoundaryObjects) Delete(ctx context.Context, key string) error {
	o.deletes++
	*o.calls = append(*o.calls, "delete")
	return o.MemoryObjectStore.Delete(ctx, key)
}

type r5BoundaryFixture struct {
	svc   *Service
	st    *r5BoundaryStore
	obj   *r5BoundaryObjects
	calls []string
	mb    *models.Mailbox
	raw   []byte
}

func newR5BoundaryFixture(t *testing.T) *r5BoundaryFixture {
	t.Helper()
	f := &r5BoundaryFixture{raw: []byte("Subject: immutable\r\n\r\noriginal bytes")}
	f.st = &r5BoundaryStore{FakeStore: testutil.NewFakeStore(), calls: &f.calls}
	f.obj = &r5BoundaryObjects{MemoryObjectStore: testutil.NewMemoryObjectStore(), calls: &f.calls}
	planID, tenantID, zoneID := uuid.New(), uuid.New(), uuid.New()
	f.st.SeedPlan(&models.Plan{ID: planID, MaxMessagesPerMailbox: 100, MaxMessageBytes: 1024 * 1024, RetentionHours: 24, DailyQuota: 100})
	f.st.SeedTenant(&models.Tenant{ID: tenantID, PlanID: planID})
	f.st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	f.mb = &models.Mailbox{ID: uuid.New(), TenantID: tenantID, ZoneID: zoneID, LocalPart: "user", ResolvedDomain: "mail.test", FullAddress: "user@mail.test", AccessMode: models.AccessAPIKey}
	f.st.SeedMailbox(f.mb)
	f.svc = NewService(f.st, f.obj, resolver.New(f.st, policy.NamingFull, true), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	return f
}
func (f *r5BoundaryFixture) accept(addresses ...string) (AcceptResult, error) {
	return f.svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: addresses}, f.raw)
}
func (f *r5BoundaryFixture) assertRaw(t *testing.T) {
	t.Helper()
	if f.st.job == nil {
		t.Fatal("ledger did not see a receipt")
	}
	key := f.st.job.RawObjectKey
	if key != "ingress-"+f.st.job.ID.String()+".eml" {
		t.Fatalf("mutable/shared key: %s", key)
	}
	r, err := f.obj.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	closeErr := r.Close()
	if err != nil || closeErr != nil || !bytes.Equal(b, f.raw) {
		t.Fatalf("original bytes not preserved: %q %v %v", b, err, closeErr)
	}
	if f.st.hash != fmt.Sprintf("%x", sha256.Sum256(f.raw)) || f.st.size != int64(len(f.raw)) {
		t.Fatal("receipt hash/size not bound to original")
	}
	if f.obj.deletes != 0 {
		t.Fatal("accept deleted possibly referenced bytes")
	}
}
func TestR5AcceptBoundaryEmptyTargetsNotAcknowledged(t *testing.T) {
	f := newR5BoundaryFixture(t)
	result, err := f.accept()
	// Current API intentionally permits an empty no-op. Neither returned field
	// grants acknowledgement; SMTP Data rejects zero valid/stored recipients.
	if err != nil || result.Queued || result.Delivered != 0 || len(f.calls) != 0 || f.obj.Count() != 0 {
		t.Fatalf("empty accept performed work/ack: %+v %v %v", result, err, f.calls)
	}
}
func TestR5AcceptBoundaryResolveFailureBeforePut(t *testing.T) {
	for _, mode := range []string{"error", "missing", "unverified", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			f := newR5BoundaryFixture(t)
			address := f.mb.FullAddress
			switch mode {
			case "error":
				f.st.resolveErr = errors.New("injected resolve failure")
			case "missing":
				address = "missing@mail.test"
			case "unverified":
				z, _ := f.st.GetZone(context.Background(), f.mb.ZoneID)
				z.MXVerified = false
				f.st.SeedZone(z)
			case "invalid":
				address = "not-an-address"
			}
			result, err := f.accept(address)
			if err == nil || result.Queued || result.Delivered != 0 || f.obj.Count() != 0 || f.st.job != nil {
				t.Fatalf("resolve failure acknowledged/persisted: %+v %v %v", result, err, f.calls)
			}
			for _, call := range f.calls {
				if call == "put" || call == "ledger" {
					t.Fatalf("resolve failure crossed persistence boundary: %v", f.calls)
				}
			}
		})
	}
}
func TestR5AcceptBoundaryPutFailureNoLedger(t *testing.T) {
	f := newR5BoundaryFixture(t)
	injected := errors.New("injected put failure")
	f.obj.putErr = injected
	result, err := f.accept(f.mb.FullAddress)
	if !errors.Is(err, injected) || result.Queued || f.st.job != nil || f.obj.Count() != 0 || !reflect.DeepEqual(f.calls, []string{"resolve:user@mail.test", "put"}) {
		t.Fatalf("put failure crossed ledger: %+v %v %v", result, err, f.calls)
	}
}
func TestR5AcceptBoundaryLedgerErrorRetainsOriginal(t *testing.T) {
	for _, persist := range []bool{false, true} {
		t.Run(fmt.Sprintf("persist_before_error_%v", persist), func(t *testing.T) {
			f := newR5BoundaryFixture(t)
			injected := errors.New("injected commit response failure")
			f.st.createErr = injected
			f.st.persistBeforeError = persist
			result, err := f.accept(f.mb.FullAddress)
			if !errors.Is(err, injected) || result.Queued || result.Delivered != 0 || !reflect.DeepEqual(f.calls, []string{"resolve:user@mail.test", "put", "ledger"}) {
				t.Fatalf("ledger error acknowledged: %+v %v %v", result, err, f.calls)
			}
			f.assertRaw(t)
			targets, err := f.st.ListIngressTargets(context.Background(), f.st.job.ID)
			if err != nil || (len(targets) > 0) != persist {
				t.Fatalf("fault fixture did not exercise intended persistence: %v %v", targets, err)
			}
		})
	}
}
func TestR5AcceptBoundaryAliasesFreezeOneTarget(t *testing.T) {
	f := newR5BoundaryFixture(t)
	addresses := []string{"user@mail.test", "USER@mail.test", "user+tag@mail.test", "user+tag@mail.test"}
	result, err := f.accept(addresses...)
	if err != nil || !result.Queued || result.Delivered != 0 {
		t.Fatalf("accept: %+v %v", result, err)
	}
	f.assertRaw(t)
	if len(f.st.targets) != 1 {
		t.Fatalf("duplicate aliases created %d destinations", len(f.st.targets))
	}
	target := f.st.targets[0]
	if target.MailboxID != f.mb.ID || target.TenantID != f.mb.TenantID || target.ZoneID != f.mb.ZoneID || target.Address != f.mb.FullAddress {
		t.Fatalf("target not frozen: %+v", target)
	}
	if !reflect.DeepEqual(f.st.job.Recipients, addresses) {
		t.Fatal("original envelope recipients changed")
	}
	if f.calls[len(f.calls)-2] != "put" || f.calls[len(f.calls)-1] != "ledger" {
		t.Fatalf("wrong persistence order: %v", f.calls)
	}
}
func TestR5AcceptBoundaryRepeatedAddressResolvedOnce(t *testing.T) {
	f := newR5BoundaryFixture(t)
	other := *f.mb
	other.ID = uuid.New()
	other.FullAddress = "other@mail.test"
	other.LocalPart = "other"
	f.st.SeedMailbox(&other)
	f.st.resolveHook = func(_ string, n int) (*models.Mailbox, error) {
		if n == 1 {
			cp := *f.mb
			return &cp, nil
		}
		cp := other
		return &cp, nil
	}
	result, err := f.accept("user@mail.test", "USER@mail.test")
	if err != nil || !result.Queued {
		t.Fatalf("accept: %+v %v", result, err)
	}
	if f.st.resolveCalls != 1 || len(f.st.targets) != 1 || f.st.targets[0].MailboxID != f.mb.ID {
		t.Fatalf("one normalized recipient resolved to multiple identities: calls=%d targets=%+v", f.st.resolveCalls, f.st.targets)
	}
}
func TestR5AcceptBoundaryDestinationChangesBeforeLedgerRejected(t *testing.T) {
	f := newR5BoundaryFixture(t)
	f.obj.afterPut = func() { cp := *f.mb; cp.FullAddress = "renamed@mail.test"; f.st.SeedMailbox(&cp) }
	result, err := f.accept(f.mb.FullAddress)
	if err == nil || result.Queued || len(f.st.targets) != 1 || f.st.targets[0].Address != f.mb.FullAddress {
		t.Fatalf("changed target acknowledged/refrozen: %+v %v %+v", result, err, f.st.targets)
	}
	f.assertRaw(t)
	targets, _ := f.st.ListIngressTargets(context.Background(), f.st.job.ID)
	if len(targets) != 0 {
		t.Fatal("rejected receipt persisted in fault fixture")
	}
}
func TestR5AcceptBoundaryDeletedAddressRecreatedNeverRedirects(t *testing.T) {
	f := newR5BoundaryFixture(t)
	result, err := f.accept(f.mb.FullAddress)
	if err != nil || !result.Queued {
		t.Fatalf("accept: %+v %v", result, err)
	}
	ctx := context.Background()
	if err := f.st.DeleteMailbox(ctx, f.mb.ID); err != nil {
		t.Fatal(err)
	}
	replacement := *f.mb
	replacement.ID = uuid.New()
	f.st.SeedMailbox(&replacement)
	// Recovery must not resolve the original address against the replacement.
	f.st.resolveErr = errors.New("recovery must not re-resolve accepted address")
	callsBefore := f.st.resolveCalls
	f.svc.ProcessBatch(ctx)
	if f.st.resolveCalls != callsBefore {
		t.Fatal("recovery resolved mutable envelope addresses")
	}
	targets, err := f.st.ListIngressTargets(ctx, f.st.job.ID)
	if err != nil || len(targets) != 1 || targets[0].MailboxID != f.mb.ID || targets[0].State != "held" || !strings.Contains(targets[0].LastError, "destination changed") {
		t.Fatalf("accepted destination redirected/not held: %+v %v", targets, err)
	}
	messages, total, err := f.st.ListMessages(ctx, replacement.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil || total != 0 || len(messages) != 0 {
		t.Fatalf("old receipt delivered into replacement: %+v %d %v", messages, total, err)
	}
	f.assertRaw(t)
}

func TestR5AcceptBoundaryDistinctReceiptsNeverShareReclaimableObject(t *testing.T) {
	f := newR5BoundaryFixture(t)
	result, err := f.accept(f.mb.FullAddress)
	if err != nil || !result.Queued {
		t.Fatalf("first accept: %+v %v", result, err)
	}
	first := *f.st.job
	result, err = f.accept(f.mb.FullAddress)
	if err != nil || !result.Queued {
		t.Fatalf("second accept: %+v %v", result, err)
	}
	if first.ID == f.st.job.ID || first.RawObjectKey == f.st.job.RawObjectKey || f.obj.Count() != 2 {
		t.Fatal("distinct receipts share a reclaimable original")
	}
	f.assertRaw(t)
	r, err := f.obj.Get(context.Background(), first.RawObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	closeErr := r.Close()
	if err != nil || closeErr != nil || !bytes.Equal(raw, f.raw) {
		t.Fatal("second acceptance changed first original")
	}
}

func TestR5AcceptBoundaryAllRecipientsResolvedBeforePut(t *testing.T) {
	f := newR5BoundaryFixture(t)
	result, err := f.accept(f.mb.FullAddress, "missing@mail.test")
	if err == nil || result.Queued || result.Delivered != 0 || f.st.job != nil || f.obj.Count() != 0 {
		t.Fatalf("partial resolution persisted/acknowledged: %+v %v %v", result, err, f.calls)
	}
	if !reflect.DeepEqual(f.calls, []string{"resolve:user@mail.test", "resolve:missing@mail.test"}) {
		t.Fatalf("did not validate all targets before storing bytes: %v", f.calls)
	}
}
