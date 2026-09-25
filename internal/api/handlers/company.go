package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/submissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// companyWorkspace is only the administrative/operations surface. Employee
// content and draft adapters receive separate, narrower ports.
type companyWorkspace interface {
	company.SettingsService
	company.EmployeeService
	company.MailboxAdminService
	company.TemplateAdminService
	company.TemplateSendReader
	company.DeliveryRecovery
	company.RecoveryService
}

type companyAdminStore interface {
	app.AuditStore
	GetUser(context.Context, uuid.UUID) (*models.User, error)
}

type CompanyHandler struct {
	repo    companyWorkspace
	store   companyAdminStore
	objects store.ObjectStore
	subs    *submissions.Service
	logger  zerolog.Logger
}

func NewCompanyHandler(repo companyWorkspace, st companyAdminStore, obj store.ObjectStore, subs *submissions.Service, l zerolog.Logger) *CompanyHandler {
	return &CompanyHandler{repo: repo, store: st, objects: obj, subs: subs, logger: l.With().Str("handler", "company").Logger()}
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
	v, ok := companyBody[struct {
		models.MailboxGrant
		Revision *int64 `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	if v.Revision == nil || *v.Revision < 1 {
		errBadRequest(w, "positive mailbox revision required; reload permissions")
		return
	}
	v.MailboxID = id
	v.TenantID = companyActor(r).TenantID
	h.result(w, map[string]any{"updated": true, "revision": *v.Revision + 1}, h.repo.SetWorkGrant(r.Context(), companyActor(r), v.MailboxGrant, *v.Revision))
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
		Policy   *string `json:"send_policy"`
		Revision *int64  `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	if v.Revision == nil || *v.Revision < 1 {
		errBadRequest(w, "positive mailbox revision required; reload permissions")
		return
	}
	h.result(w, map[string]any{"updated": true, "revision": *v.Revision + 1}, h.repo.SetWorkMailboxSendPolicy(r.Context(), companyActor(r), id, v.Policy, *v.Revision))
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

// RevokeTemplateVersion handles POST
// /company/templates/{id}/versions/{version}/revoke — the emergency one-way
// revoke of one published version. Semantically distinct from template-level
// retire: the revoke stops every not-yet-started delivery attempt of that
// single version while delivered outcomes and sibling versions stay untouched.
// The administrator guard lives inside the store transaction, matching the
// other template governance routes.
func (h *CompanyHandler) RevokeTemplateVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	version, e := strconv.Atoi(chi.URLParam(r, "version"))
	if e != nil || version < 1 {
		errBadRequest(w, "invalid version")
		return
	}
	v, ok := companyBody[struct {
		Revision int `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"revoked": true}, h.repo.RevokeMailTemplateVersion(r.Context(), companyActor(r), id, version, v.Revision))
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
	if h.subs == nil || !h.subs.OutboundEnabled() {
		errNotFound(w, "outbound disabled")
		return
	}
	j, e := h.subs.AccessibleOutboundJob(r.Context(), middleware.TenantFromCtx(r.Context()), companyActor(r), id)
	if e != nil {
		writeOutboundJobAccessError(w, h.logger, e, "listing recipient outcomes")
		return
	}
	view, e := h.subs.RedactOutboundJob(r.Context(), companyActor(r), j)
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
	if h.subs == nil || !h.subs.OutboundEnabled() {
		errNotFound(w, "outbound disabled")
		return
	}
	j, e := h.subs.AccessibleOutboundJob(r.Context(), middleware.TenantFromCtx(r.Context()), companyActor(r), id)
	if e != nil {
		writeOutboundJobAccessError(w, h.logger, e, "inspecting outbound")
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
