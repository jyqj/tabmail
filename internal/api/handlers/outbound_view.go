package handlers

import (
	"context"
	"net/http"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Never derive content authority from an administrative role. A task's durable
// sender identity or a CURRENT read grant for its original mailbox is required.
func (h *OutboundHandler) outboundContentAllowed(ctx context.Context, actor authz.Actor, job *models.OutboundJob) (bool, error) {
	if job == nil || actor.TenantID != job.TenantID || !actor.Permission.AllowsZone(job.ZoneID) {
		return false, nil
	}
	uid := actor.EffectiveUserID()
	if uid == nil {
		return false, nil
	}
	u, err := h.store.GetUser(ctx, *uid)
	if err != nil {
		return false, err
	}
	if u == nil || !u.IsActive || (u.TenantID != job.TenantID && !(actor.IsSuperAdmin && u.Role == models.RoleSuperAdmin)) {
		return false, nil
	}
	if job.SenderUserID != nil && *job.SenderUserID == *uid {
		return true, nil
	}
	if job.SenderMailboxID == nil {
		return false, nil
	}
	mb, err := h.store.ForTenant(job.TenantID).GetMailbox(ctx, *job.SenderMailboxID)
	if err != nil {
		return false, err
	}
	if mb == nil || mb.ZoneID != job.ZoneID {
		return false, nil
	}
	g, err := authz.MailboxRights(ctx, h.store, job.TenantID, uid, mb)
	return g != nil && g.CanRead, err
}

// Copy before redacting: cached/shared store objects and delivery state must not
// be mutated by presentation. RcptTo contains BCC; protocol errors may echo it.
func (h *OutboundHandler) redactOutboundJob(ctx context.Context, actor authz.Actor, job *models.OutboundJob) (*models.OutboundJob, error) {
	allowed, err := h.outboundContentAllowed(ctx, actor, job)
	if err != nil {
		return nil, err
	}
	cp := *job
	cp.DeliveryToken = nil
	cp.RawMIME = nil
	cp.ContentRedacted = !allowed
	if !allowed {
		cp.TextBody = ""
		cp.HTMLBody = ""
		cp.BCC = nil
		cp.HeadersJSON = nil
		cp.RcptTo = append(append([]string{}, job.To...), job.CC...)
		if cp.LastError != "" {
			cp.LastError = "Delivery details restricted; inspect status and SMTP code"
		}
		if cp.SMTPResponse != "" {
			cp.SMTPResponse = "Protocol response restricted"
		}
	}
	return &cp, nil
}
func (h *OutboundHandler) writeOutboundView(w http.ResponseWriter, r *http.Request, job *models.OutboundJob) {
	view, err := h.redactOutboundJob(r.Context(), middleware.ActorFromContext(r.Context()), job)
	if err != nil {
		errInternal(w)
		return
	}
	ok(w, view)
}

// A read grant may expose shared history, but must not authorize resending it.
func (h *OutboundHandler) authorizeOutboundRetry(ctx context.Context, job *models.OutboundJob) error {
	actor := middleware.ActorFromContext(ctx)
	if !actor.IsTenantAdmin() && actor.Permission != nil && !actor.Permission.CanSend {
		return authz.ErrForbidden("sending not allowed")
	}
	if job.SenderMailboxID != nil {
		mb, err := h.store.ForTenant(job.TenantID).GetMailbox(ctx, *job.SenderMailboxID)
		if err != nil {
			return err
		}
		return authz.CheckMailboxSender(ctx, h.store, actor, mb, job.TemplateName != nil)
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
