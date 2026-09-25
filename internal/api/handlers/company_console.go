package handlers

import (
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/company"
)

type CompanyConsoleHandler struct {
	repo   company.ConsoleReader
	logger zerolog.Logger
}

func NewCompanyConsoleHandler(repo company.ConsoleReader, l zerolog.Logger) *CompanyConsoleHandler {
	return &CompanyConsoleHandler{repo, l}
}
func (h *CompanyConsoleHandler) Overview(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.CompanyOverview(r.Context(), companyActor(r))
	companyResponse(w, h.logger, v, e)
}
func (h *CompanyConsoleHandler) Audit(w http.ResponseWriter, r *http.Request) {
	p := pageFromReq(r)
	v, n, e := h.repo.ListCompanyAudit(r.Context(), companyActor(r), p)
	if e != nil {
		companyResponse(w, h.logger, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	okList(w, v, n, p.Page, p.PerPage)
}

func (h *CompanyConsoleHandler) Access(w http.ResponseWriter, r *http.Request) {
	mailbox, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	user, valid := companyID(w, r, "user")
	if !valid {
		return
	}
	v, e := h.repo.ExplainMailboxAccess(r.Context(), companyActor(r), mailbox, user)
	companyResponse(w, h.logger, v, e)
}
