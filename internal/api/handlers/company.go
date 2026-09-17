package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type CompanyHandler struct {
	repo     company.Repository
	store    store.Store
	objects  store.ObjectStore
	messages *MessageHandler
	outbound *OutboundHandler
	domains  *CompanyDomainHandler
	logger   zerolog.Logger
}

func NewCompanyHandler(repo company.Repository, st store.Store, obj store.ObjectStore, m *MessageHandler, o *OutboundHandler, d *CompanyDomainHandler, l zerolog.Logger) *CompanyHandler {
	return &CompanyHandler{repo: repo, store: st, objects: obj, messages: m, outbound: o, domains: d, logger: l.With().Str("handler", "company").Logger()}
}
func (h *CompanyHandler) Routes(r chi.Router) {
	r.Post("/company/activate", h.Activate)
	r.Route("/company", func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Get("/settings", h.Settings)
		r.Put("/settings", h.Configure)
		r.Get("/invitations", h.Invitations)
		r.Post("/invitations", h.Invite)
		r.Delete("/invitations/{id}", h.RevokeInvite)
		r.Post("/employees/{id}/offboard", h.Offboard)
		r.Get("/mailboxes", h.Mailboxes)
		r.Post("/mailboxes", h.CreateMailbox)
		r.Post("/mailboxes/{id}/handover", h.Handover)
		r.Post("/mailboxes/{id}/convert-shared", h.ConvertShared)
		r.Get("/mailboxes/{id}/grants", h.Grants)
		r.Put("/mailboxes/{id}/grants", h.Grant)
		// Mailbox send-policy override (tenant administrators only). The
		// handler's repo call re-checks actor.IsTenantAdmin inside the
		// transaction, mirroring the double guard on the domain routes.
		r.With(middleware.RequireAdmin).Put("/mailboxes/{id}/send-policy", h.MailboxSendPolicy)
		r.Get("/templates", h.Templates)
		r.Post("/templates", h.SaveTemplate)
		r.Put("/templates/{id}", h.SaveTemplate)
		r.Post("/templates/preview", h.Preview)
		r.Post("/templates/{id}/publish", h.Publish)
		r.Post("/templates/{id}/retire", h.Retire)
		r.Get("/templates/{id}/versions", h.Versions)
		r.Get("/templates/{id}/grants", h.TemplateGrants)
		r.Put("/templates/{id}/grants", h.TemplateGrant)
		r.Get("/mailboxes/{id}/templates", h.UsableTemplates)
		r.Get("/mailboxes/{id}/events", h.MailboxEvents)
		r.Get("/mailboxes/{id}/messages", h.Messages)
		r.Get("/mailboxes/{id}/messages/{message}", h.Message)
		r.Post("/mailboxes/{id}/messages/{message}/actions", h.MessageAction)
		r.Post("/mailboxes/{id}/messages/{message}/compose", h.ComposeReply)
		r.Get("/mailboxes/{id}/messages/{message}/source", h.Source)
		r.Get("/mailboxes/{id}/messages/{message}/attachments", h.InboundAttachments)
		r.Get("/mailboxes/{id}/messages/{message}/attachments/{index}", h.InboundAttachment)
		r.Post("/mailboxes/{id}/attachments", h.UploadAttachment)
		r.Get("/attachments/{id}", h.Attachment)
		r.Get("/drafts", h.Drafts)
		r.Post("/drafts", h.SaveDraft)
		r.Put("/drafts/{id}", h.SaveDraft)
		r.Post("/drafts/{id}/submit", h.SubmitDraft)
		r.Delete("/drafts/{id}", h.DeleteDraft)
		r.Get("/submissions", h.Submissions)
		r.Get("/submissions/{id}", h.Submission)
		r.Get("/recovery", h.Recovery)
		r.Post("/recovery/{id}/inspect", h.InspectReceipt)
		r.Post("/recovery/{id}/retry", h.RetryReceipt)
		r.Get("/outbound/{id}/recipients", h.Recipients)
		r.Post("/outbound/{id}/reconcile", h.Reconcile)
		r.With(middleware.RequireSuperAdmin).Post("/outbound/{id}/inspect", h.InspectOutbound)
		// Company domain onboarding (tenant administrators only). The handler
		// re-checks actor.IsTenantAdmin at the service boundary.
		if h.domains != nil {
			r.With(middleware.RequireAdmin).Post("/domains", h.domains.Create)
			r.With(middleware.RequireAdmin).Get("/domains", h.domains.List)
			r.With(middleware.RequireAdmin).Post("/domains/{id}/verify", h.domains.Verify)
			r.With(middleware.RequireAdmin).Get("/domains/{id}/verification", h.domains.Verification)
			r.With(middleware.RequireAdmin).Delete("/domains/{id}", h.domains.Delete)
		}
	})
}
func companyID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, e := uuid.Parse(chi.URLParam(r, name))
	if e != nil {
		errBadRequest(w, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}
func companyBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	if e := decodeBody(r, &v); e != nil {
		errBadRequest(w, "invalid request body")
		return v, false
	}
	return v, true
}
func (h *CompanyHandler) result(w http.ResponseWriter, v any, e error) {
	if e != nil {
		if authz.IsAuthzError(e) {
			e = app.Forbidden(e.Error())
		}
		respondAppError(w, h.logger, e)
		return
	}
	ok(w, v)
}
func companyActor(r *http.Request) authz.Actor { return middleware.ActorFromContext(r.Context()) }
func (h *CompanyHandler) Settings(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.GetCompanySettings(r.Context(), companyActor(r).TenantID)
	h.result(w, v, e)
}
func (h *CompanyHandler) Configure(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.Settings](w, r)
	if !ok {
		return
	}
	out, e := h.repo.ConfigureCompany(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}
func (h *CompanyHandler) Invitations(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListEmployeeInvitations(r.Context(), companyActor(r))
	h.result(w, v, e)
}
func (h *CompanyHandler) Invite(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.InvitationInput](w, r)
	if !ok {
		return
	}
	buf := make([]byte, 32)
	if _, e := rand.Read(buf); e != nil {
		errInternal(w)
		return
	}
	token := hex.EncodeToString(buf)
	out, e := h.repo.InviteEmployee(r.Context(), companyActor(r), v, company.Hash(token))
	if e != nil {
		h.result(w, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.result(w, map[string]any{"invitation": out, "activation_token": token}, nil)
}
func (h *CompanyHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	h.result(w, map[string]bool{"revoked": true}, h.repo.RevokeEmployeeInvitation(r.Context(), companyActor(r), id))
}
func (h *CompanyHandler) Activate(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}](w, r)
	if !ok {
		return
	}
	if len(v.Token) != 64 || len(v.Password) < 12 || len(v.Password) > 72 {
		errBadRequest(w, "activation token and 12-72 byte password required")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
	if e != nil {
		errInternal(w)
		return
	}
	h.result(w, map[string]bool{"activated": true}, h.repo.ActivateEmployee(r.Context(), company.Hash(v.Token), string(hash)))
}
func (h *CompanyHandler) Offboard(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Successor uuid.UUID `json:"successor_user_id"`
		Reason    string    `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"offboarded": true}, h.repo.OffboardEmployee(r.Context(), companyActor(r), id, v.Successor, v.Reason))
}
func (h *CompanyHandler) Mailboxes(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListWorkMailboxes(r.Context(), companyActor(r))
	h.result(w, v, e)
}
func (h *CompanyHandler) CreateMailbox(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.MailboxInput](w, r)
	if !ok {
		return
	}
	out, e := h.repo.CreateWorkMailbox(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}
func (h *CompanyHandler) Handover(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Owner    uuid.UUID `json:"owner_user_id"`
		Revision int64     `json:"revision"`
		Reason   string    `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"transferred": true}, h.repo.TransferWorkMailbox(r.Context(), companyActor(r), id, v.Owner, v.Revision, v.Reason))
}
func (h *CompanyHandler) Grants(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.ListWorkGrants(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}
func (h *CompanyHandler) Grant(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[models.MailboxGrant](w, r)
	if !ok {
		return
	}
	v.MailboxID = id
	v.TenantID = companyActor(r).TenantID
	h.result(w, map[string]bool{"updated": true}, h.repo.SetWorkGrant(r.Context(), companyActor(r), v))
}

// MailboxSendPolicy handles PUT /company/mailboxes/{id}/send-policy — the
// administrative per-mailbox override of the outbound send policy. A null (or
// empty) send_policy clears the override so the mailbox inherits the company
// default again; the effective value keeps flowing to clients on the mailbox
// views through Mailbox.SendPolicy.
func (h *CompanyHandler) MailboxSendPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Policy *string `json:"send_policy"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"updated": true}, h.repo.SetWorkMailboxSendPolicy(r.Context(), companyActor(r), id, v.Policy))
}
func (h *CompanyHandler) Templates(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListMailTemplates(r.Context(), companyActor(r))
	h.result(w, v, e)
}
func (h *CompanyHandler) SaveTemplate(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.Template](w, r)
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
	out, e := h.repo.SaveMailTemplate(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}
func (h *CompanyHandler) Publish(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Revision int `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	out, e := h.repo.PublishMailTemplate(r.Context(), companyActor(r), id, v.Revision)
	h.result(w, out, e)
}
func (h *CompanyHandler) Retire(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Revision int  `json:"revision"`
		Retired  bool `json:"retired"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"updated": true}, h.repo.SetMailTemplateRetired(r.Context(), companyActor(r), id, v.Revision, v.Retired))
}
func (h *CompanyHandler) Versions(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.ListTemplateVersions(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}
func (h *CompanyHandler) TemplateGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.ListTemplateGrants(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}
func (h *CompanyHandler) TemplateGrant(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Mailbox uuid.UUID `json:"mailbox_id"`
		User    uuid.UUID `json:"user_id"`
		Enabled bool      `json:"enabled"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"updated": true}, h.repo.SetTemplateGrant(r.Context(), companyActor(r), company.TemplateGrant{TemplateID: id, MailboxID: v.Mailbox, UserID: v.User}, v.Enabled))
}
func (h *CompanyHandler) UsableTemplates(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.ListUsableTemplates(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}
func (h *CompanyHandler) Preview(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[struct {
		Mailbox uuid.UUID              `json:"mailbox_id"`
		Version *uuid.UUID             `json:"template_version_id"`
		Draft   *company.TemplateDraft `json:"draft"`
		Vars    map[string]string      `json:"vars"`
	}](w, r)
	if !ok {
		return
	}
	a := companyActor(r)
	mb, e := h.repo.GetWorkMailbox(r.Context(), a, v.Mailbox)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	var draft company.TemplateDraft
	var employee, name string
	if v.Draft != nil {
		if !a.IsTenantAdmin() {
			errForbidden(w, "template editing requires administrator")
			return
		}
		draft = *v.Draft
		u, e := h.store.GetUser(r.Context(), a.ID)
		if e != nil || u == nil {
			errInternal(w)
			return
		}
		employee = u.DisplayName
		c, e := h.repo.GetCompanySettings(r.Context(), a.TenantID)
		if e != nil || c == nil {
			errBadRequest(w, "configure company first")
			return
		}
		name = c.Name
	} else if v.Version != nil {
		t, emp, n, e := h.repo.TemplateForSend(r.Context(), a.TenantID, &a.ID, nil, v.Mailbox, *v.Version)
		if e != nil {
			h.result(w, nil, e)
			return
		}
		draft = t.Snapshot
		employee = emp
		name = n
	} else {
		errBadRequest(w, "draft or published version required")
		return
	}
	subject, text, html, e := company.Render(draft, v.Vars, employee, name, mb.Mailbox.FullAddress)
	h.result(w, map[string]string{"subject": subject, "text_body": text, "html_body": html}, e)
}
// MailboxEvents serves GET /api/v1/company/mailboxes/{id}/events — the
// company-side SSE stream for one mailbox. Access is decided by company grants
// (read permission); the platform durable stream then carries the events.
func (h *CompanyHandler) MailboxEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	mb, e := h.repo.GetWorkMailbox(r.Context(), companyActor(r), id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if !mb.CanRead || h.messages.eventReader == nil {
		errForbidden(w, "mailbox read permission required")
		return
	}
	h.messages.streamDurable(w, r, &mb.Mailbox)
}

func (h *CompanyHandler) Messages(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	page := pageFromReq(r)
	v, total, e := h.repo.ListWorkMessages(r.Context(), companyActor(r), id, r.URL.Query().Get("folder"), r.URL.Query().Get("q"), page)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}

// Submissions serves GET /api/v1/company/submissions — the employee-facing
// projection of their own and their readable mailboxes' outbound submissions.
func (h *CompanyHandler) Submissions(w http.ResponseWriter, r *http.Request) {
	page := pageFromReq(r)
	v, total, e := h.repo.ListSubmissions(r.Context(), companyActor(r), page)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}

// Submission serves GET /api/v1/company/submissions/{id}.
func (h *CompanyHandler) Submission(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.GetSubmission(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}
func (h *CompanyHandler) mailboxForRead(w http.ResponseWriter, r *http.Request) (*company.MailboxAccess, uuid.UUID, bool) {
	mbID, ok := companyID(w, r, "id")
	if !ok {
		return nil, uuid.Nil, false
	}
	id, ok := companyID(w, r, "message")
	if !ok {
		return nil, id, false
	}
	mb, e := h.repo.GetWorkMailbox(r.Context(), companyActor(r), mbID)
	if e != nil {
		h.result(w, nil, e)
		return nil, id, false
	}
	if !mb.CanRead {
		errForbidden(w, "mailbox read permission required")
		return nil, id, false
	}
	return mb, id, true
}
func (h *CompanyHandler) Message(w http.ResponseWriter, r *http.Request) {
	mb, id, ok := h.mailboxForRead(w, r)
	if !ok {
		return
	}
	v, e := h.messages.service.GetMessageDetail(r.Context(), mb.Mailbox.FullAddress, id, h.messages.resolveViewer(r))
	if v != nil {
		v.RawObjectKey = ""
	}
	h.result(w, v, e)
}
func (h *CompanyHandler) MessageAction(w http.ResponseWriter, r *http.Request) {
	mb, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	id, ok := companyID(w, r, "message")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Action string `json:"action"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"updated": true}, h.repo.MutateWorkMessage(r.Context(), companyActor(r), mb, id, v.Action))
}
func (h *CompanyHandler) Source(w http.ResponseWriter, r *http.Request) {
	mb, id, ok := h.mailboxForRead(w, r)
	if !ok {
		return
	}
	rc, e := h.messages.service.GetRawSource(r.Context(), mb.Mailbox.FullAddress, id, h.messages.resolveViewer(r))
	if e != nil {
		h.result(w, nil, e)
		return
	}
	defer rc.Close()
	downloadHeaders(w, "message.eml", "message/rfc822")
	_, _ = io.Copy(w, rc)
}
func downloadHeaders(w http.ResponseWriter, name, ct string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
}
func (h *CompanyHandler) inboundEnvelope(w http.ResponseWriter, r *http.Request) (*enmime.Envelope, bool) {
	mb, id, ok := h.mailboxForRead(w, r)
	if !ok {
		return nil, false
	}
	rc, e := h.messages.service.GetRawSource(r.Context(), mb.Mailbox.FullAddress, id, h.messages.resolveViewer(r))
	if e != nil {
		h.result(w, nil, e)
		return nil, false
	}
	defer rc.Close()
	raw, e := io.ReadAll(io.LimitReader(rc, 26*1024*1024))
	if e != nil || len(raw) > 25*1024*1024 {
		errBadRequest(w, "message exceeds attachment viewer limit")
		return nil, false
	}
	env, e := enmime.ReadEnvelope(bytes.NewReader(raw))
	if e != nil {
		h.result(w, nil, app.Internal(e))
		return nil, false
	}
	return env, true
}
func envelopeFiles(env *enmime.Envelope) []*enmime.Part {
	return append(append([]*enmime.Part{}, env.Attachments...), env.Inlines...)
}
func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." {
		name = "attachment"
	}
	if len(name) > 180 {
		name = "attachment"
	}
	return name
}
func (h *CompanyHandler) InboundAttachments(w http.ResponseWriter, r *http.Request) {
	env, ok := h.inboundEnvelope(w, r)
	if !ok {
		return
	}
	out := []map[string]any{}
	for i, p := range envelopeFiles(env) {
		out = append(out, map[string]any{"index": i, "filename": safeFilename(p.FileName), "size": len(p.Content), "content_type": p.ContentType})
	}
	h.result(w, out, nil)
}
func (h *CompanyHandler) InboundAttachment(w http.ResponseWriter, r *http.Request) {
	env, ok := h.inboundEnvelope(w, r)
	if !ok {
		return
	}
	index, e := strconv.Atoi(chi.URLParam(r, "index"))
	files := envelopeFiles(env)
	if e != nil || index < 0 || index >= len(files) {
		errNotFound(w, "attachment not found")
		return
	}
	p := files[index]
	downloadHeaders(w, safeFilename(p.FileName), "application/octet-stream")
	_, _ = w.Write(p.Content)
}
func (h *CompanyHandler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 21*1024*1024)
	if e := r.ParseMultipartForm(1024 * 1024); e != nil {
		errBadRequest(w, "invalid or oversized multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, header, e := r.FormFile("file")
	if e != nil {
		errBadRequest(w, "file required")
		return
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 20*1024*1024+1))
	if e != nil || len(raw) > 20*1024*1024 {
		errBadRequest(w, "attachment exceeds 20 MiB")
		return
	}
	v, e := h.repo.ReserveMailAttachment(r.Context(), companyActor(r), company.Attachment{MailboxID: id, Filename: safeFilename(header.Filename), Size: int64(len(raw))})
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if h.objects == nil {
		errInternal(w)
		return
	}
	if e = h.objects.Put(r.Context(), v.ObjectKey, bytes.NewReader(raw), int64(len(raw))); e != nil {
		h.result(w, nil, app.Internal(e))
		return
	}
	e = h.repo.FinishMailAttachment(r.Context(), companyActor(r), v.ID, company.Hash(string(raw)))
	v.State = "ready"
	h.result(w, v, e)
}
func (h *CompanyHandler) Attachment(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.GetWorkAttachment(r.Context(), companyActor(r), id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	rc, e := h.objects.Get(r.Context(), v.ObjectKey)
	if e != nil {
		h.result(w, nil, app.Internal(e))
		return
	}
	defer rc.Close()
	raw, e := io.ReadAll(io.LimitReader(rc, v.Size+1))
	if e != nil || int64(len(raw)) != v.Size || company.Hash(string(raw)) != v.SHA256 {
		errInternal(w)
		return
	}
	downloadHeaders(w, v.Filename, "application/octet-stream")
	_, _ = w.Write(raw)
}
func (h *CompanyHandler) Drafts(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListMailDrafts(r.Context(), companyActor(r))
	h.result(w, v, e)
}
func (h *CompanyHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
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
func (h *CompanyHandler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
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

// SubmitDraft handles POST /company/drafts/{id}/submit — submit the draft as
// an outbound job and consume the draft inside the enqueue transaction. The
// Idempotency-Key header is required; a repeated key replays the original job
// with 200 instead of creating a second one.
func (h *CompanyHandler) SubmitDraft(w http.ResponseWriter, r *http.Request) {
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

	actor := companyActor(r)
	draft, e := h.repo.GetMailDraft(r.Context(), actor, id)
	if e != nil {
		if authz.IsAuthzError(e) {
			e = app.Forbidden(e.Error())
		}
		// A missing draft is only a plain 404 when no submission consumed it;
		// otherwise it is a replay (same key) or an already-consumed conflict.
		if appErr, isApp := app.As(e); isApp && appErr.Kind == app.KindNotFound {
			consumed, replay, e2 := h.outbound.ConsumedDraftSubmission(r.Context(), actor.TenantID, id, &actor.ID, key)
			if e2 != nil {
				h.logger.Err(e2).Msg("looking up consumed draft submission")
				errInternal(w)
				return
			}
			if consumed != nil {
				if replay {
					ok(w, consumed)
				} else {
					errConflict(w, "draft was already submitted; check its send status instead of retrying")
				}
				return
			}
		}
		respondAppError(w, h.logger, e)
		return
	}
	if draft.Revision != body.ExpectedRevision {
		// The draft still exists with a newer revision: tell the caller what to
		// refresh to, instead of a bare conflict.
		writeJSON(w, http.StatusConflict, envelope{
			Data:  map[string]any{"revision": draft.Revision},
			Error: &apiErr{Code: "CONFLICT", Message: "draft revision changed; refresh the draft before retrying"},
		})
		return
	}
	mb, e := h.store.GetMailbox(r.Context(), draft.MailboxID)
	if e != nil {
		h.logger.Err(e).Msg("loading draft mailbox")
		errInternal(w)
		return
	}
	if mb == nil || mb.TenantID != actor.TenantID {
		errConflict(w, "draft mailbox is no longer available")
		return
	}

	job, replayed, submitted := h.outbound.submitAuthorized(w, r, outboundSubmitInput{
		From:              mb.FullAddress,
		To:                draft.Payload.To,
		CC:                draft.Payload.CC,
		BCC:               draft.Payload.BCC,
		Subject:           draft.Payload.Subject,
		TextBody:          draft.Payload.TextBody,
		HTMLBody:          draft.Payload.HTMLBody,
		Headers:           draft.Payload.Headers,
		TemplateVersionID: draft.Payload.TemplateVersionID,
		TemplateVars:      draft.Payload.TemplateVars,
		AttachmentIDs:     draft.Payload.AttachmentIDs,
		IdempotencyKey:    key,
		Draft: &store.DraftConsumption{
			TenantID: actor.TenantID,
			UserID:   actor.ID,
			ID:       draft.ID,
			Revision: draft.Revision,
		},
	})
	if !submitted {
		return
	}
	if replayed {
		ok(w, job)
		return
	}
	created(w, job)
}
func (h *CompanyHandler) Recovery(w http.ResponseWriter, r *http.Request) {
	page := pageFromReq(r)
	v, total, e := h.repo.ListRecoveryReceipts(r.Context(), companyActor(r), page)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}
func (h *CompanyHandler) verifyReceipt(ctx context.Context, v *company.RecoveryReceipt) bool {
	if h.objects == nil || v.RawSize < 0 || v.RawSize > 25*1024*1024 || v.RawHash == "" {
		return false
	}
	rc, e := h.objects.Get(ctx, v.RawKey)
	if e != nil {
		return false
	}
	defer rc.Close()
	raw, e := io.ReadAll(io.LimitReader(rc, v.RawSize+1))
	return e == nil && int64(len(raw)) == v.RawSize && company.Hash(string(raw)) == v.RawHash
}
func (h *CompanyHandler) InspectReceipt(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	body, ok := companyBody[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	v, e := h.repo.InspectRecoveryReceipt(r.Context(), companyActor(r), id, body.Reason)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	h.result(w, map[string]any{"receipt": v, "original_valid": h.verifyReceipt(r.Context(), v)}, nil)
}
func (h *CompanyHandler) RetryReceipt(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	body, ok := companyBody[struct {
		UpdatedAt time.Time   `json:"updated_at"`
		Targets   []uuid.UUID `json:"targets"`
		Reason    string      `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	v, e := h.repo.InspectRecoveryReceipt(r.Context(), companyActor(r), id, body.Reason)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if !v.UpdatedAt.Equal(body.UpdatedAt) || !h.verifyReceipt(r.Context(), v) {
		errConflict(w, "receipt changed or original failed verification; inspect again")
		return
	}
	h.result(w, map[string]bool{"queued": true}, h.repo.RetryRecoveryReceipt(r.Context(), companyActor(r), id, body.UpdatedAt, body.Targets, body.Reason, v.RawHash))
}
func (h *CompanyHandler) Recipients(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	if h.outbound == nil {
		errNotFound(w, "outbound disabled")
		return
	}
	j, e := h.outbound.getAccessibleOutboundJob(r.Context(), id)
	if e != nil {
		h.outbound.writeOutboundJobAccessError(w, e, "listing recipient outcomes")
		return
	}
	view, e := h.outbound.redactOutboundJob(r.Context(), companyActor(r), j)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	rows, e := h.repo.ListOutboundRecipients(r.Context(), j.TenantID, id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if view.ContentRedacted {
		visible := map[string]bool{}
		for _, a := range view.RcptTo {
			visible[a] = true
		}
		filtered := []company.Recipient{}
		for _, row := range rows {
			if visible[row.Address] {
				row.Diagnostic = "Protocol details restricted"
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	h.result(w, rows, nil)
}
func (h *CompanyHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		UpdatedAt time.Time           `json:"updated_at"`
		Results   []company.Recipient `json:"results"`
		Reason    string              `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"reconciled": true}, h.repo.ReconcileOutbound(r.Context(), companyActor(r), id, v.UpdatedAt, v.Results, v.Reason))
}
func (h *CompanyHandler) InspectOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	if len(strings.TrimSpace(v.Reason)) < 8 || len(v.Reason) > 1000 {
		errBadRequest(w, "inspection reason must be 8-1000 bytes")
		return
	}
	if h.outbound == nil {
		errNotFound(w, "outbound disabled")
		return
	}
	j, e := h.outbound.getAccessibleOutboundJob(r.Context(), id)
	if e != nil {
		h.outbound.writeOutboundJobAccessError(w, e, "inspecting outbound")
		return
	}
	a := companyActor(r)
	e = app.InsertAuditRequired(r.Context(), h.store, models.AuditEntry{TenantID: &a.TenantID, Actor: a.AuditLabel(), Action: "outbound.break_glass", ResourceType: "outbound_job", ResourceID: &id, Details: app.MustJSON(map[string]any{"reason": v.Reason})})
	if e != nil {
		errInternal(w)
		return
	}
	cp := *j
	cp.DeliveryToken = nil
	cp.RawMIME = nil
	rows, e := h.repo.ListOutboundRecipients(r.Context(), a.TenantID, id)
	h.result(w, map[string]any{"job": cp, "recipients": rows}, e)
}

func (h *CompanyHandler) ConvertShared(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, valid := companyBody[struct {
		Revision int64  `json:"revision"`
		Reason   string `json:"reason"`
	}](w, r)
	if !valid {
		return
	}
	h.result(w, map[string]bool{"converted": true}, h.repo.ConvertSharedMailbox(r.Context(), companyActor(r), id, v.Revision, v.Reason))
}

// ComposeReply parses address lists on the server, not by splitting quoted
// display names in JavaScript. Forwarded attachments are copied into the new
// author's authorized draft workspace; no inbound object key is exposed.
func (h *CompanyHandler) ComposeReply(w http.ResponseWriter, r *http.Request) {
	v, valid := companyBody[struct {
		Mode        string    `json:"mode"`
		FromMailbox uuid.UUID `json:"from_mailbox_id"`
	}](w, r)
	if !valid {
		return
	}
	if v.Mode != "reply" && v.Mode != "reply_all" && v.Mode != "forward" {
		errBadRequest(w, "invalid compose mode")
		return
	}
	from, e := h.repo.GetWorkMailbox(r.Context(), companyActor(r), v.FromMailbox)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if !from.CanSend {
		errForbidden(w, "send_as permission required")
		return
	}
	env, valid := h.inboundEnvelope(w, r)
	if !valid {
		return
	}
	p := company.DraftPayload{To: []string{}, CC: []string{}, Subject: env.GetHeader("Subject"), TextBody: "\n\n--- " + env.GetHeader("From") + " · " + env.GetHeader("Date") + " ---\n" + env.Text}
	seen := map[string]bool{strings.ToLower(from.Mailbox.FullAddress): true}
	parse := func(raw string) ([]string, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		as, e := mail.ParseAddressList(raw)
		if e != nil {
			return nil, e
		}
		out := []string{}
		for _, a := range as {
			v := strings.ToLower(a.Address)
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out, nil
	}
	if v.Mode == "forward" {
		if !strings.HasPrefix(strings.ToLower(p.Subject), "fwd:") {
			p.Subject = "Fwd: " + p.Subject
		}
		files := env.Attachments
		if len(files) > 10 {
			errBadRequest(w, "too many attachments to forward; select attachments manually")
			return
		}
		total := 0
		for _, file := range files {
			total += len(file.Content)
		}
		if total > 20*1024*1024 {
			errBadRequest(w, "forward attachments exceed 20 MiB")
			return
		}
		for _, file := range files {
			a, e := h.repo.ReserveMailAttachment(r.Context(), companyActor(r), company.Attachment{MailboxID: v.FromMailbox, Filename: safeFilename(file.FileName), Size: int64(len(file.Content))})
			if e != nil {
				h.result(w, nil, e)
				return
			}
			if e = h.objects.Put(r.Context(), a.ObjectKey, bytes.NewReader(file.Content), a.Size); e != nil {
				h.result(w, nil, app.Internal(e))
				return
			}
			if e = h.repo.FinishMailAttachment(r.Context(), companyActor(r), a.ID, company.Hash(string(file.Content))); e != nil {
				h.result(w, nil, e)
				return
			}
			p.AttachmentIDs = append(p.AttachmentIDs, a.ID)
		}
	} else {
		replyTo := env.GetHeader("Reply-To")
		if replyTo == "" {
			replyTo = env.GetHeader("From")
		}
		p.To, e = parse(replyTo)
		if e != nil {
			errBadRequest(w, "message has an invalid reply address; compose manually")
			return
		}
		if v.Mode == "reply_all" {
			to, e := parse(env.GetHeader("To"))
			if e != nil {
				errBadRequest(w, "invalid original To header")
				return
			}
			p.To = append(p.To, to...)
			p.CC, e = parse(env.GetHeader("Cc"))
			if e != nil {
				errBadRequest(w, "invalid original Cc header")
				return
			}
		}
		if !strings.HasPrefix(strings.ToLower(p.Subject), "re:") {
			p.Subject = "Re: " + p.Subject
		}
		id := strings.TrimSpace(env.GetHeader("Message-Id"))
		refs := strings.TrimSpace(env.GetHeader("References"))
		if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") && !strings.ContainsAny(id, "\r\n") && len(id) <= 254 {
			refs = strings.Join(strings.Fields(refs), " ")
			if len(refs) > 600 {
				refs = ""
			}
			p.Headers = map[string]string{"In-Reply-To": id, "References": strings.TrimSpace(refs + " " + id)}
		}
	}
	h.result(w, p, nil)
}
