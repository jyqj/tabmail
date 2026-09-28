package handlers

import (
	"errors"
	"net/http"

	appcore "tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/store"
)

// memberAppError maps member-guard domain errors onto the app error kinds the
// member endpoints have always produced; unknown errors stay internal with
// their cause preserved for logging.
func memberAppError(err error) error {
	switch {
	case err == nil:
		return nil
	case authz.IsAuthzError(err):
		return appcore.Forbidden(err.Error())
	case errors.Is(err, store.ErrMemberNotFound):
		return appcore.NotFound(err.Error())
	case errors.Is(err, store.ErrLastAdministrator), errors.Is(err, store.ErrMemberOwnsMailbox):
		return appcore.Conflict(err.Error())
	default:
		return appcore.Internal(err)
	}
}

func (h *UserAdminHandler) writeMemberError(w http.ResponseWriter, err error) {
	respondAppError(w, h.logger, memberAppError(err))
}
