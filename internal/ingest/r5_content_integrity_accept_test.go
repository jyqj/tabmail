package ingest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/testutil"
)

type integrityAcceptObjects struct {
	*fileobj.FileStore
	getErr, closeErr, putErr error
	gets, puts, deletes      int
	putKeys                  []string
}
type integrityAcceptReader struct {
	io.ReadCloser
	err error
}

func (r *integrityAcceptReader) Close() error { return errors.Join(r.ReadCloser.Close(), r.err) }
func (o *integrityAcceptObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	o.gets++
	if o.getErr != nil {
		return nil, o.getErr
	}
	r, err := o.FileStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &integrityAcceptReader{r, o.closeErr}, nil
}
func (o *integrityAcceptObjects) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	o.puts++
	o.putKeys = append(o.putKeys, key)
	if o.putErr != nil {
		return o.putErr
	}
	return o.FileStore.Put(ctx, key, r, n)
}
func (o *integrityAcceptObjects) Delete(ctx context.Context, key string) error {
	o.deletes++
	return o.FileStore.Delete(ctx, key)
}

type integrityAcceptStore struct {
	*testutil.FakeStore
	beforeEnsure func()
	creates      int
}

func (s *integrityAcceptStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, n int, ensure func(context.Context) error) (bool, error) {
	s.creates++
	if s.beforeEnsure != nil {
		s.beforeEnsure()
	}
	return s.FakeStore.CreateMessageWithQuota(ctx, m, n, ensure)
}

// Actual resolver -> Accept -> raw lifecycle -> real FileStore publication,
// with metadata ports synthetic. Durable receipt UUID ownership is a control.
func TestContinueRawIntegrityAccept(t *testing.T) {
	for _, name := range []string{"matching", "corrupt-existing-reference", "corrupted-before-ensure", "get-error", "close-error", "repair-error", "ensure-close-error", "durable-UUID-unchanged"} {
		t.Run(name, func(t *testing.T) {
			fs, err := fileobj.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			obj := &integrityAcceptObjects{FileStore: fs}
			st := &integrityAcceptStore{FakeStore: testutil.NewFakeStore()}
			tenant, plan, zone := uuid.New(), uuid.New(), uuid.New()
			st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: 2048, MaxMessagesPerMailbox: 100, RetentionHours: 24})
			st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
			st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
			st.SeedMailbox(mb)
			raw := []byte("Subject: accept integrity\r\n\r\nowned synthetic original\r\n")
			key := rawobject.Key(raw)
			damaged := append([]byte(nil), raw...)
			damaged[len(damaged)-3] ^= 1
			stored := raw
			wantMessages, wantPuts, wantGets := 1, 0, 2
			wantError := false
			fault := errors.New("synthetic object unavailable")
			if name == "corrupt-existing-reference" || name == "repair-error" || name == "durable-UUID-unchanged" {
				stored = damaged
			}
			if err := fs.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
				t.Fatal(err)
			}
			if name == "corrupt-existing-reference" {
				st.SeedMessage(&models.Message{ID: uuid.New(), MailboxID: mb.ID, TenantID: tenant, RawObjectKey: key})
				wantMessages, wantPuts = 2, 1
			}
			if name == "corrupted-before-ensure" {
				wantPuts = 1
				st.beforeEnsure = func() {
					if err := fs.Put(context.Background(), key, bytes.NewReader(damaged), int64(len(damaged))); err != nil {
						t.Error(err)
					}
				}
			}
			if name == "get-error" {
				obj.getErr = fault
				wantError = true
				wantMessages, wantGets = 0, 1
			}
			if name == "close-error" {
				obj.closeErr = fault
				wantError = true
				wantMessages, wantGets = 0, 1
			}
			if name == "repair-error" {
				obj.putErr = fault
				wantError = true
				wantMessages, wantPuts, wantGets = 0, 1, 1
			}
			if name == "ensure-close-error" {
				wantError = true
				wantMessages = 1
				st.SeedMessage(&models.Message{ID: uuid.New(), MailboxID: mb.ID, TenantID: tenant, RawObjectKey: key})
				st.beforeEnsure = func() { obj.closeErr = fault }
			}
			durable := name == "durable-UUID-unchanged"
			if durable {
				wantMessages, wantPuts, wantGets = 0, 1, 0
			}
			rv := resolver.New(st, policy.NamingFull, true)
			svc := NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: durable}, zerolog.Nop())
			got, err := svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: []string{mb.FullAddress}}, raw)
			if wantError {
				if err == nil || got.Delivered != 0 || got.Queued {
					t.Errorf("uncertain original acknowledged: %+v %v", got, err)
				}
			} else if err != nil || got.Queued != durable || (!durable && got.Delivered != 1) {
				t.Errorf("valid delivery failed: %+v %v", got, err)
			}
			_, count, e := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
			if e != nil || count != wantMessages {
				t.Errorf("messages=%d want=%d err=%v", count, wantMessages, e)
			}
			if obj.puts != wantPuts || obj.gets != wantGets || obj.deletes != 0 {
				t.Errorf("wrong object effects: put=%d get=%d delete=%d, want %d/%d/0", obj.puts, obj.gets, obj.deletes, wantPuts, wantGets)
			}
			r, e := fs.Get(context.Background(), key)
			if e != nil {
				t.Fatal(e)
			}
			data, e := io.ReadAll(r)
			_ = r.Close()
			want := raw
			if wantError || durable {
				want = stored
			}
			if e != nil || !bytes.Equal(data, want) {
				t.Errorf("wrong original content: %q want=%q error=%v", data, want, e)
			}
			if durable {
				if len(obj.putKeys) != 1 || !strings.HasPrefix(obj.putKeys[0], "ingress-") {
					t.Errorf("durable content-key route changed: %v", obj.putKeys)
				}
				jobs, total, e := st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 10}, "", "", "")
				if e != nil || total != 1 || len(jobs) != 1 || jobs[0].RawObjectKey != obj.putKeys[0] {
					t.Errorf("durable receipt missing: %v %d %v", jobs, total, e)
				}
			}
		})
	}
}
