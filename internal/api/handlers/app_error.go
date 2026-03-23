package handlers

import (
	"net/http"

	"github.com/rs/zerolog"
	appcore "tabmail/internal/app"
)

func respondAppError(w http.ResponseWriter, logger zerolog.Logger, err error) {
	if err == nil {
		return
	}
	appErr, ok := appcore.As(err)
	if !ok {
		logger.Err(err).Msg("unhandled application error")
		errInternal(w)
		return
	}
	switch appErr.Kind {
	case appcore.KindBadRequest:
		errBadRequest(w, appErr.Message)
	case appcore.KindForbidden:
		errForbidden(w, appErr.Message)
	case appcore.KindNotFound:
		errNotFound(w, appErr.Message)
	case appcore.KindConflict:
		errConflict(w, appErr.Message)
	default:
		if appErr.Err != nil {
			logger.Err(appErr.Err).Msg("application internal error")
		} else {
			logger.Error().Msg(appErr.Message)
		}
		errInternal(w)
	}
}
