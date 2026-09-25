package handlers

import (
	"github.com/go-chi/chi/v5"
	"tabmail/internal/api/middleware"
)

// RegisterCompanyRoutes is transport composition, not a business dependency.
// Administrative, employee and event handlers share services, not handlers.
type CompanyRoutes struct {
	Setup     *CompanySetupHandler
	Mailboxes *MailboxAdminHandler
	Templates *CompanyTemplateHandler
	Recovery  *CompanyRecoveryHandler
	Mail      *CompanyMailHandler
	Events    *MailboxEventHandler
	Domains   *CompanyDomainHandler
	Archive   *MailArchiveHandler
	Employees *EmployeeLifecycleHandler
	Drafts    *CompanyDraftHandler
	Index     *CompanyIndexHandler
	Console   *CompanyConsoleHandler
}

func RegisterCompanyRoutes(r chi.Router, c CompanyRoutes) {
	r.Post("/company/activate", c.Setup.Activate)
	r.Route("/company", func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Get("/overview", c.Console.Overview)
		r.With(middleware.RequireAdmin).Post("/index/retry", c.Console.RetryIndex)
		r.Get("/audit", c.Console.Audit)
		r.Get("/mailboxes/{id}/access/{user}", c.Console.Access)
		r.Get("/settings", c.Setup.Settings)
		r.Put("/settings", c.Setup.Configure)
		r.Get("/invitations", c.Setup.Invitations)
		r.Post("/invitations", c.Setup.Invite)
		r.Delete("/invitations/{id}", c.Setup.RevokeInvite)
		r.Post("/employees/{id}/offboard/preview", c.Employees.Preview)
		r.Post("/employees/{id}/offboard", c.Employees.Execute)
		r.Get("/mailboxes", c.Mailboxes.Mailboxes)
		r.Post("/mailboxes", c.Mailboxes.CreateMailbox)
		r.Post("/mailboxes/{id}/handover", c.Mailboxes.Handover)
		r.Post("/mailboxes/{id}/convert-shared", c.Mailboxes.ConvertShared)
		r.Get("/mailboxes/{id}/grants", c.Mailboxes.Grants)
		r.Put("/mailboxes/{id}/grants", c.Mailboxes.Grant)
		// Mailbox send-policy override (tenant administrators only). The
		// handler's repo call re-checks actor.IsTenantAdmin inside the
		// transaction, mirroring the double guard on the domain routes.
		r.With(middleware.RequireAdmin).Put("/mailboxes/{id}/send-policy", c.Mailboxes.MailboxSendPolicy)
		r.Get("/templates", c.Templates.Templates)
		r.Post("/templates", c.Templates.SaveTemplate)
		r.Put("/templates/{id}", c.Templates.SaveTemplate)
		r.Post("/templates/preview", c.Templates.Preview)
		r.Post("/templates/{id}/publish", c.Templates.Publish)
		r.Post("/templates/{id}/retire", c.Templates.Retire)
		r.Get("/templates/{id}/versions", c.Templates.Versions)
		r.Post("/templates/{id}/versions/{version}/revoke", c.Templates.RevokeTemplateVersion)
		r.Get("/templates/{id}/grants", c.Templates.TemplateGrants)
		r.Put("/templates/{id}/grants", c.Templates.TemplateGrant)
		r.Get("/mailboxes/{id}/templates", c.Templates.UsableTemplates)
		r.Get("/mailboxes/{id}/events", c.Events.Events)
		r.Get("/mailboxes/{id}/sent", c.Archive.List)
		r.Post("/mailboxes/{id}/sent/{message}/actions", c.Archive.Change)
		r.Get("/mailboxes/{id}/index-status", c.Index.Status)
		r.Get("/mailboxes/{id}/messages/{message}/conversation", c.Index.Conversation)
		r.Get("/mailboxes/{id}/messages", c.Mail.Messages)
		r.Get("/mailboxes/{id}/messages/{message}", c.Mail.Message)
		r.Post("/mailboxes/{id}/messages/{message}/actions", c.Mail.MessageAction)
		r.Post("/mailboxes/{id}/messages/{message}/compose", c.Mail.ComposeReply)
		r.Get("/mailboxes/{id}/messages/{message}/source", c.Mail.Source)
		r.Get("/mailboxes/{id}/messages/{message}/attachments", c.Mail.InboundAttachments)
		r.Get("/mailboxes/{id}/messages/{message}/attachments/{index}", c.Mail.InboundAttachment)
		r.Get("/mailboxes/{id}/messages/{message}/parts/{attachment}", c.Mail.InboundAttachmentByID)
		r.Post("/mailboxes/{id}/attachments", c.Mail.UploadAttachment)
		r.Get("/attachments/{id}", c.Mail.Attachment)
		r.Get("/drafts", c.Drafts.List)
		r.Get("/drafts/{id}", c.Drafts.Get)
		r.Post("/drafts", c.Drafts.Save)
		r.Put("/drafts/{id}", c.Drafts.Save)
		r.Post("/drafts/{id}/submit", c.Mail.SubmitDraft)
		r.Delete("/drafts/{id}", c.Drafts.Delete)
		r.Get("/submissions", c.Mail.Submissions)
		r.Get("/submissions/{id}", c.Mail.Submission)
		// Content requires CURRENT mailbox read rights, unlike operation receipts.
		// Missing/revoked/out-of-scope content is consistently a 404.
		r.Get("/submissions/{id}/content", c.Mail.SubmissionContent)
		r.Get("/submissions/{id}/attachments", c.Mail.SubmissionAttachments)
		r.Get("/submissions/{id}/attachments/{aid}/download", c.Mail.SubmissionAttachmentDownload)
		r.Get("/recovery", c.Recovery.Recovery)
		r.Post("/recovery/{id}/inspect", c.Recovery.InspectReceipt)
		r.Post("/recovery/{id}/retry", c.Recovery.RetryReceipt)
		r.Get("/outbound/{id}/recipients", c.Recovery.Recipients)
		r.Post("/outbound/{id}/reconcile", c.Recovery.Reconcile)
		r.With(middleware.RequireSuperAdmin).Post("/outbound/{id}/inspect", c.Recovery.InspectOutbound)
		// Company domain onboarding (tenant administrators only). The handler
		// re-checks actor.IsTenantAdmin at the service boundary.
		if c.Domains != nil {
			r.With(middleware.RequireAdmin).Post("/domains", c.Domains.Create)
			r.With(middleware.RequireAdmin).Get("/domains", c.Domains.List)
			r.With(middleware.RequireAdmin).Post("/domains/{id}/verify", c.Domains.Verify)
			r.With(middleware.RequireAdmin).Get("/domains/{id}/verification", c.Domains.Verification)
			r.With(middleware.RequireAdmin).Delete("/domains/{id}", c.Domains.Delete)
		}
	})
}
