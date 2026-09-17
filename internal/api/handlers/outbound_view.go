package handlers

import (
	"context"
	"net/http"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
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

// A read grant may expose shared history, but must not authorize resending it.
func (h *OutboundHandler) authorizeOutboundRetry(ctx context.Context, job *models.OutboundJob) error {
	actor := middleware.ActorFromContext(ctx)
	if job.SenderMailboxID != nil {
		mb, err := h.store.ForTenant(job.TenantID).GetMailbox(ctx, *job.SenderMailboxID)
		if err != nil {
			return err
		}
		return authz.CheckMailboxSender(ctx, h.store, actor, mb, job.TemplateVersionID != nil)
	}
	uid := actor.EffectiveUserID()
	if uid != nil && job.SenderUserID != nil && *uid == *job.SenderUserID {
		return nil
	}
	if actor.Type == authz.PrincipalAPIKey && job.SenderKeyID != nil && actor.ID == *job.SenderKeyID {
		return nil
	}
	return authz.ErrForbidden("send identity authority required to retry")
}
