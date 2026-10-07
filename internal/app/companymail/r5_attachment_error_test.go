package companymail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"tabmail/internal/app"
	"tabmail/internal/mailcontent"
)

type r5AttachmentErrorObjects struct {
	ObjectStore
	err    error
	onOpen func()
}

func (o *r5AttachmentErrorObjects) Get(context.Context, string) (io.ReadCloser, error) {
	if o.onOpen != nil {
		o.onOpen()
	}
	return nil, o.err
}

func TestR5InboundAttachmentPreservesSourceFailure(t *testing.T) {
	for _, cause := range []error{errors.New("synthetic storage outage"), context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			_, repo, _, actor, mailbox, message := contentFixture()
			svc := NewService(repo, &r5AttachmentErrorObjects{err: fmt.Errorf("source backend: %w", cause)})
			file, err := svc.InboundAttachmentByID(context.Background(), actor, mailbox, message, strings.Repeat("0", 64))
			expectKind(t, err, app.KindInternal)
			if file != nil || !errors.Is(err, cause) {
				t.Fatalf("source cause or failed-result boundary lost: file=%v error=%v", file, err)
			}
		})
	}
}

func TestR5InboundAttachmentPreservesActiveCancellation(t *testing.T) {
	_, repo, _, actor, mailbox, message := contentFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(repo, &r5AttachmentErrorObjects{err: context.Canceled, onOpen: cancel})
	file, err := svc.InboundAttachmentByID(ctx, actor, mailbox, message, strings.Repeat("0", 64))
	if file != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("active caller cancellation was turned into a missing attachment: %v", err)
	}
}

func TestR5InboundAttachmentDistinguishesMalformedSourceAndMissingPart(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		kind app.ErrorKind
		want error
	}{
		{"malformed", "Subject broken no colon\r\n\r\nbody", app.KindInternal, mailcontent.ErrMIMEParse},
		{"missing_part", "Subject: valid\r\n\r\nbody", app.KindNotFound, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, objects, actor, mailbox, message := contentFixture()
			objects.raw = []byte(tc.raw)
			file, err := svc.InboundAttachmentByID(context.Background(), actor, mailbox, message, strings.Repeat("0", 64))
			expectKind(t, err, tc.kind)
			if file != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("wrong source/part classification: file=%v error=%v", file, err)
			}
		})
	}
}
