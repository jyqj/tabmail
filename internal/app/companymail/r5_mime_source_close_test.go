package companymail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

type r5MIMECloseCacheRepo struct {
	*contentRepo
	saved *company.ParsedMessage
	saves int
}

func (r *r5MIMECloseCacheRepo) GetParsedMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*company.ParsedMessage, error) {
	return r.saved, nil
}
func (r *r5MIMECloseCacheRepo) SaveParsedMessage(_ context.Context, _ authz.Actor, _ uuid.UUID, doc company.ParsedMessage) error {
	r.saves++
	r.saved = &doc
	return nil
}

type r5MIMECloseObjects struct {
	ObjectStore
	raw              []byte
	failure          error
	opens, completed atomic.Int32
}

type r5MIMECloseObjectReader struct {
	io.Reader
	close func() error
}

func (r *r5MIMECloseObjectReader) Close() error { return r.close() }
func (o *r5MIMECloseObjects) Get(context.Context, string) (io.ReadCloser, error) {
	attempt := o.opens.Add(1)
	return &r5MIMECloseObjectReader{Reader: bytes.NewReader(o.raw), close: func() error {
		o.completed.Add(1)
		if attempt == 1 {
			return o.failure
		}
		return nil
	}}, nil
}

func TestR5MIMESourceCloseDoesNotPublishCompanyContent(t *testing.T) {
	for _, entry := range []string{"message", "inbound-attachments"} {
		t.Run(entry, func(t *testing.T) {
			_, base, original, actor, mailbox, message := contentFixture()
			repo := &r5MIMECloseCacheRepo{contentRepo: base}
			closeErr := errors.New("owned MIME source close failure")
			objects := &r5MIMECloseObjects{raw: original.raw, failure: closeErr}
			service := NewService(repo, objects)
			call := func() (bool, error) {
				if entry == "message" {
					value, err := service.Message(context.Background(), actor, mailbox, message)
					return value != nil, err
				}
				value, err := service.InboundAttachments(context.Background(), actor, mailbox, message)
				return value != nil, err
			}
			if value, err := call(); value || !errors.Is(err, closeErr) {
				t.Errorf("company content accepted failed close: value=%v err=%v", value, err)
			}
			if repo.saves != 0 || repo.saved != nil {
				t.Error("failed source reached the persistent parsed-content write port")
			}
			for i := 0; i < 2; i++ {
				if value, err := call(); !value || err != nil {
					t.Errorf("healthy retry/cache read failed: value=%v err=%v", value, err)
				}
			}
			if repo.saves != 1 || repo.saved == nil || repo.saved.TextBody != "private body" || objects.opens.Load() != 2 || objects.completed.Load() != 2 {
				t.Errorf("retry/cache ownership changed: saves=%d opens=%d closes=%d", repo.saves, objects.opens.Load(), objects.completed.Load())
			}
		})
	}
}
