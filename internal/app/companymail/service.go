// Package companymail owns employee mail-content use cases. Its inputs are
// identities and resource IDs, never HTTP requests, handlers or storage keys.
package companymail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
)

const (
	MaxAttachmentBytes int64 = 20 * 1024 * 1024
	MaxMessageBytes    int64 = 25 * 1024 * 1024
)

// Repository is deliberately smaller than company.Repository or store.Store.
// Implementations must reload current member and mailbox rights on each call.
// Atomic draft consumption and delivery ledgers remain in submissions.Service.
type Repository interface {
	GetWorkMailbox(context.Context, authz.Actor, uuid.UUID) (*company.MailboxAccess, error)
	GetWorkMessage(context.Context, authz.Actor, uuid.UUID, uuid.UUID) (*models.Message, error)
	company.AttachmentRepository
	company.SubmissionContentReader
}

type ObjectStore interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.Reader, int64) error
}

type Service struct {
	repo    Repository
	objects ObjectStore
	parser  *mailcontent.Parser
}

func NewService(repo Repository, objects ObjectStore) *Service {
	return &Service{repo: repo, objects: objects, parser: mailcontent.New(objects)}
}

// File is already authorized and verified. It contains neither an object key
// nor a repository reference that a transport could use to bypass permission.
type File struct {
	Filename string
	Content  []byte
}

type InboundAttachment = company.ParsedAttachment

func (s *Service) message(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*models.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m, err := s.repo.GetWorkMessage(ctx, a, mailbox, id)
	if err != nil {
		return nil, err
	}
	// Defense in depth against incorrect adapters; never open an object from
	// a mismatched tenant, mailbox, or message, even when addresses match.
	if m == nil || m.TenantID != a.TenantID || m.MailboxID != mailbox || m.ID != id {
		return nil, app.NotFound("message not found")
	}
	// Keep the observed provenance even if an adapter reuses its object.
	value := *m
	return &value, nil
}

func (s *Service) Message(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*models.MessageDetail, error) {
	m, err := s.message(ctx, a, mailbox, id)
	if err != nil {
		return nil, err
	}
	v := &models.MessageDetail{Message: *m}
	v.RawObjectKey = ""
	if m.RawObjectKey == "" {
		return v, nil
	}
	doc, err := s.document(ctx, a, mailbox, m)
	if err != nil {
		return nil, err
	}
	v.TextBody, v.HTMLBody, v.BodyAccess = doc.TextBody, doc.HTMLBody, doc.BodyAccess
	return v, nil
}

func (s *Service) Source(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (io.ReadCloser, error) {
	m, err := s.message(ctx, a, mailbox, id)
	if err != nil {
		return nil, err
	}
	r, err := s.open(ctx, m.RawObjectKey)
	if err != nil {
		return nil, err
	}
	check := func() error { return s.recheckMessage(ctx, a, mailbox, m) }
	if err = check(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return &authorizedSource{ReadCloser: r, check: check}, nil
}

func (s *Service) open(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, app.NotFound("raw source not available")
	}
	if s.objects == nil {
		return nil, app.Internal(errors.New("object storage unavailable"))
	}
	r, err := s.objects.Get(ctx, key)
	if err != nil {
		return nil, app.Internal(err)
	}
	if r == nil {
		return nil, app.Internal(errors.New("object storage returned no reader"))
	}
	return r, nil
}

func (s *Service) envelope(ctx context.Context, key string) (*enmime.Envelope, error) {
	return s.parser.Envelope(ctx, key)
}
func (s *Service) document(ctx context.Context, a authz.Actor, mailbox uuid.UUID, m *models.Message) (*company.ParsedMessage, error) {
	if cache, ok := s.repo.(company.ParsedContentReader); ok {
		doc, e := cache.GetParsedMessage(ctx, a, mailbox, m.ID)
		if e != nil {
			return nil, e
		}
		if doc != nil {
			// A cache is derived content, never a replacement authority or a
			// license to mix metadata with a different immutable source.
			if doc.MessageID != m.ID || doc.SourceKey != m.RawObjectKey || doc.ParserVersion != 1 {
				return nil, app.Conflict("message source changed; reload")
			}
			if e = s.recheckMessage(ctx, a, mailbox, m); e != nil {
				return nil, e
			}
			return doc, nil
		}
	}
	doc, e := s.parser.Document(ctx, m.ID, m.RawObjectKey)
	if e != nil {
		return nil, app.Internal(e)
	}
	if cache, ok := s.repo.(company.ParsedContentReader); ok {
		if e = cache.SaveParsedMessage(ctx, a, mailbox, *doc); e != nil {
			return nil, e
		}
	}
	if e = s.recheckMessage(ctx, a, mailbox, m); e != nil {
		return nil, e
	}
	return doc, nil
}

// Return the observed source as well as derived bytes so multi-step compose
// can retain provenance across destination uploads without reparsing the MIME.
func (s *Service) inboundEnvelope(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) (*enmime.Envelope, *models.Message, error) {
	m, err := s.message(ctx, a, mailbox, id)
	if err != nil {
		return nil, nil, err
	}
	env, err := s.envelope(ctx, m.RawObjectKey)
	if err != nil {
		return nil, nil, err
	}
	if err = s.recheckMessage(ctx, a, mailbox, m); err != nil {
		return nil, nil, err
	}
	return env, m, nil
}

func envelopeFiles(env *enmime.Envelope) []*enmime.Part {
	return append(append([]*enmime.Part{}, env.Attachments...), env.Inlines...)
}

// SafeFilename is shared by inbound, uploaded and forwarded attachments.
func SafeFilename(name string) string { return mailcontent.SafeFilename(name) }

func (s *Service) InboundAttachments(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID) ([]InboundAttachment, error) {
	m, e := s.message(ctx, a, mailbox, id)
	if e != nil {
		return nil, e
	}
	d, e := s.document(ctx, a, mailbox, m)
	if e != nil {
		return nil, e
	}
	return d.Parts, nil
}
func (s *Service) InboundAttachmentByID(ctx context.Context, a authz.Actor, mailbox, message uuid.UUID, id string) (*File, error) {
	m, e := s.message(ctx, a, mailbox, message)
	if e != nil {
		return nil, e
	}
	part, raw, e := s.parser.Attachment(ctx, message, m.RawObjectKey, id)
	if e != nil {
		return nil, app.NotFound("attachment not found")
	}
	if e = s.recheckMessage(ctx, a, mailbox, m); e != nil {
		return nil, e
	}
	return &File{Filename: part.Filename, Content: raw}, nil
}

func (s *Service) InboundAttachment(ctx context.Context, a authz.Actor, mailbox, id uuid.UUID, index int) (*File, error) {
	if index < 0 {
		return nil, app.NotFound("attachment not found")
	}
	env, _, err := s.inboundEnvelope(ctx, a, mailbox, id)
	if err != nil {
		return nil, err
	}
	files := envelopeFiles(env)
	if index >= len(files) {
		return nil, app.NotFound("attachment not found")
	}
	p := files[index]
	return &File{Filename: SafeFilename(p.FileName), Content: p.Content}, nil
}

func (s *Service) sender(ctx context.Context, a authz.Actor, mailbox uuid.UUID) (*company.MailboxAccess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mb, err := s.repo.GetWorkMailbox(ctx, a, mailbox)
	if err != nil {
		return nil, err
	}
	if mb == nil || mb.Mailbox.ID != mailbox || mb.Mailbox.TenantID != a.TenantID {
		return nil, app.NotFound("mailbox not found")
	}
	if !mb.CanSend {
		return nil, app.Forbidden("send_as permission required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Keep the observed destination address even if an adapter reuses its value.
	value := *mb
	return &value, nil
}

func digest(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (s *Service) UploadAttachment(ctx context.Context, a authz.Actor, mailbox uuid.UUID, filename string, input io.Reader) (*company.Attachment, error) {
	if _, err := s.sender(ctx, a, mailbox); err != nil {
		return nil, err
	}
	if s.objects == nil || input == nil {
		return nil, app.Internal(errors.New("attachment storage unavailable"))
	}
	raw, err := io.ReadAll(io.LimitReader(input, MaxAttachmentBytes+1))
	if err != nil {
		return nil, app.BadRequest("unable to read attachment")
	}
	if int64(len(raw)) > MaxAttachmentBytes {
		return nil, app.BadRequest("attachment exceeds 20 MiB")
	}
	v, err := s.repo.ReserveMailAttachment(ctx, a, company.Attachment{MailboxID: mailbox, Filename: SafeFilename(filename), Size: int64(len(raw))})
	if err != nil {
		return nil, err
	}
	if err = s.objects.Put(ctx, v.ObjectKey, bytes.NewReader(raw), int64(len(raw))); err != nil {
		return nil, app.Internal(err)
	}
	if err = s.repo.FinishMailAttachment(ctx, a, v.ID, digest(raw)); err != nil {
		// Keep the reservation for the existing retention/reconciliation path;
		// never claim ready or delete an object after an ambiguous DB commit.
		return nil, err
	}
	v.State = "ready"
	return v, nil
}

func (s *Service) verifiedFile(ctx context.Context, key, filename, state string, size int64, hash string) (*File, error) {
	// Check metadata before opening/allocating. Overflow, negative sizes and
	// unfinished or corrupt objects must not produce a successful download.
	if state != "ready" || size < 0 || size > MaxAttachmentBytes || len(hash) != 64 {
		return nil, app.Internal(errors.New("invalid attachment metadata"))
	}
	r, err := s.open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil || int64(len(raw)) != size || digest(raw) != hash {
		return nil, app.Internal(errors.New("attachment integrity check failed"))
	}
	return &File{Filename: SafeFilename(filename), Content: raw}, nil
}

func (s *Service) Attachment(ctx context.Context, a authz.Actor, id uuid.UUID) (*File, error) {
	v, err := s.repo.GetWorkAttachment(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if v == nil || v.ID != id {
		return nil, app.NotFound("attachment not found")
	}
	observed := *v
	file, err := s.verifiedFile(ctx, observed.ObjectKey, observed.Filename, observed.State, observed.Size, observed.SHA256)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	current, err := s.repo.GetWorkAttachment(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if current == nil || current.ID != observed.ID || current.ObjectKey != observed.ObjectKey || current.SHA256 != observed.SHA256 || current.Size != observed.Size || current.State != observed.State {
		return nil, app.Conflict("attachment changed; reload")
	}
	return file, nil
}

func (s *Service) SubmissionContent(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.SubmissionContent, error) {
	return s.repo.GetSubmissionContent(ctx, a, id)
}

func (s *Service) SubmissionAttachments(ctx context.Context, a authz.Actor, id uuid.UUID) ([]company.SubmissionAttachment, error) {
	return s.repo.ListSubmissionAttachments(ctx, a, id)
}

func (s *Service) SubmissionAttachment(ctx context.Context, a authz.Actor, id, attachment uuid.UUID) (*File, error) {
	v, err := s.repo.GetSubmissionAttachment(ctx, a, id, attachment)
	if err != nil {
		return nil, err
	}
	if v == nil || v.ID != attachment {
		return nil, app.NotFound("attachment not found")
	}
	observed := *v
	file, err := s.verifiedFile(ctx, observed.ObjectKey, observed.Filename, observed.State, observed.Size, observed.SHA256)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Re-resolve through the sent-asset authority, not uploader or job identity.
	current, err := s.repo.GetSubmissionAttachment(ctx, a, id, attachment)
	if err != nil {
		return nil, err
	}
	if current == nil || current.ID != observed.ID || current.ObjectKey != observed.ObjectKey || current.SHA256 != observed.SHA256 || current.Size != observed.Size || current.State != observed.State {
		return nil, app.Conflict("attachment changed; reload")
	}
	return file, nil
}
