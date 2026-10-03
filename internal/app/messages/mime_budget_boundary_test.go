package messageapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

type mimeBoundaryObject struct {
	*testutil.MemoryObjectStore
	raw     []byte
	events  *[]string
	getErr  error
	readErr error
	opens   int
	last    *mimeBoundaryReader
}

func (o *mimeBoundaryObject) Get(context.Context, string) (io.ReadCloser, error) {
	o.opens++
	*o.events = append(*o.events, "object.get")
	o.last = &mimeBoundaryReader{Reader: bytes.NewReader(o.raw), failure: o.readErr, events: o.events}
	return o.last, o.getErr
}

type mimeBoundaryReader struct {
	*bytes.Reader
	failure error
	events  *[]string
	closes  atomic.Int32
}

func (r *mimeBoundaryReader) Read(p []byte) (int, error) {
	*r.events = append(*r.events, "object.read")
	if r.failure != nil {
		return 0, r.failure
	}
	if len(p) > 7 {
		p = p[:7]
	}
	return r.Reader.Read(p)
}
func (r *mimeBoundaryReader) Close() error { r.closes.Add(1); return nil }

type mimeBoundaryAuditStore struct {
	*testutil.FakeStore
	events   *[]string
	auditErr error
}

func (s *mimeBoundaryAuditStore) InsertAudit(ctx context.Context, entry *models.AuditEntry) error {
	*s.events = append(*s.events, "audit.required")
	if s.auditErr != nil {
		return s.auditErr
	}
	return s.FakeStore.InsertAudit(ctx, entry)
}
func mimeBoundaryFixture(t *testing.T, raw []byte) (*Service, *mimeBoundaryObject, *mimeBoundaryAuditStore, *models.Message, *models.Mailbox, Viewer) {
	t.Helper()
	st, svc, tenant, mb := seededMessageService(t, models.AccessPublic, nil)
	msg := &models.Message{ID: uuid.New(), TenantID: tenant.ID, MailboxID: mb.ID, RawObjectKey: "mime-boundary", OTPCode: "123456"}
	st.SeedMessage(msg)
	events := []string{}
	obj := &mimeBoundaryObject{MemoryObjectStore: testutil.NewMemoryObjectStore(), raw: raw, events: &events}
	audit := &mimeBoundaryAuditStore{FakeStore: st, events: &events}
	svc.obj = obj
	svc.store = audit
	return svc, obj, audit, msg, mb, Viewer{Tenant: tenant, AuthMode: AuthModePublic}
}
func mimeBoundaryDeep() []byte {
	raw := "Content-Type: text/plain\r\n\r\nbody"
	for level := mailcontent.MaxMIMEDepth; level > 0; level-- {
		b := fmt.Sprintf("depth%d", level)
		raw = fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", b, b, raw, b)
	}
	return []byte(raw)
}
func mimeBoundaryWide() []byte {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=W\r\n\r\n")
	for i := 0; i < mailcontent.MaxMIMENodes; i++ {
		raw.WriteString("--W\r\nContent-Type: text/plain\r\n\r\nbody\r\n")
	}
	raw.WriteString("--W--\r\n")
	return []byte(raw.String())
}
func TestMessagesMIMEPreparseBudgetRealBodyPaths(t *testing.T) {
	fault := errors.New("object read failed")
	for _, breakGlass := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			raw     []byte
			readErr error
			want    error
		}{
			{"legal", []byte("Content-Type: text/plain\r\n\r\nbody"), nil, nil},
			{"deep", mimeBoundaryDeep(), nil, mailcontent.ErrMIMEDepth},
			{"many_body_nodes", mimeBoundaryWide(), nil, mailcontent.ErrMIMENodes},
			{"malformed", []byte("Subject broken no colon\r\n\r\nbody"), nil, mailcontent.ErrMIMEParse},
			{"read_failure", []byte("Subject: valid\r\n\r\nbody"), fault, fault},
		} {
			t.Run(fmt.Sprintf("breakglass_%v/%s", breakGlass, tc.name), func(t *testing.T) {
				svc, obj, _, msg, mb, viewer := mimeBoundaryFixture(t, tc.raw)
				obj.readErr = tc.readErr
				var detail *models.MessageDetail
				var err error
				if breakGlass {
					viewer.AuthMode = AuthModeUser
					viewer.IsAdmin = true
					detail, err = svc.BreakGlassRead(context.Background(), mb.FullAddress, msg.ID, viewer, "admin", "investigate delivery issue")
				} else {
					detail, err = svc.GetMessageDetail(context.Background(), mb.FullAddress, msg.ID, viewer)
				}
				if tc.want == nil {
					if err != nil || detail.TextBody != "body" {
						t.Fatalf("detail %+v, error %v", detail, err)
					}
				} else {
					appErr, ok := app.As(err)
					if !ok || appErr.Kind != app.KindInternal || !errors.Is(err, tc.want) {
						t.Fatalf("error %v want internal/%v", err, tc.want)
					}
					if detail != nil {
						t.Fatal("rejected content returned")
					}
				}
				if obj.opens != 1 || obj.last.closes.Load() != 1 {
					t.Fatal("object reopen or leaked close")
				}
				if breakGlass && (*obj.events)[0] != "audit.required" {
					t.Fatalf("order %v", *obj.events)
				}
			})
		}
	}
}
func TestMessagesMIMEPreparseBudgetAuthorityAndAuditBeforeRead(t *testing.T) {
	svc, obj, audit, msg, mb, viewer := mimeBoundaryFixture(t, mimeBoundaryDeep())
	viewer.AuthMode = AuthModeUser
	viewer.IsAdmin = true
	detail, err := svc.GetMessageDetail(context.Background(), mb.FullAddress, msg.ID, viewer)
	if err != nil || !detail.BodyRedacted || detail.OTPCode != "" || obj.opens != 0 {
		t.Fatalf("redacted detail %+v, error %v, opens %d", detail, err, obj.opens)
	}
	audit.auditErr = errors.New("required audit failed")
	if _, err := svc.BreakGlassRead(context.Background(), mb.FullAddress, msg.ID, viewer, "admin", "investigate delivery issue"); !errors.Is(err, audit.auditErr) {
		t.Fatal(err)
	}
	if obj.opens != 0 || !reflect.DeepEqual(*obj.events, []string{"audit.required"}) {
		t.Fatalf("object touched before audit: %v", *obj.events)
	}
	viewer.IsAdmin = false
	if _, err := svc.BreakGlassRead(context.Background(), mb.FullAddress, msg.ID, viewer, "user", "investigate delivery issue"); appErrKind(err) != app.KindForbidden {
		t.Fatal(err)
	}
	if obj.opens != 0 {
		t.Fatal("nonadmin opened object")
	}
}
func appErrKind(err error) app.ErrorKind {
	if e, ok := app.As(err); ok {
		return e.Kind
	}
	return ""
}
func TestMessagesMIMEPreparseBudgetAcquisitionErrorClosesReader(t *testing.T) {
	for _, breakGlass := range []bool{false, true} {
		svc, obj, _, msg, mb, viewer := mimeBoundaryFixture(t, []byte("Subject: valid\r\n\r\nbody"))
		obj.getErr = errors.New("get failed with reader")
		var err error
		if breakGlass {
			viewer.AuthMode = AuthModeUser
			viewer.IsAdmin = true
			_, err = svc.BreakGlassRead(context.Background(), mb.FullAddress, msg.ID, viewer, "admin", "investigate delivery issue")
		} else {
			_, err = svc.GetMessageDetail(context.Background(), mb.FullAddress, msg.ID, viewer)
		}
		if !errors.Is(err, obj.getErr) || obj.last.closes.Load() != 1 {
			t.Fatalf("error %v, closes %d", err, obj.last.closes.Load())
		}
		if !reflect.DeepEqual(*obj.events, func() []string {
			if breakGlass {
				return []string{"audit.required", "object.get"}
			}
			return []string{"object.get"}
		}()) {
			t.Fatalf("read after failed get: %v", *obj.events)
		}
	}
}
