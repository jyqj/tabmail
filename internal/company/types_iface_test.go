package company

// roleComposition restates Repository purely as the embedding of the role
// interfaces. The paired assertions below prove the roles are an exact
// partition of the original 42-method surface: no method lost, none added.
type roleComposition interface {
	SettingsService
	EmployeeService
	MailboxAdminService
	TemplateAdminService
	TemplateSendReader
	DraftService
	MailReadService
	SubmissionReader
	RecoveryService
}

// roleComposition satisfies Repository (the roles cover the whole surface).
var _ Repository = roleComposition(nil)

// Repository satisfies roleComposition (the roles add nothing beyond it).
var _ roleComposition = Repository(nil)

// PgStore-style consumers may still depend on the combined interface; the
// production assertion lives next to PgStore (store/postgres/company_ops.go).
