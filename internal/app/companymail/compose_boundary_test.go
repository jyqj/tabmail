package companymail

import (
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

type composeBoundaryRepository struct {
	*contentRepo
	sourceDenied      error
	destinationDenied error
	afterFinish       func()
}

func (r *composeBoundaryRepository) GetWorkMessage(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*models.Message, error) {
	if r.sourceDenied != nil {
		return nil, r.sourceDenied
	}
	return r.contentRepo.GetWorkMessage(ctx, a, mailbox, id)
}
func (r *composeBoundaryRepository) GetWorkMailbox(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	if r.destinationDenied != nil {
		return nil, r.destinationDenied
	}
	return r.contentRepo.GetWorkMailbox(ctx, a, id)
}
func (r *composeBoundaryRepository) FinishMailAttachment(ctx context.Context, a authz.Actor, id uuid.UUID, hash string) error {
	if err := r.contentRepo.FinishMailAttachment(ctx, a, id, hash); err != nil {
		return err
	}
	if r.afterFinish != nil {
		r.afterFinish()
	}
	return nil
}

type composeBoundaryObjects struct {
	*objectMemory
	onOpen func()
}

func (o *composeBoundaryObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if o.onOpen != nil {
		o.onOpen()
	}
	return o.objectMemory.Get(ctx, key)
}
func composeBoundaryMIME(parts int) []byte {
	raw := "From: sender@client.test\r\nTo: me@company.test\r\nSubject: fixture\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=fixture\r\n\r\n--fixture\r\nContent-Type: text/plain\r\n\r\nprivate-fixture-body\r\n"
	for i := 0; i < parts; i++ {
		raw += "--fixture\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=fixture.txt\r\nContent-Transfer-Encoding: base64\r\n\r\nYWJj\r\n"
	}
	return []byte(raw + "--fixture--\r\n")
}
func composeBoundaryFixture(parts int) (*Service, *composeBoundaryRepository, *composeBoundaryObjects, authz.Actor, uuid.UUID, uuid.UUID, uuid.UUID) {
	_, r, o, a, mailbox, id := contentFixture()
	from := uuid.New()
	r.access.Mailbox.ID = from
	repo := &composeBoundaryRepository{contentRepo: r}
	objects := &composeBoundaryObjects{objectMemory: o}
	objects.raw = composeBoundaryMIME(parts)
	return NewService(repo, objects), repo, objects, a, mailbox, id, from
}
func TestCompanyComposeRechecksDestinationAfterParsing(t *testing.T) {
	for _, mode := range []string{"reply", "reply_all", "forward"} {
		for _, change := range []string{"revoked", "database", "address-changed"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				s, r, o, a, mb, id, from := composeBoundaryFixture(2)
				o.onOpen = func() {
					switch change {
					case "revoked":
						r.destinationDenied = app.Forbidden("send_as revoked")
					case "database":
						r.destinationDenied = app.Internal(errors.New("fixture database error"))
					case "address-changed":
						r.access.Mailbox.FullAddress = "replacement@company.test"
					}
				}
				value, err := s.Compose(context.Background(), a, mb, id, from, mode)
				if err == nil || value != nil {
					t.Fatal("stale destination produced a composed payload")
				}
				if len(*r.events) != 0 {
					t.Fatal("copy started after destination changed during parse")
				}
				if !o.closed {
					t.Fatal("parse reader leaked")
				}
			})
		}
	}
}
func TestCompanyForwardRechecksSourceBetweenCopies(t *testing.T) {
	for _, parts := range []int{1, 2} {
		for _, change := range []string{"revoked", "source-changed", "destination-revoked", "cancelled"} {
			label := change + "/one"
			if parts == 2 {
				label = change + "/two"
			}
			t.Run(label, func(t *testing.T) {
				s, r, _, a, mb, id, from := composeBoundaryFixture(parts)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				r.afterFinish = func() {
					switch change {
					case "revoked":
						r.sourceDenied = app.Forbidden("read revoked")
					case "source-changed":
						r.message.RawObjectKey = "replacement"
					case "destination-revoked":
						r.destinationDenied = app.Forbidden("send revoked")
					case "cancelled":
						cancel()
					}
				}
				value, err := s.Compose(ctx, a, mb, id, from, "forward")
				if err == nil || value != nil {
					t.Error("authority changed during copying but payload escaped")
				}
				if strings.Join(*r.events, ",") != "reserve,put,finish" {
					t.Error("forward copied another attachment after changed authority")
				}
				// The accepted first reservation is retained: no invented rollback or
				// object deletion after a possibly committed Finish operation.
				if r.attachment == nil {
					t.Error("completed reservation was discarded")
				}
			})
		}
	}
}
func TestCompanyComposeBoundaryPreservesForward(t *testing.T) {
	s, r, o, a, mb, id, from := composeBoundaryFixture(2)
	value, err := s.Compose(context.Background(), a, mb, id, from, "forward")
	if err != nil {
		t.Fatal(err)
	}
	if value.Subject != "Fwd: fixture" || !strings.Contains(value.TextBody, "private-fixture-body") || len(value.AttachmentIDs) != 2 || len(value.BCC) != 0 {
		t.Error("authorized forward changed")
	}
	if strings.Join(*r.events, ",") != "reserve,put,finish,reserve,put,finish" {
		t.Error("valid forward did not preserve upload protocol")
	}
	if o.gets != 1 {
		t.Error("source was re-parsed for each copied attachment")
	}
}
