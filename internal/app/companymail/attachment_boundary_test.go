package companymail

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
)

func TestCompanyAttachmentRechecksAfterReadingBytes(t *testing.T) {
	for _, sent := range []bool{false, true} {
		name := "uploaded"
		if sent {
			name = "submission"
		}
		for _, change := range []string{"revoked", "expired", "object", "hash", "size", "state"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				_, repo, _, actor, _, _ := contentFixture()
				id := uuid.New()
				repo.attachment = &company.Attachment{ID: id, ObjectKey: "original", Filename: "fixture.txt", State: "ready", Size: 3, SHA256: digest([]byte("abc"))}
				objects := &boundaryObjects{raw: []byte("abc"), onRead: func() {
					switch change {
					case "revoked":
						repo.denied = app.Forbidden("mailbox permission revoked")
					case "expired":
						repo.denied = app.NotFound("attachment unavailable")
					case "object":
						repo.attachment.ObjectKey = "replaced"
					case "hash":
						repo.attachment.SHA256 = digest([]byte("xyz"))
					case "size":
						repo.attachment.Size = 4
					case "state":
						repo.attachment.State = "uploading"
					}
				}}
				svc := NewService(repo, objects)
				var file *File
				var err error
				if sent {
					file, err = svc.SubmissionAttachment(context.Background(), actor, uuid.New(), id)
				} else {
					file, err = svc.Attachment(context.Background(), actor, id)
				}
				if err == nil || file != nil {
					t.Fatal("attachment bytes released after authority or pinned identity changed")
				}
				if objects.closes.Load() != 1 {
					t.Fatal("attachment reader leaked")
				}
			})
		}
	}
}
