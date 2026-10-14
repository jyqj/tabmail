package companymail

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestR5InboundAttachmentBytesBelongToCaller(t *testing.T) {
	for _, source := range []string{"ordinal", "stable-id"} {
		for _, target := range []string{"ordinal", "stable-id", "metadata", "another-message-same-source"} {
			t.Run(source+"/"+target, func(t *testing.T) {
				svc, repo, objects, actor, mailbox, message := boundaryFixture()
				ctx := context.Background()
				parts, err := svc.InboundAttachments(ctx, actor, mailbox, message)
				if err != nil || len(parts) != 1 {
					t.Fatalf("fixture parts: %+v %v", parts, err)
				}
				original, err := svc.InboundAttachmentByID(ctx, actor, mailbox, message, parts[0].ID)
				if err != nil || len(original.Content) == 0 {
					t.Fatalf("fixture attachment: %+v %v", original, err)
				}
				// Freeze the expectation independently of every returned File,
				// including the stable-ID consumer used to read the fixture.
				wantContent := bytes.Clone(original.Content)
				wantFilename := original.Filename
				var first *File
				if source == "ordinal" {
					first, err = svc.InboundAttachment(ctx, actor, mailbox, message, 0)
				} else {
					first, err = svc.InboundAttachmentByID(ctx, actor, mailbox, message, parts[0].ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				for i := range first.Content {
					first.Content[i] = 'X'
				}
				first.Filename = "caller-edited.txt"
				var next *File
				switch target {
				case "ordinal":
					next, err = svc.InboundAttachment(ctx, actor, mailbox, message, 0)
				case "stable-id":
					next, err = svc.InboundAttachmentByID(ctx, actor, mailbox, message, parts[0].ID)
				case "metadata":
					current, readErr := svc.InboundAttachments(ctx, actor, mailbox, message)
					if readErr != nil || !reflect.DeepEqual(current, parts) {
						t.Fatalf("caller mutation changed source-derived identity/hash: before=%+v after=%+v err=%v", parts, current, readErr)
					}
				case "another-message-same-source":
					other := uuid.New()
					repo.mu.Lock()
					repo.message.ID = other
					repo.mu.Unlock()
					next, err = svc.InboundAttachment(ctx, actor, mailbox, other, 0)
				}
				if target != "metadata" && (err != nil || next == nil || next.Filename != wantFilename || !bytes.Equal(next.Content, wantContent)) {
					t.Fatalf("caller mutation escaped its returned File: got=%+v err=%v", next, err)
				}
				if objects.opens.Load() != 1 || objects.closes.Load() != 1 {
					t.Fatalf("ownership fix must retain parsed-source reuse: opens=%d closes=%d", objects.opens.Load(), objects.closes.Load())
				}
			})
		}
	}
}
