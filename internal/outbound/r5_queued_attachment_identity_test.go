package outbound

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type queuedAttachmentRepository struct {
	Repository
	rows  []company.Attachment
	calls int
}

func (r *queuedAttachmentRepository) OutboundAttachments(context.Context, uuid.UUID) ([]company.Attachment, error) {
	r.calls++
	return r.rows, nil
}

type queuedAttachmentObjects struct {
	store.ObjectStore
	data  map[string]string
	reads []string
}

func (s *queuedAttachmentObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.reads = append(s.reads, key)
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("unknown test object")
	}
	return io.NopCloser(strings.NewReader(v)), nil
}

func queuedAttachmentFixture() (company.Attachment, company.Attachment, *queuedAttachmentObjects) {
	a := company.Attachment{ID: uuid.New(), ObjectKey: "first", Filename: "first.txt", State: "ready", Size: 5, SHA256: company.Hash("first")}
	b := company.Attachment{ID: uuid.New(), ObjectKey: "second", Filename: "second.txt", State: "ready", Size: 6, SHA256: company.Hash("second")}
	return a, b, &queuedAttachmentObjects{data: map[string]string{"first": "first", "second": "second"}}
}

func TestR5QueuedAttachmentsRequireExactPinnedIdentities(t *testing.T) {
	a, b, _ := queuedAttachmentFixture()
	c := a
	c.ID = uuid.New()
	zero := a
	zero.ID = uuid.Nil
	tests := []struct {
		name string
		pins []uuid.UUID
		rows []company.Attachment
	}{
		{"same_count_replaced_identity", []uuid.UUID{a.ID, b.ID}, []company.Attachment{a, c}},
		{"same_count_duplicate_row", []uuid.UUID{a.ID, b.ID}, []company.Attachment{a, a}},
		{"missing_row", []uuid.UUID{a.ID, b.ID}, []company.Attachment{a}},
		{"extra_row", []uuid.UUID{a.ID}, []company.Attachment{a, b}},
		{"duplicate_pin", []uuid.UUID{a.ID, a.ID}, []company.Attachment{a, a}},
		{"nil_pin", []uuid.UUID{uuid.Nil}, []company.Attachment{zero}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objects := &queuedAttachmentObjects{data: map[string]string{"first": "first", "second": "second"}}
			repo := &queuedAttachmentRepository{rows: tc.rows}
			svc := &Service{store: repo, objects: objects}
			job := &models.OutboundJob{ID: uuid.New(), AttachmentIDs: tc.pins, MailFrom: "sender@example.test", To: []string{"recipient@example.test"}, TextBody: "message"}
			wire, err := svc.buildQueuedMIME(context.Background(), job)
			if err == nil || wire != nil {
				t.Errorf("invalid pinned attachment relation rendered MIME: err=%v bytes=%d", err, len(wire))
			}
			if len(objects.reads) != 0 {
				t.Errorf("invalid relation must fail before any object read: %v", objects.reads)
			}
		})
	}
}

func TestR5QueuedAttachmentsAcceptUnorderedExactSet(t *testing.T) {
	a, b, objects := queuedAttachmentFixture()
	repo := &queuedAttachmentRepository{rows: []company.Attachment{b, a}}
	svc := &Service{store: repo, objects: objects}
	job := &models.OutboundJob{ID: uuid.New(), AttachmentIDs: []uuid.UUID{a.ID, b.ID}, MailFrom: "sender@example.test", To: []string{"recipient@example.test"}, TextBody: "message"}
	wire, err := svc.buildQueuedMIME(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(strings.NewReader(string(wire)))
	if err != nil {
		t.Fatal(err)
	}
	media, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || media != "multipart/mixed" {
		t.Fatalf("expected attachment MIME, got %s %v", media, err)
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	attachments := map[string]string{}
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if part.FileName() == "" {
			continue
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatal(err)
		}
		attachments[part.FileName()] = string(decoded)
	}
	if !reflect.DeepEqual(attachments, map[string]string{"first.txt": "first", "second.txt": "second"}) {
		t.Fatalf("wrong MIME attachments: %v", attachments)
	}
	if repo.calls != 1 || len(objects.reads) != 2 {
		t.Fatalf("expected one relation lookup and two objects, got %d %v", repo.calls, objects.reads)
	}
}
