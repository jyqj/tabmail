package handlers

import (
	"errors"
	"net/http"

	"tabmail/internal/authz"
	"tabmail/internal/store"
)

func (h *UserAdminHandler) writeMemberError(w http.ResponseWriter, err error) {
	switch {
	case authz.IsAuthzError(err):
		errForbidden(w, err.Error())
	case errors.Is(err, store.ErrMemberNotFound):
		errNotFound(w, err.Error())
	case errors.Is(err, store.ErrLastAdministrator), errors.Is(err, store.ErrMemberOwnsMailbox):
		errConflict(w, err.Error())
	default:
		h.logger.Error().Err(err).Msg("guarded member mutation")
		errInternal(w)
	}
}
