package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type contentRepo struct {
	Repository // Unimplemented calls panic: tests must use only declared ports.
	message    *models.Message
	access     *company.MailboxAccess
	attachment *company.Attachment
	denied     error
	finishErr  error
	events     *[]string
}

func (r *contentRepo) GetWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*models.Message, error) {
	return r.message, r.denied
}
func (r *contentRepo) GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*company.MailboxAccess, error) {
	return r.access, r.denied
}
func (r *contentRepo) GetWorkAttachment(context.Context, authz.Actor, uuid.UUID) (*company.Attachment, error) {
	return r.attachment, r.denied
}
func (r *contentRepo) GetSubmissionAttachment(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*company.SubmissionAttachment, error) {
	if r.denied != nil {
		return nil, r.denied
	}
	a := r.attachment
	return &company.SubmissionAttachment{ID: a.ID, ObjectKey: a.ObjectKey, Filename: a.Filename, State: a.State, Size: a.Size, SHA256: a.SHA256}, nil
}
func (r *contentRepo) ReserveMailAttachment(_ context.Context, _ authz.Actor, a company.Attachment) (*company.Attachment, error) {
	*r.events = append(*r.events, "reserve")
	a.ID = uuid.New()
	a.ObjectKey = "reserved"
	a.State = "uploading"
	r.attachment = &a
	return &a, nil
}
func (r *contentRepo) FinishMailAttachment(_ context.Context, _ authz.Actor, _ uuid.UUID, hash string) error {
	*r.events = append(*r.events, "finish")
	if r.finishErr != nil {
		return r.finishErr
	}
	r.attachment.SHA256 = hash
	return nil
}

type trackedReader struct {
	io.Reader
	closed *bool
}

func (r trackedReader) Close() error { *r.closed = true; return nil }

type objectMemory struct {
	raw    []byte
	gets   int
	closed bool
	putErr error
	events *[]string
}

func (o *objectMemory) Get(context.Context, string) (io.ReadCloser, error) {
	o.gets++
	return trackedReader{bytes.NewReader(o.raw), &o.closed}, nil
}
func (o *objectMemory) Put(_ context.Context, _ string, r io.Reader, n int64) error {
	*o.events = append(*o.events, "put")
	if o.putErr != nil {
		return o.putErr
	}
	raw, e := io.ReadAll(r)
	o.raw = raw
	if int64(len(raw)) != n {
		return errors.New("size mismatch")
	}
	return e
}
func contentFixture() (*Service, *contentRepo, *objectMemory, authz.Actor, uuid.UUID, uuid.UUID) {
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser}
	mb, id := uuid.New(), uuid.New()
	events := []string{}
	repo := &contentRepo{message: &models.Message{ID: id, TenantID: a.TenantID, MailboxID: mb, RawObjectKey: "private/key", Seen: true, Starred: true},
		access: &company.MailboxAccess{Mailbox: models.Mailbox{ID: mb, TenantID: a.TenantID, FullAddress: "me@company.test"}, CanRead: true, CanSend: true}, events: &events}
	obj := &objectMemory{raw: []byte("From: sender@client.test\r\nSubject: hello\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nprivate body"), events: &events}
	return NewService(repo, obj), repo, obj, a, mb, id
}
func expectKind(t *testing.T, e error, kind app.ErrorKind) {
	t.Helper()
	v, ok := app.As(e)
	if !ok || v.Kind != kind {
		t.Fatalf("want %s, got %v", kind, e)
	}
}
func TestCompanyContentAuthorizesBeforeOpeningObjects(t *testing.T) {
	for _, mode := range []string{"revoked", "tenant", "mailbox", "message", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, r, o, a, mb, id := contentFixture()
			switch mode {
			case "revoked":
				r.denied = app.NotFound("not found")
			case "tenant":
				r.message.TenantID = uuid.New()
			case "mailbox":
				r.message.MailboxID = uuid.New()
			case "message":
				r.message.ID = uuid.New()
			case "missing":
				r.message = nil
			}
			_, e := s.Message(context.Background(), a, mb, id)
			expectKind(t, e, app.KindNotFound)
			if _, e = s.Source(context.Background(), a, mb, id); e == nil {
				t.Fatal("source bypassed authorization")
			}
			if _, e = s.InboundAttachments(context.Background(), a, mb, id); e == nil {
				t.Fatal("attachments bypassed authorization")
			}
			if o.gets != 0 {
				t.Fatal("opened private bytes before authorization")
			}
		})
	}
}
func TestCompanyMessageKeepsPersonalStateAndRemovesObjectKey(t *testing.T) {
	s, r, o, a, mb, id := contentFixture()
	v, e := s.Message(context.Background(), a, mb, id)
	if e != nil {
		t.Fatal(e)
	}
	if v.TextBody != "private body" || v.RawObjectKey != "" || !v.Seen || !v.Starred {
		t.Fatalf("invalid projection: %+v", v)
	}
	if r.message.RawObjectKey == "" {
		t.Fatal("mutated adapter metadata")
	}
	if !o.closed {
		t.Fatal("MIME reader leaked")
	}
}
func TestCompanyAttachmentIntegrityAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size        int64
		hash, state string
		raw         []byte
		wantOpen    bool
		success     bool
	}{
		{"valid", 3, digest([]byte("abc")), "ready", []byte("abc"), true, true},
		{"truncated", 3, digest([]byte("abc")), "ready", []byte("ab"), true, false},
		{"extra", 3, digest([]byte("abc")), "ready", []byte("abcd"), true, false},
		{"corrupt", 3, digest([]byte("abc")), "ready", []byte("xyz"), true, false},
		{"unfinished", 3, digest([]byte("abc")), "uploading", nil, false, false},
		{"negative", -1, digest(nil), "ready", nil, false, false},
		{"oversized", MaxAttachmentBytes + 1, digest(nil), "ready", nil, false, false},
		{"no-hash", 3, "", "ready", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, o, a, _, _ := contentFixture()
			o.raw = tc.raw
			r.attachment = &company.Attachment{ID: uuid.New(), ObjectKey: "key", Filename: "../../report.txt", State: tc.state, Size: tc.size, SHA256: tc.hash}
			for _, sent := range []bool{false, true} {
				o.gets = 0
				o.closed = false
				var v *File
				var e error
				if sent {
					v, e = s.SubmissionAttachment(context.Background(), a, uuid.New(), r.attachment.ID)
				} else {
					v, e = s.Attachment(context.Background(), a, r.attachment.ID)
				}
				if tc.success {
					if e != nil || string(v.Content) != "abc" || v.Filename != "report.txt" {
						t.Fatalf("invalid verified download: %v %v", v, e)
					}
				} else if e == nil || v != nil {
					t.Fatal("invalid bytes returned as successful download")
				}
				if (o.gets > 0) != tc.wantOpen || (tc.wantOpen && !o.closed) {
					t.Fatal("object open/close boundary violated")
				}
			}
		})
	}
}
func TestCompanyUploadReservePutFinishAndFailures(t *testing.T) {
	for _, mode := range []string{"ok", "no-send", "revoked", "put-failure", "finish-failure", "too-large"} {
		t.Run(mode, func(t *testing.T) {
			s, r, o, a, mb, _ := contentFixture()
			raw := []byte{0, 255, 128, 1}
			switch mode {
			case "no-send":
				r.access.CanSend = false
			case "revoked":
				r.denied = app.Forbidden("revoked")
			case "put-failure":
				o.putErr = errors.New("disk error")
			case "finish-failure":
				r.finishErr = errors.New("commit unknown")
			case "too-large":
				raw = make([]byte, MaxAttachmentBytes+1)
			}
			v, e := s.UploadAttachment(context.Background(), a, mb, "../binary.dat", bytes.NewReader(raw))
			if mode == "ok" {
				if e != nil || v.State != "ready" || v.SHA256 != digest(raw) || v.Filename != "binary.dat" {
					t.Fatalf("upload failed: %+v %v", v, e)
				}
			} else if e == nil || v != nil {
				t.Fatal("failed upload reported ready")
			}
			want := "reserve,put,finish"
			switch mode {
			case "no-send", "revoked", "too-large":
				want = ""
			case "put-failure":
				want = "reserve,put"
			}
			if strings.Join(*r.events, ",") != want {
				t.Fatalf("wrong side effects: %v", *r.events)
			}
		})
	}
}
func TestCompanyComposeRecipientRules(t *testing.T) {
	s, _, o, a, mb, id := contentFixture()
	o.raw = []byte("From: sender@client.test\r\nReply-To: reply@client.test\r\nTo: me@company.test, reply@client.test, other@client.test\r\nCc: other@client.test, cc@client.test\r\nBcc: private@client.test\r\nSubject: hello\r\nMessage-ID: <original@client.test>\r\n\r\nbody")
	p, e := s.Compose(context.Background(), a, mb, id, mb, "reply_all")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(p.To, ",") != "reply@client.test,other@client.test" || strings.Join(p.CC, ",") != "cc@client.test" || len(p.BCC) != 0 || p.Subject != "Re: hello" || p.Headers["In-Reply-To"] != "<original@client.test>" {
		t.Fatalf("unsafe reply: %+v", p)
	}
	o.raw = []byte("From: sender@client.test\r\nReply-To: invalid <\r\nSubject: bad\r\n\r\nbody")
	_, e = s.Compose(context.Background(), a, mb, id, mb, "reply")
	expectKind(t, e, app.KindBadRequest)
}
func TestCompanyComposeNeedsDestinationSendAndSourceRead(t *testing.T) {
	s, r, o, a, mb, id := contentFixture()
	r.access.CanSend = false
	if _, e := s.Compose(context.Background(), a, mb, id, mb, "forward"); e == nil {
		t.Fatal("forward without send allowed")
	}
	if o.gets != 0 {
		t.Fatal("source read despite destination denial")
	}
	r.access.CanSend = true
	r.message.MailboxID = uuid.New()
	if _, e := s.Compose(context.Background(), a, mb, id, mb, "forward"); e == nil {
		t.Fatal("forward read a different mailbox")
	}
}
func TestCompanySafeFilename(t *testing.T) {
	for name, want := range map[string]string{"../../report.pdf": "report.pdf", `C:\private\report.txt`: "report.txt", "\r\n": "attachment", "..": "attachment", "": "attachment"} {
		if got := SafeFilename(name); got != want {
			t.Fatalf("%q: got %q want %q", name, got, want)
		}
	}
}
