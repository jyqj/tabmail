package companymail

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
)

// Equal IDs and equal raw keys are not sufficient evidence of the same
// received record. ReceivedAt/ZoneID bind the observed record across I/O.
// This does not claim a schema-level incarnation token for identical rebuilds.
func TestR5ReceivedRecordBindingAcrossIO(t *testing.T) {
	for _, change := range []string{"received-at", "zone"} {
		for _, endpoint := range []string{"detail", "source", "ordinal", "stable-part", "warm-cache", "first-byte"} {
			t.Run(change+"/"+endpoint, func(t *testing.T) {
				s, repo, objects, actor, mailbox, message := boundaryFixture()
				replace := func() {
					repo.mu.Lock()
					defer repo.mu.Unlock()
					if change == "zone" {
						repo.message.ZoneID = uuid.New()
					} else {
						repo.message.ReceivedAt = time.Now().UTC()
					}
				}
				var err error
				exposed := false
				switch endpoint {
				case "warm-cache":
					cache := &boundaryCache{boundaryRepository: repo, doc: &company.ParsedMessage{MessageID: message, SourceKey: "original-key", ParserVersion: 1, TextBody: "private cached body"}, onGet: replace}
					v, e := NewService(cache, objects).Message(context.Background(), actor, mailbox, message)
					err = e
					exposed = v != nil
				case "first-byte":
					objects.onRead = replace
					r, e := s.Source(context.Background(), actor, mailbox, message)
					if e != nil {
						t.Fatal(e)
					}
					defer r.Close()
					buf := make([]byte, 100)
					n, e := r.Read(buf)
					err = e
					exposed = n != 0
					for _, b := range buf {
						if b != 0 {
							t.Fatal("rejected first-byte buffer retained content")
						}
					}
				default:
					part := ""
					if endpoint == "stable-part" {
						doc, e := s.parser.Document(context.Background(), message, "original-key")
						if e != nil {
							t.Fatal(e)
						}
						part = doc.Parts[0].ID
						s = NewService(repo, objects)
					}
					objects.onOpen = replace
					switch endpoint {
					case "detail":
						v, e := s.Message(context.Background(), actor, mailbox, message)
						err = e
						exposed = v != nil
					case "source":
						v, e := s.Source(context.Background(), actor, mailbox, message)
						err = e
						exposed = v != nil
						if v != nil {
							v.Close()
						}
					case "ordinal":
						v, e := s.InboundAttachment(context.Background(), actor, mailbox, message, 0)
						err = e
						exposed = v != nil
					case "stable-part":
						v, e := s.InboundAttachmentByID(context.Background(), actor, mailbox, message, part)
						err = e
						exposed = v != nil
					}
				}
				if exposed {
					t.Fatal("same ID/key replacement released old content")
				}
				value, ok := app.As(err)
				if !ok || value.Kind != app.KindConflict {
					t.Fatalf("want record generation conflict, got %v", err)
				}
			})
		}
	}
}

func TestR5ReceivedEmptyEOFRechecksAuthority(t *testing.T) {
	for _, change := range []string{"removed", "revoked", "database-error", "source-replaced", "active"} {
		t.Run(change, func(t *testing.T) {
			s, repo, objects, actor, mailbox, message := boundaryFixture()
			objects.raw = nil
			if change != "active" {
				objects.onRead = func() { repo.change(change) }
			}
			source, e := s.Source(context.Background(), actor, mailbox, message)
			if e != nil {
				t.Fatal(e)
			}
			defer source.Close()
			buf := make([]byte, 10)
			n, e := source.Read(buf)
			if change == "active" {
				if n != 0 || e != io.EOF {
					t.Fatalf("active empty source: %d %v", n, e)
				}
				return
			}
			if n != 0 || e == nil || e == io.EOF {
				t.Fatalf("empty terminal read bypassed authority: %d %v", n, e)
			}
			if objects.closes.Load() != 1 {
				t.Fatal("denied empty source did not close")
			}
			n, e = source.Read(buf)
			if n != 0 || e == nil || e == io.EOF {
				t.Fatal("denied empty source resumed")
			}
		})
	}
}
