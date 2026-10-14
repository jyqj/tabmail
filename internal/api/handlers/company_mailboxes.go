package handlers

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Administrative ports never expose message contents or storage keys.
type MailboxAdminHandler struct {
	repo   company.MailboxAdminService
	logger zerolog.Logger
}

func NewMailboxAdminHandler(repo company.MailboxAdminService, l zerolog.Logger) *MailboxAdminHandler {
	return &MailboxAdminHandler{repo, l}
}
func (h *MailboxAdminHandler) result(w http.ResponseWriter, v any, e error) {
	companyResponse(w, h.logger, v, e)
}
func (h *MailboxAdminHandler) Mailboxes(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListWorkMailboxes(r.Context(), companyActor(r))
	h.result(w, v, e)
}

func (h *MailboxAdminHandler) CreateMailbox(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.MailboxInput](w, r)
	if !ok {
		return
	}
	out, e := h.repo.CreateWorkMailbox(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}

func (h *MailboxAdminHandler) Handover(w http.ResponseWriter, r *http.Request) {
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

func (h *MailboxAdminHandler) Grants(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.repo.ListWorkGrants(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}

func (h *MailboxAdminHandler) Grant(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		UserID       uuid.UUID `json:"user_id"`
		CanRead      *bool     `json:"can_read"`
		CanOrganize  *bool     `json:"can_organize"`
		CanSend      *bool     `json:"can_send"`
		TemplateOnly *bool     `json:"template_only"`
		Revision     *int64    `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	if v.Revision == nil || *v.Revision < 1 {
		errBadRequest(w, "positive mailbox revision required; reload permissions")
		return
	}
	if v.UserID == uuid.Nil || v.CanRead == nil || v.CanOrganize == nil || v.CanSend == nil || v.TemplateOnly == nil {
		errBadRequest(w, "user_id and all four permission booleans required; reload permissions")
		return
	}
	// This command replaces the entire grant. Never derive a revocation from
	// an omitted or null field in an incomplete client's request.
	actor := companyActor(r)
	grant := models.MailboxGrant{
		MailboxID: id, TenantID: actor.TenantID, UserID: v.UserID,
		CanRead: *v.CanRead, CanOrganize: *v.CanOrganize,
		CanSend: *v.CanSend, TemplateOnly: *v.TemplateOnly,
	}
	h.result(w, map[string]any{"updated": true, "revision": *v.Revision + 1}, h.repo.SetWorkGrant(r.Context(), actor, grant, *v.Revision))
}

func (h *MailboxAdminHandler) MailboxSendPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Policy   json.RawMessage `json:"send_policy"`
		Revision *int64          `json:"revision"`
	}](w, r)
	if !ok {
		return
	}
	if v.Revision == nil || *v.Revision < 1 {
		errBadRequest(w, "positive mailbox revision required; reload permissions")
		return
	}
	if len(v.Policy) == 0 {
		errBadRequest(w, "send_policy is required; use null explicitly to inherit")
		return
	}
	var policy *string
	if err := json.Unmarshal(v.Policy, &policy); err != nil {
		errBadRequest(w, "send_policy must be a string or null")
		return
	}
	h.result(w, map[string]any{"updated": true, "revision": *v.Revision + 1}, h.repo.SetWorkMailboxSendPolicy(r.Context(), companyActor(r), id, policy, *v.Revision))
}

func (h *MailboxAdminHandler) ConvertShared(w http.ResponseWriter, r *http.Request) {
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
