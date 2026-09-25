package handlers

import (
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/app/mailarchive"
	"tabmail/internal/company"
)

type MailArchiveHandler struct {
	service *mailarchive.Service
	logger  zerolog.Logger
}

func NewMailArchiveHandler(repo company.SentArchive, l zerolog.Logger) *MailArchiveHandler {
	return &MailArchiveHandler{service: mailarchive.New(repo), logger: l}
}
func (h *MailArchiveHandler) List(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	page := pageFromReq(r)
	rows, total, e := h.service.List(r.Context(), companyActor(r), id, r.URL.Query().Get("folder"), r.URL.Query().Get("q"), page)
	if e != nil {
		companyResponse(w, h.logger, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	okList(w, rows, total, page.Page, page.PerPage)
}
func (h *MailArchiveHandler) Change(w http.ResponseWriter, r *http.Request) {
	mailbox, id, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	in, valid := companyBody[struct {
		Action   string `json:"action"`
		Revision int64  `json:"revision"`
	}](w, r)
	if !valid {
		return
	}
	e := h.service.Change(r.Context(), companyActor(r), mailbox, id, in.Revision, in.Action)
	companyResponse(w, h.logger, map[string]any{"updated": true, "revision": in.Revision + 1}, e)
}
