package outbound

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func submissionActor(user, key *uuid.UUID) string {
	return SubmissionActor(user, key)
}

// SubmissionActor is the durable command identity, shared by idempotent
// lookup and safe POST replay projection. Administrative roles do not widen it.
func SubmissionActor(user, key *uuid.UUID) string {
	if key != nil {
		return "key:" + key.String()
	}
	if user != nil {
		return "user:" + user.String()
	}
	return ""
}
func requestDigest(req SendRequest) string {
	var draftID *uuid.UUID
	var draftRevision int
	if req.Draft != nil {
		id := req.Draft.ID
		draftID, draftRevision = &id, req.Draft.Revision
	}
	return company.Digest(struct {
		From                string
		To, CC, BCC         []string
		Subject, Text, HTML string
		Headers             map[string]string
		Version             *uuid.UUID
		Vars                map[string]string
		Attachments         []uuid.UUID
		DraftID             *uuid.UUID
		DraftRevision       int
	}{req.From, req.To, req.CC, req.BCC, req.Subject, req.TextBody, req.HTMLBody, req.Headers, req.TemplateVersionID, req.TemplateVars, req.AttachmentIDs, draftID, draftRevision})
}
func contentDigest(j *models.OutboundJob) string {
	return company.Digest(struct {
		From                string
		To, CC, BCC         []string
		Subject, Text, HTML string
		Headers             map[string]string
		Version             *uuid.UUID
		Attachments         []uuid.UUID
	}{j.MailFrom, append([]string{}, j.To...), append([]string{}, j.CC...), append([]string{}, j.BCC...), j.Subject, j.TextBody, j.HTMLBody, parseCustomHeaders(j.HeadersJSON), j.TemplateVersionID, j.AttachmentIDs})
}

// SetObjectStore supplies the same attachment store used by upload handlers.
func (s *Service) SetObjectStore(st store.ObjectStore) { s.objects = st }
func (s *Service) buildQueuedMIME(ctx context.Context, j *models.OutboundJob) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := messageFromJob(j)
	if len(j.AttachmentIDs) > 0 {
		if len(j.AttachmentIDs) > 10 {
			return nil, fmt.Errorf("too many attachments")
		}
		pinned := make(map[uuid.UUID]struct{}, len(j.AttachmentIDs))
		for _, id := range j.AttachmentIDs {
			if _, duplicate := pinned[id]; id == uuid.Nil || duplicate {
				return nil, fmt.Errorf("invalid pinned attachment set")
			}
			pinned[id] = struct{}{}
		}
		repo, ok := s.store.(interface {
			OutboundAttachments(context.Context, uuid.UUID) ([]company.Attachment, error)
		})
		if !ok || s.objects == nil {
			return nil, fmt.Errorf("attachment delivery unavailable")
		}
		rows, e := repo.OutboundAttachments(ctx, j.ID)
		if e != nil {
			return nil, e
		}
		if len(rows) != len(j.AttachmentIDs) {
			return nil, fmt.Errorf("pinned attachment set incomplete")
		}
		// A matching count is insufficient: a duplicate or substituted row
		// must never supply bytes for an attachment fixed by this submission.
		// Check the whole relation before opening any object.
		var total int64
		for _, a := range rows {
			if _, expected := pinned[a.ID]; !expected {
				return nil, fmt.Errorf("pinned attachment set mismatch")
			}
			delete(pinned, a.ID)
			if a.State != "ready" || a.Size < 0 || a.Size > 20*1024*1024 {
				return nil, fmt.Errorf("invalid stored attachment")
			}
			total += a.Size
			if total > 20*1024*1024 {
				return nil, fmt.Errorf("attachment budget exceeded")
			}
			if len(a.Filename) == 0 || len(a.Filename) > 200 || strings.ContainsAny(a.Filename, "\r\n/\\\x00") {
				return nil, fmt.Errorf("invalid attachment")
			}
		}
		for _, a := range rows {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r, e := s.objects.Get(ctx, a.ObjectKey)
			if e != nil {
				// A store can supply a partial resource alongside its error.
				// The worker owns that resource even though it must not read it.
				var closeErr error
				if r != nil {
					closeErr = r.Close()
				}
				return nil, errors.Join(e, closeErr, ctx.Err())
			}
			b, e := readQueuedAttachment(ctx, r, a.Size+1)
			if e != nil {
				return nil, e
			}
			if int64(len(b)) != a.Size || company.Hash(string(b)) != a.SHA256 {
				return nil, fmt.Errorf("attachment integrity mismatch")
			}
			m.Attachments = append(m.Attachments, Attachment{Filename: a.Filename, ContentType: a.ContentType, Data: b})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wire, err := Build(m)
	if canceled := ctx.Err(); canceled != nil {
		return nil, errors.Join(err, canceled)
	}
	return wire, err
}
