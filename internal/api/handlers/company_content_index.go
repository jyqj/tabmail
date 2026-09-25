package handlers

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (h *CompanyMailHandler) InboundAttachmentByID(w http.ResponseWriter, r *http.Request) {
	mailbox, message, valid := companyMessageIDs(w, r)
	if !valid {
		return
	}
	id := chi.URLParam(r, "attachment")
	if len(id) != 64 {
		errNotFound(w, "attachment not found")
		return
	}
	v, e := h.mail.InboundAttachmentByID(r.Context(), companyActor(r), mailbox, message, id)
	h.file(w, v, e)
}
