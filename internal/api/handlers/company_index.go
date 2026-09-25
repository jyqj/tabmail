package handlers

import (
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/company"
)

type CompanyIndexHandler struct {
	repo   company.MailboxIndexReader
	logger zerolog.Logger
}

func NewCompanyIndexHandler(repo company.MailboxIndexReader, l zerolog.Logger) *CompanyIndexHandler {
	return &CompanyIndexHandler{repo, l}
}
func (h *CompanyIndexHandler) Status(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	v, e := h.repo.ContentIndexStatus(r.Context(), companyActor(r), id)
	companyResponse(w, h.logger, v, e)
}
func (h *CompanyIndexHandler) Conversation(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	p := pageFromReq(r)
	v, total, e := h.repo.ListMessageConversation(r.Context(), companyActor(r), mailbox, message, p)
	if e != nil {
		companyResponse(w, h.logger, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	okList(w, v, total, p.Page, p.PerPage)
}
