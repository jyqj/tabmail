package company

// roleComposition restates Repository purely as the embedding of the role
// interfaces. The paired assertions below prove the roles are an exact
// composition of the current surface: the aggregate adds no hidden methods.
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
	SentArchive
	ParsedContentReader
	OffboardingPlanner
	DraftQuery
	MailboxIndexReader
	ConsoleReader
}

// roleComposition satisfies Repository (the roles cover the whole surface).
var _ Repository = roleComposition(nil)

// Repository satisfies roleComposition (the roles add nothing beyond it).
var _ roleComposition = Repository(nil)

// PgStore-style consumers may still depend on the combined interface; the
// production assertion lives next to PgStore (store/postgres/company_ops.go).
