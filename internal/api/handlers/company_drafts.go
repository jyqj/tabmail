package handlers

import (
	"github.com/rs/zerolog"
	"net/http"
	"strconv"
	"tabmail/internal/app/drafts"
	"tabmail/internal/company"
)

type CompanyDraftHandler struct {
	service *drafts.Service
	logger  zerolog.Logger
}

func NewCompanyDraftHandler(repo drafts.Repository, logger zerolog.Logger) *CompanyDraftHandler {
	return &CompanyDraftHandler{drafts.New(repo), logger}
}
func (h *CompanyDraftHandler) List(w http.ResponseWriter, r *http.Request) {
	p := pageFromReq(r)
	v, n, e := h.service.List(r.Context(), companyActor(r), p)
	if e != nil {
		companyResponse(w, h.logger, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	okList(w, v, n, p.Page, p.PerPage)
}
func (h *CompanyDraftHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, e := h.service.Get(r.Context(), companyActor(r), id)
	companyResponse(w, h.logger, v, e)
}
func (h *CompanyDraftHandler) Save(w http.ResponseWriter, r *http.Request) {
	v, valid := companyBody[company.Draft](w, r)
	if !valid {
		return
	}
	if r.Method == http.MethodPut {
		id, valid := companyID(w, r, "id")
		if !valid {
			return
		}
		v.ID = id
	}
	out, e := h.service.Save(r.Context(), companyActor(r), v, r.Method == http.MethodPost)
	companyResponse(w, h.logger, out, e)
}
func (h *CompanyDraftHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	rev, e := strconv.Atoi(r.URL.Query().Get("revision"))
	if e != nil {
		errBadRequest(w, "positive draft revision required")
		return
	}
	companyResponse(w, h.logger, map[string]bool{"deleted": true}, h.service.Delete(r.Context(), companyActor(r), id, rev))
}
