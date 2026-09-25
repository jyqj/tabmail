package handlers

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/http"
	"strconv"
	"tabmail/internal/app/templates"
	"tabmail/internal/company"
)

type CompanyTemplateHandler struct {
	service *templates.Service
	logger  zerolog.Logger
}

func NewCompanyTemplateHandler(repo templates.Repository, identities templates.IdentityReader, l zerolog.Logger) *CompanyTemplateHandler {
	return &CompanyTemplateHandler{templates.New(repo, identities), l}
}
func (h *CompanyTemplateHandler) result(w http.ResponseWriter, v any, e error) {
	companyResponse(w, h.logger, v, e)
}
func (h *CompanyTemplateHandler) Templates(w http.ResponseWriter, r *http.Request) {
	v, e := h.service.ListMailTemplates(r.Context(), companyActor(r))
	h.result(w, v, e)
}

func (h *CompanyTemplateHandler) SaveTemplate(w http.ResponseWriter, r *http.Request) {
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
	out, e := h.service.SaveMailTemplate(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}

func (h *CompanyTemplateHandler) Publish(w http.ResponseWriter, r *http.Request) {
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
	out, e := h.service.PublishMailTemplate(r.Context(), companyActor(r), id, v.Revision)
	h.result(w, out, e)
}

func (h *CompanyTemplateHandler) Retire(w http.ResponseWriter, r *http.Request) {
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
	h.result(w, map[string]bool{"updated": true}, h.service.SetMailTemplateRetired(r.Context(), companyActor(r), id, v.Revision, v.Retired))
}

func (h *CompanyTemplateHandler) Versions(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.service.ListTemplateVersions(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}

func (h *CompanyTemplateHandler) RevokeTemplateVersion(w http.ResponseWriter, r *http.Request) {
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
	h.result(w, map[string]bool{"revoked": true}, h.service.RevokeMailTemplateVersion(r.Context(), companyActor(r), id, version, v.Revision))
}

func (h *CompanyTemplateHandler) TemplateGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.service.ListTemplateGrants(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}

func (h *CompanyTemplateHandler) TemplateGrant(w http.ResponseWriter, r *http.Request) {
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
	h.result(w, map[string]bool{"updated": true}, h.service.SetTemplateGrant(r.Context(), companyActor(r), company.TemplateGrant{TemplateID: id, MailboxID: v.Mailbox, UserID: v.User}, v.Enabled))
}

func (h *CompanyTemplateHandler) UsableTemplates(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, e := h.service.ListUsableTemplates(r.Context(), companyActor(r), id)
	h.result(w, v, e)
}

func (h *CompanyTemplateHandler) Preview(w http.ResponseWriter, r *http.Request) {
	in, valid := companyBody[templates.PreviewInput](w, r)
	if !valid {
		return
	}
	out, e := h.service.Preview(r.Context(), companyActor(r), in)
	h.result(w, out, e)
}
