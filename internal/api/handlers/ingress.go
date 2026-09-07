package handlers

import (
	"net/http"
	"strings"
	"time"
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
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		Reason            string  `json:"reason"`
		ObservedUpdatedAt *string `json:"observed_updated_at"`
	}
	if err = decodeBody(r, &body); err != nil || strings.TrimSpace(body.Reason) == "" || utf8.RuneCountInString(body.Reason) > 2000 {
		errBadRequest(w, "reason is required")
		return
	}
	if body.ObservedUpdatedAt != nil {
		observed, parseErr := time.Parse(time.RFC3339Nano, *body.ObservedUpdatedAt)
		inspector, available := h.Ledger.(store.IngressInspector)
		if parseErr != nil || observed.IsZero() {
			errBadRequest(w, "invalid observed_updated_at")
			return
		}
		if !available {
			errInternal(w)
			return
		}
		err = inspector.RetryReviewedIngress(r.Context(), id, actorFromRequest(r), body.Reason, observed)
	} else {
		err = h.Ledger.RetryIngress(r.Context(), id, actorFromRequest(r), body.Reason)
	}
	if err != nil {
		respondAppError(w, h.Logger, err)
		return
	}
	ok(w, map[string]bool{"requeued": true})
}

// Inspect returns one consistent operator view, including explicit retry eligibility.
func (h IngressHandler) Inspect(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid receipt id")
		return
	}
	inspector, okInspector := h.Ledger.(store.IngressInspector)
	if !okInspector {
		errInternal(w)
		return
	}
	snapshot, err := inspector.InspectIngress(r.Context(), id)
	if err != nil {
		respondAppError(w, h.Logger, err)
		return
	}
	ok(w, snapshot)
}
