package smtp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/testutil"
)

type integrityWireObjects struct {
	*fileobj.FileStore
	closeErr            error
	puts, gets, deletes atomic.Int32
}
type integrityWireReader struct {
	io.ReadCloser
	err error
}

func (r *integrityWireReader) Close() error { return errors.Join(r.ReadCloser.Close(), r.err) }
func (o *integrityWireObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	o.gets.Add(1)
	r, err := o.FileStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &integrityWireReader{r, o.closeErr}, nil
}
func (o *integrityWireObjects) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	o.puts.Add(1)
	return o.FileStore.Put(ctx, key, r, n)
}
func (o *integrityWireObjects) Delete(ctx context.Context, key string) error {
	o.deletes.Add(1)
	return o.FileStore.Delete(ctx, key)
}

func TestContinueRawIntegritySMTP(t *testing.T) {
	for _, name := range []string{"matching", "corrupt-repaired", "uncertain-close"} {
		t.Run(name, func(t *testing.T) {
			st := testutil.NewFakeStore()
			fs, err := fileobj.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			obj := &integrityWireObjects{FileStore: fs}
			tenant, plan, zone := uuid.New(), uuid.New(), uuid.New()
			st.SeedPlan(&models.Plan{ID: plan, MaxMessageBytes: 2048, MaxMessagesPerMailbox: 100, RetentionHours: 24})
			st.SeedTenant(&models.Tenant{ID: tenant, PlanID: plan})
			st.SeedZone(&models.DomainZone{ID: zone, TenantID: tenant, Domain: "mail.test", IsVerified: true, MXVerified: true})
			mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant, ZoneID: zone, FullAddress: "reader@mail.test", LocalPart: "reader", ResolvedDomain: "mail.test", AccessMode: models.AccessPublic}
			st.SeedMailbox(mb)
			body := "Subject: integrity wire\r\n\r\nowned synthetic body\r\n"
			raw := []byte(body)
			stored := append([]byte(nil), raw...)
			key := rawobject.Key(raw)
			wantPuts := int32(0)
			code, wantMessages, wantGets := "250", 1, int32(2)
			if name == "corrupt-repaired" {
				stored[len(stored)-3] ^= 1
				wantPuts = 1
			}
			if name == "uncertain-close" {
				obj.closeErr = errors.New("synthetic close uncertainty")
				code, wantMessages, wantGets = "451", 0, 1
			}
			if err := fs.Put(context.Background(), key, bytes.NewReader(stored), int64(len(stored))); err != nil {
				t.Fatal(err)
			}
			rv := resolver.New(st, policy.NamingFull, true)
			svc := ingest.NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{}, zerolog.Nop())
			s := NewServer(config.SMTP{Domain: "mx.mail.test", Timeout: 5 * time.Second, MaxRecipients: 10, MaxMessageBytes: 2048}, svc, rv, zerolog.Nop())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			s.listen = func(context.Context, string, string) (net.Listener, error) { return ln, nil }
			started := r5Start(s, context.Background())
			r5Await(t, s.ready)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.Shutdown(ctx); err != nil {
					t.Error(err)
				}
				if err := r5Result(t, started); err != nil {
					t.Error(err)
				}
			})
			conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			r := bufio.NewReader(conn)
			r5CloudWire(t, conn, r, "", "220")
			r5CloudWire(t, conn, r, "EHLO fixture.example.test", "250")
			r5CloudWire(t, conn, r, "MAIL FROM:<sender@example.test>", "250")
			r5CloudWire(t, conn, r, "RCPT TO:<reader@mail.test>", "250")
			r5CloudWire(t, conn, r, "DATA", "354")
			r5CloudWire(t, conn, r, body+".", code)
			_, count, e := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
			if e != nil || count != wantMessages || obj.puts.Load() != wantPuts || obj.gets.Load() != wantGets || obj.deletes.Load() != 0 {
				t.Errorf("SMTP effects messages=%d err=%v puts=%d gets=%d deletes=%d", count, e, obj.puts.Load(), obj.gets.Load(), obj.deletes.Load())
			}
			original, e := fs.Get(context.Background(), key)
			if e != nil {
				t.Fatal(e)
			}
			data, e := io.ReadAll(original)
			_ = original.Close()
			want := raw
			if name == "uncertain-close" {
				want = stored
			}
			if e != nil || !bytes.Equal(data, want) {
				t.Errorf("SMTP accepted corrupt original: %q want=%q error=%v", data, want, e)
			}
		})
	}
}
