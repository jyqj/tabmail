package handlers

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/companymail"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

type mailWorkspace interface {
	company.DraftWorkspace
	company.MailReadService
	company.SubmissionReceipts
}

// CompanyMailHandler only adapts HTTP. MIME, attachment integrity, source
// authorization and reply/forward rules belong to companymail.Service.
type CompanyMailHandler struct {
	repo   mailWorkspace
	mail   *companymail.Service
	subs   *submissions.Service
	logger zerolog.Logger
}

func NewCompanyMailHandler(repo mailWorkspace, mail *companymail.Service, subs *submissions.Service, logger zerolog.Logger) *CompanyMailHandler {
	return &CompanyMailHandler{repo: repo, mail: mail, subs: subs, logger: logger.With().Str("handler", "company_mail").Logger()}
}

func (h *CompanyMailHandler) result(w http.ResponseWriter, v any, err error) {
	if err != nil {
		if authz.IsAuthzError(err) {
			err = app.Forbidden(err.Error())
		}
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, v)
}

func companyMessageIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	mailbox, valid := companyID(w, r, "id")
	if !valid {
		return uuid.Nil, uuid.Nil, false
	}
	message, valid := companyID(w, r, "message")
	return mailbox, message, valid
}

func (h *CompanyMailHandler) Messages(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	page := pageFromReq(r)
	v, total, err := h.repo.ListWorkMessages(r.Context(), companyActor(r), id, r.URL.Query().Get("folder"), r.URL.Query().Get("q"), page)
	if err != nil {
		h.result(w, nil, err)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}

func (h *CompanyMailHandler) Message(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	v, err := h.mail.Message(r.Context(), companyActor(r), mailbox, message)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) MessageAction(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	v, valid := companyBody[struct {
		Action string `json:"action"`
	}](w, r)
	if !valid {
		return
	}
	h.result(w, map[string]bool{"updated": true}, h.repo.MutateWorkMessage(r.Context(), companyActor(r), mailbox, message, v.Action))
}

func (h *CompanyMailHandler) Source(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	source, err := h.mail.Source(r.Context(), companyActor(r), mailbox, message)
	if err != nil {
		h.result(w, nil, err)
		return
	}
	defer source.Close()
	downloadHeaders(w, "message.eml", "message/rfc822")
	_, _ = io.Copy(w, source)
}

func downloadHeaders(w http.ResponseWriter, name, ct string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
}

func (h *CompanyMailHandler) file(w http.ResponseWriter, file *companymail.File, err error) {
	if err != nil {
		h.result(w, nil, err)
		return
	}
	downloadHeaders(w, file.Filename, "application/octet-stream")
	_, _ = w.Write(file.Content)
}

func (h *CompanyMailHandler) InboundAttachments(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	v, err := h.mail.InboundAttachments(r.Context(), companyActor(r), mailbox, message)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) InboundAttachment(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		errNotFound(w, "attachment not found")
		return
	}
	v, err := h.mail.InboundAttachment(r.Context(), companyActor(r), mailbox, message, index)
	h.file(w, v, err)
}

func (h *CompanyMailHandler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, companymail.MaxAttachmentBytes+1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		errBadRequest(w, "invalid or oversized multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	input, header, err := r.FormFile("file")
	if err != nil {
		errBadRequest(w, "file required")
		return
	}
	defer input.Close()
	v, err := h.mail.UploadAttachment(r.Context(), companyActor(r), id, header.Filename, input)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) Attachment(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, err := h.mail.Attachment(r.Context(), companyActor(r), id)
	h.file(w, v, err)
}

func (h *CompanyMailHandler) Submissions(w http.ResponseWriter, r *http.Request) {
	page := pageFromReq(r)
	v, total, err := h.repo.ListSubmissions(r.Context(), companyActor(r), page)
	if err != nil {
		h.result(w, nil, err)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}

func (h *CompanyMailHandler) Submission(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, err := h.repo.GetSubmission(r.Context(), companyActor(r), id)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) SubmissionContent(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, err := h.mail.SubmissionContent(r.Context(), companyActor(r), id)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) SubmissionAttachments(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, err := h.mail.SubmissionAttachments(r.Context(), companyActor(r), id)
	h.result(w, v, err)
}

func (h *CompanyMailHandler) SubmissionAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	attachment, valid := companyID(w, r, "aid")
	if !valid {
		return
	}
	v, err := h.mail.SubmissionAttachment(r.Context(), companyActor(r), id, attachment)
	h.file(w, v, err)
}

func (h *CompanyMailHandler) ComposeReply(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	v, valid := companyBody[struct {
		Mode        string    `json:"mode"`
		FromMailbox uuid.UUID `json:"from_mailbox_id"`
	}](w, r)
	if !valid {
		return
	}
	out, err := h.mail.Compose(r.Context(), companyActor(r), mailbox, message, v.FromMailbox, v.Mode)
	h.result(w, out, err)
}

func (h *CompanyMailHandler) Drafts(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListMailDrafts(r.Context(), companyActor(r))
	h.result(w, v, e)
}
func (h *CompanyMailHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.Draft](w, r)
	if !ok {
		return
	}
	if r.Method == "PUT" {
		id, ok := companyID(w, r, "id")
		if !ok {
			return
		}
		v.ID = id
	} else {
		v.ID = uuid.Nil
	}
	out, e := h.repo.SaveMailDraft(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}
func (h *CompanyMailHandler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	rev, e := strconv.Atoi(r.URL.Query().Get("revision"))
	if e != nil {
		errBadRequest(w, "draft revision required")
		return
	}
	h.result(w, map[string]bool{"deleted": true}, h.repo.DeleteMailDraft(r.Context(), companyActor(r), id, rev))
}

// submitDraftRequest is the JSON body for POST /company/drafts/{id}/submit.
type submitDraftRequest struct {
	ExpectedRevision int `json:"expected_revision"`
}

// writeSubmitFailure maps a submissions service failure to the exact HTTP
// response the draft submit endpoint has always produced. Order, status codes
// and body shapes are pinned by the integration tests.
func writeSubmitFailure(w http.ResponseWriter, logger zerolog.Logger, f *submissions.Failure) {
	switch f.Kind {
	case submissions.FailureAuthRequired:
		errForbidden(w, "authentication required")
	case submissions.FailureBadRequest:
		errBadRequest(w, f.Message)
	case submissions.FailureForbidden:
		errForbidden(w, f.Message)
	case submissions.FailureInternal:
		errInternal(w)
	case submissions.FailureQuota:
		writeJSON(w, http.StatusTooManyRequests, envelope{
			Error: &apiErr{Code: "QUOTA_EXCEEDED", Message: f.Message},
		})
	case submissions.FailureConflict:
		errConflict(w, f.Message)
	case submissions.FailureRevisionConflict:
		writeJSON(w, http.StatusConflict, envelope{
			Data:  map[string]any{"revision": f.Revision},
			Error: &apiErr{Code: "CONFLICT", Message: "draft revision changed; refresh the draft before retrying"},
		})
	case submissions.FailureApp:
		respondAppError(w, logger, f.Err)
	default:
		errBadRequest(w, f.Message)
	}
}

// SubmitDraft handles POST /company/drafts/{id}/submit — submit the draft as
// an outbound job and consume the draft inside the enqueue transaction. The
// Idempotency-Key header is required; a repeated key replays the original job
// with 200 instead of creating a second one. The orchestration lives in the
// submissions use-case service; this handler parses the request and maps the
// outcome.
func (h *CompanyMailHandler) SubmitDraft(w http.ResponseWriter, r *http.Request) {
	id, idOK := companyID(w, r, "id")
	if !idOK {
		return
	}
	body, bodyOK := companyBody[submitDraftRequest](w, r)
	if !bodyOK {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		errBadRequest(w, "idempotency key header is required")
		return
	}
	if len(key) > 128 || strings.ContainsAny(key, "\r\n") {
		errBadRequest(w, "idempotency key must be at most 128 bytes without CR/LF")
		return
	}
	if body.ExpectedRevision < 1 {
		errBadRequest(w, "expected_revision is required")
		return
	}

	job, replayed, failure := h.subs.SubmitDraft(r.Context(), middleware.TenantFromCtx(r.Context()), companyActor(r), id, body.ExpectedRevision, key)
	if failure != nil {
		writeSubmitFailure(w, h.logger, failure)
		return
	}
	if replayed {
		ok(w, job)
		return
	}
	created(w, job)
}
