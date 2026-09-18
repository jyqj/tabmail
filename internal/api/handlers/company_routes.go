package handlers

import (
	"github.com/go-chi/chi/v5"
	"tabmail/internal/api/middleware"
)

// RegisterCompanyRoutes is transport composition, not a business dependency.
// Administrative, employee and event handlers share services, not handlers.
func RegisterCompanyRoutes(r chi.Router, h *CompanyHandler, mail *CompanyMailHandler, events *MailboxEventHandler, domains *CompanyDomainHandler) {
	r.Post("/company/activate", h.Activate)
	r.Route("/company", func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Get("/settings", h.Settings)
		r.Put("/settings", h.Configure)
		r.Get("/invitations", h.Invitations)
		r.Post("/invitations", h.Invite)
		r.Delete("/invitations/{id}", h.RevokeInvite)
		r.Post("/employees/{id}/offboard", h.Offboard)
		r.Get("/mailboxes", h.Mailboxes)
		r.Post("/mailboxes", h.CreateMailbox)
		r.Post("/mailboxes/{id}/handover", h.Handover)
		r.Post("/mailboxes/{id}/convert-shared", h.ConvertShared)
		r.Get("/mailboxes/{id}/grants", h.Grants)
		r.Put("/mailboxes/{id}/grants", h.Grant)
		// Mailbox send-policy override (tenant administrators only). The
		// handler's repo call re-checks actor.IsTenantAdmin inside the
		// transaction, mirroring the double guard on the domain routes.
		r.With(middleware.RequireAdmin).Put("/mailboxes/{id}/send-policy", h.MailboxSendPolicy)
		r.Get("/templates", h.Templates)
		r.Post("/templates", h.SaveTemplate)
		r.Put("/templates/{id}", h.SaveTemplate)
		r.Post("/templates/preview", h.Preview)
		r.Post("/templates/{id}/publish", h.Publish)
		r.Post("/templates/{id}/retire", h.Retire)
		r.Get("/templates/{id}/versions", h.Versions)
		r.Post("/templates/{id}/versions/{version}/revoke", h.RevokeTemplateVersion)
		r.Get("/templates/{id}/grants", h.TemplateGrants)
		r.Put("/templates/{id}/grants", h.TemplateGrant)
		r.Get("/mailboxes/{id}/templates", h.UsableTemplates)
		r.Get("/mailboxes/{id}/events", events.Events)
		r.Get("/mailboxes/{id}/messages", mail.Messages)
		r.Get("/mailboxes/{id}/messages/{message}", mail.Message)
		r.Post("/mailboxes/{id}/messages/{message}/actions", mail.MessageAction)
		r.Post("/mailboxes/{id}/messages/{message}/compose", mail.ComposeReply)
		r.Get("/mailboxes/{id}/messages/{message}/source", mail.Source)
		r.Get("/mailboxes/{id}/messages/{message}/attachments", mail.InboundAttachments)
		r.Get("/mailboxes/{id}/messages/{message}/attachments/{index}", mail.InboundAttachment)
		r.Post("/mailboxes/{id}/attachments", mail.UploadAttachment)
		r.Get("/attachments/{id}", mail.Attachment)
		r.Get("/drafts", mail.Drafts)
		r.Post("/drafts", mail.SaveDraft)
		r.Put("/drafts/{id}", mail.SaveDraft)
		r.Post("/drafts/{id}/submit", mail.SubmitDraft)
		r.Delete("/drafts/{id}", mail.DeleteDraft)
		r.Get("/submissions", mail.Submissions)
		r.Get("/submissions/{id}", mail.Submission)
		// Content requires CURRENT mailbox read rights, unlike operation receipts.
		// Missing/revoked/out-of-scope content is consistently a 404.
		r.Get("/submissions/{id}/content", mail.SubmissionContent)
		r.Get("/submissions/{id}/attachments", mail.SubmissionAttachments)
		r.Get("/submissions/{id}/attachments/{aid}/download", mail.SubmissionAttachmentDownload)
		r.Get("/recovery", h.Recovery)
		r.Post("/recovery/{id}/inspect", h.InspectReceipt)
		r.Post("/recovery/{id}/retry", h.RetryReceipt)
		r.Get("/outbound/{id}/recipients", h.Recipients)
		r.Post("/outbound/{id}/reconcile", h.Reconcile)
		r.With(middleware.RequireSuperAdmin).Post("/outbound/{id}/inspect", h.InspectOutbound)
		// Company domain onboarding (tenant administrators only). The handler
		// re-checks actor.IsTenantAdmin at the service boundary.
		if domains != nil {
			r.With(middleware.RequireAdmin).Post("/domains", domains.Create)
			r.With(middleware.RequireAdmin).Get("/domains", domains.List)
			r.With(middleware.RequireAdmin).Post("/domains/{id}/verify", domains.Verify)
			r.With(middleware.RequireAdmin).Get("/domains/{id}/verification", domains.Verification)
			r.With(middleware.RequireAdmin).Delete("/domains/{id}", domains.Delete)
		}
	})
}
