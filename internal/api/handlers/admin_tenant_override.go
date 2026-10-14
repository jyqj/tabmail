package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GetTenantOverride is registered under the platform-super-admin boundary,
// matching the replacement PATCH for the explicitly addressed tenant.
func (h *AdminHandler) GetTenantOverride(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid tenant id")
		return
	}
	value, err := h.service.GetTenantOverride(r.Context(), id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, value)
}
