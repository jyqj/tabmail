package outbound

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func submissionActor(user, key *uuid.UUID) string {
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
	m := messageFromJob(j)
	if len(j.AttachmentIDs) > 0 {
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
		var total int64
		for _, a := range rows {
			if a.State != "ready" || a.Size < 0 || a.Size > 20*1024*1024 {
				return nil, fmt.Errorf("invalid stored attachment")
			}
			total += a.Size
			if total > 20*1024*1024 {
				return nil, fmt.Errorf("attachment budget exceeded")
			}
			r, e := s.objects.Get(ctx, a.ObjectKey)
			if e != nil {
				return nil, e
			}
			b, e := io.ReadAll(io.LimitReader(r, a.Size+1))
			closeErr := r.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if int64(len(b)) != a.Size || company.Hash(string(b)) != a.SHA256 {
				return nil, fmt.Errorf("attachment integrity mismatch")
			}
			m.Attachments = append(m.Attachments, Attachment{Filename: a.Filename, ContentType: a.ContentType, Data: b})
		}
	}
	return Build(m)
}
