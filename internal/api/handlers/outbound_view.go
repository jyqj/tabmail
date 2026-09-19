package handlers

import (
	"context"
	"net/http"

	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
)

func (h *OutboundHandler) writeOutboundView(w http.ResponseWriter, r *http.Request, job *models.OutboundJob) {
	view, err := h.subs.RedactOutboundJob(r.Context(), middleware.ActorFromContext(r.Context()), job)
	if err != nil {
		errInternal(w)
		return
	}
	ok(w, view)
}

// authorizeOutboundRetry delegates to the submissions service so the manual
// retry endpoint and the capabilities projection share one authority
// predicate. A read grant may expose shared history, but must not authorize
// resending it.
func (h *OutboundHandler) authorizeOutboundRetry(ctx context.Context, job *models.OutboundJob) error {
	return h.subs.RetryAuthority(ctx, middleware.ActorFromContext(ctx), job)
}
