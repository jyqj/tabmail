package handlers

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/store"
)

// Ingress recovery is an operation on a possibly multi-tenant SMTP envelope.
// It therefore remains platform-admin-only, just like the existing ingest list.
type IngressHandler struct {
	Ledger store.IngressLedger
	Logger zerolog.Logger
}

func (h IngressHandler) allowed(w http.ResponseWriter, r *http.Request) bool {
	if !middleware.IsSuperAdmin(r.Context()) {
		errForbidden(w, "platform administrator required")
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (h IngressHandler) Targets(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid receipt id")
		return
	}
	targets, err := h.Ledger.ListIngressTargets(r.Context(), id)
	if err != nil {
		respondAppError(w, h.Logger, err)
		return
	}
	if len(targets) == 0 {
		errNotFound(w, "ledger receipt not found")
		return
	}
	ok(w, targets)
}
func (h IngressHandler) Retry(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid receipt id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Reason string `json:"reason"`
	}
	if err = decodeBody(r, &body); err != nil || strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > 2000 {
		errBadRequest(w, "reason is required")
		return
	}
	if err = h.Ledger.RetryIngress(r.Context(), id, actorFromRequest(r), body.Reason); err != nil {
		respondAppError(w, h.Logger, err)
		return
	}
	ok(w, map[string]bool{"requeued": true})
}
