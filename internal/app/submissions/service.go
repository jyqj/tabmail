// Package submissions hosts the send-submission use cases shared by the HTTP
// handlers: the draft/free-send authorization and enqueue orchestration, the
// consumed-draft idempotency lookup, and outbound-job accessibility with its
// redacted view. Handlers only parse requests, call into this service, and map
// the outcome to HTTP responses; the business rules live here so worker and
// HTTP paths keep obeying the same chain.
package submissions

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store"
)

var (
	// ErrOutboundJobAuthRequired is returned when no tenant context exists.
	ErrOutboundJobAuthRequired = errors.New("authentication required")
	// ErrOutboundJobNotFound is returned when the job is invisible to the actor.
	ErrOutboundJobNotFound = errors.New("outbound job not found")
)

// FailureKind classifies a submission failure for the HTTP layer. The handler
// maps each kind to the exact status code and response body the endpoints have
// always produced; the service never touches http.ResponseWriter.
type FailureKind int

const (
	// FailurePlain is the fallthrough branch: a 400 with the raw message.
	FailurePlain FailureKind = iota
	// FailureBadRequest maps to 400 BAD_REQUEST.
	FailureBadRequest
	// FailureForbidden maps to 403 FORBIDDEN.
	FailureForbidden
	// FailureAuthRequired maps to 403 "authentication required" (no tenant).
	FailureAuthRequired
	// FailureInternal maps to 500 INTERNAL; the cause is logged service-side.
	FailureInternal
	// FailureQuota maps to 429 QUOTA_EXCEEDED with the quota message.
	FailureQuota
	// FailureConflict maps to 409 CONFLICT.
	FailureConflict
	// FailureRevisionConflict maps to the 409 body that tells the caller which
	// draft revision to refresh to (Data.revision + CONFLICT error).
	FailureRevisionConflict
	// FailureApp passes the wrapped app error to respondAppError unchanged.
	FailureApp
)

// Failure is a classified submission failure. Err carries the underlying error
// for FailureApp (replayed verbatim by respondAppError); Revision carries the
// current draft revision for FailureRevisionConflict.
type Failure struct {
	Kind     FailureKind
	Message  string
	Err      error
	Revision int
}

func plainFailure(msg string) *Failure { return &Failure{Kind: FailurePlain, Message: msg} }
func badRequest(msg string) *Failure   { return &Failure{Kind: FailureBadRequest, Message: msg} }
func forbidden(msg string) *Failure    { return &Failure{Kind: FailureForbidden, Message: msg} }
func internalFailure() *Failure        { return &Failure{Kind: FailureInternal} }
func quotaFailure(msg string) *Failure { return &Failure{Kind: FailureQuota, Message: msg} }
func conflict(msg string) *Failure     { return &Failure{Kind: FailureConflict, Message: msg} }
func authRequired() *Failure           { return &Failure{Kind: FailureAuthRequired} }
func appFailure(err error) *Failure    { return &Failure{Kind: FailureApp, Err: err} }

// SubmitInput carries the caller-supplied message fields for a submission.
type SubmitInput struct {
	From              string
	To                []string
	CC                []string
	BCC               []string
	Subject           string
	TextBody          string
	HTMLBody          string
	Headers           map[string]string
	TemplateVersionID *uuid.UUID
	TemplateVars      map[string]string
	AttachmentIDs     []uuid.UUID
	IdempotencyKey    string
	// Draft pins the mail draft this submission consumes atomically.
	Draft *store.DraftConsumption
}

// Service is the submission use-case service. It is stateless and safe for
// concurrent use.
//
// The store is required — NewService panics without it, mirroring
// outbound.NewService's "fail construction instead of degrading" rule. The
// company repository and the outbound service may be nil as an explicit wiring
// decision ("outbound disabled" mode, the same affordance RouterConfig
// documents): every method that needs them guards the nil and behaves exactly
// like the disabled mode the handlers previously implemented.
type Service struct {
	repo     company.DraftService
	store    store.Store
	outbound *outbound.Service
	az       *authz.Authorizer
	logger   zerolog.Logger
}

// NewService creates a new submissions service. It panics when the store is
// missing; nil repo/outbound selects the documented disabled mode instead.
func NewService(repo company.DraftService, st store.Store, out *outbound.Service, logger zerolog.Logger) *Service {
	if st == nil {
		panic("submissions: store dependency is required")
	}
	return &Service{
		repo:     repo,
		store:    st,
		outbound: out,
		az:       authz.New(st),
		logger:   logger.With().Str("component", "submissions").Logger(),
	}
}

// OutboundEnabled reports whether the outbound submission engine is wired in.
func (s *Service) OutboundEnabled() bool { return s.outbound != nil }

// ConsumedDraftSubmission classifies an already-missing draft (see
// outbound.Service.ConsumedDraftSubmission) for the company draft submit
// endpoint.
func (s *Service) ConsumedDraftSubmission(ctx context.Context, tenantID, draftID uuid.UUID, userID *uuid.UUID, key string) (*models.OutboundJob, bool, error) {
	if s.outbound == nil {
		return nil, false, nil
	}
	return s.outbound.ConsumedDraftSubmission(ctx, tenantID, draftID, userID, key)
}

// SubmitDraft runs the whole POST /company/drafts/{id}/submit orchestration:
// draft load, consumed-draft idempotency on a missing draft, revision
// guard, mailbox availability, and the authorized submit chain. tenant and
// actor come from the HTTP context (resolved by the handler via the auth
// middleware); the service itself stays middleware-free.
func (s *Service) SubmitDraft(ctx context.Context, tenant *models.Tenant, actor authz.Actor, draftID uuid.UUID, expectedRevision int, key string) (*models.OutboundJob, bool, *Failure) {
	if s.repo == nil || s.outbound == nil {
		return nil, false, plainFailure("outbound sending is disabled")
	}
	draft, e := s.repo.GetMailDraft(ctx, actor, draftID)
	if e != nil {
		if authz.IsAuthzError(e) {
			e = app.Forbidden(e.Error())
		}
		// A missing draft is only a plain 404 when no submission consumed it;
		// otherwise it is a replay (same key) or an already-consumed conflict.
		if appErr, isApp := app.As(e); isApp && appErr.Kind == app.KindNotFound {
			consumed, replay, e2 := s.ConsumedDraftSubmission(ctx, actor.TenantID, draftID, &actor.ID, key)
			if e2 != nil {
				s.logger.Err(e2).Msg("looking up consumed draft submission")
				return nil, false, internalFailure()
			}
			if consumed != nil {
				if replay {
					return consumed, true, nil
				}
				return nil, false, conflict("draft was already submitted; check its send status instead of retrying")
			}
		}
		return nil, false, appFailure(e)
	}
	if draft.Revision != expectedRevision {
		// The draft still exists with a newer revision: tell the caller what to
		// refresh to, instead of a bare conflict.
		return nil, false, &Failure{Kind: FailureRevisionConflict, Revision: draft.Revision}
	}
	mb, e := s.store.GetMailbox(ctx, draft.MailboxID)
	if e != nil {
		s.logger.Err(e).Msg("loading draft mailbox")
		return nil, false, internalFailure()
	}
	if mb == nil || mb.TenantID != actor.TenantID {
		return nil, false, conflict("draft mailbox is no longer available")
	}
	return s.SubmitAuthorized(ctx, tenant, actor, SubmitInput{
		From:              mb.FullAddress,
		To:                draft.Payload.To,
		CC:                draft.Payload.CC,
		BCC:               draft.Payload.BCC,
		Subject:           draft.Payload.Subject,
		TextBody:          draft.Payload.TextBody,
		HTMLBody:          draft.Payload.HTMLBody,
		Headers:           draft.Payload.Headers,
		TemplateVersionID: draft.Payload.TemplateVersionID,
		TemplateVars:      draft.Payload.TemplateVars,
		AttachmentIDs:     draft.Payload.AttachmentIDs,
		IdempotencyKey:    key,
		Draft: &store.DraftConsumption{
			TenantID: actor.TenantID,
			UserID:   actor.ID,
			ID:       draft.ID,
			Revision: draft.Revision,
		},
	})
}

// SubmitAuthorized runs the full send authorization chain (zone lookup,
// ActionSendFrom, verified/MX, DKIM policy, ResolveSendAuthorization, quota,
// suppression) and enqueues the job. On failure it returns a classified
// Failure; on success the job and whether the response is an idempotent replay
// of an earlier submission.
func (s *Service) SubmitAuthorized(ctx context.Context, tenant *models.Tenant, actor authz.Actor, in SubmitInput) (*models.OutboundJob, bool, *Failure) {
	if tenant == nil {
		return nil, false, authRequired()
	}

	// Resolve caller identity for job attribution and quota tracking.
	// actor.Permission is populated by middleware.PermissionLoader for JWT
	// users and by the auth middleware for API keys (owner permission or
	// zone-restricted synthetic permission).
	var apiKeyID *uuid.UUID
	if actor.Type == authz.PrincipalAPIKey {
		keyID := actor.ID
		apiKeyID = &keyID
	}
	userID := actor.EffectiveUserID()

	canonical, addressErr := authz.CanonicalSender(in.From)
	if addressErr != nil {
		return nil, false, badRequest(addressErr.Error())
	}
	in.From = canonical
	// Validate the from address domain belongs to this tenant and is verified.
	fromDomain := extractDomainFromAddress(in.From)
	if fromDomain == "" {
		return nil, false, badRequest("invalid from address")
	}

	zone, err := s.store.GetZoneByDomain(ctx, fromDomain)
	if err != nil {
		s.logger.Err(err).Str("domain", fromDomain).Msg("looking up zone by domain")
		return nil, false, internalFailure()
	}
	if zone == nil {
		return nil, false, badRequest("from domain is not registered")
	}

	// Authorize sending from this zone through the authz seam: tenant
	// isolation, CanSend flag, and zone allowlist. OwnerUserID is
	// intentionally NOT set — sending must not require zone ownership,
	// so tenant users can send from shared zones.
	if err := s.az.Authorize(ctx, actor, authz.ActionSendFrom, authz.Resource{
		Type:     "zone",
		ID:       zone.ID,
		TenantID: zone.TenantID,
		ZoneID:   zone.ID,
	}); err != nil {
		if authz.IsAuthzError(err) {
			return nil, false, forbidden(err.Error())
		}
		return nil, false, internalFailure()
	}

	if !zone.IsVerified {
		return nil, false, badRequest("from domain is not verified")
	}

	if !zone.MXVerified {
		return nil, false, badRequest("from domain MX is not verified")
	}

	// Reject synchronously when the zone's DKIM policy cannot be satisfied,
	// rather than accepting a job that would only ever be driven to dead.
	if reason := s.outbound.DKIMSendBlockReason(zone); reason != "" {
		return nil, false, badRequest(reason)
	}

	quota := store.OutboundQuotaReservation{}
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	// The From address must be a real mailbox in this tenant or a verified
	// send identity — the two authorized send-as paths. Inbound domain routes
	// govern ingress delivery, not outbound From authorization, so they no
	// longer gate sending. This makes the send_identities feature (manual
	// exact identities, the auto-created *@domain wildcard, and its Verified
	// flag) actually enforce send-as.
	//
	// The decision tree lives in outbound.ResolveSendAuthorization, the same
	// function ValidateJobAuthorization re-runs before every delivery attempt;
	// only the error precedence and wording below are HTTP-side.
	res, err := outbound.ResolveSendAuthorization(ctx, s.store, actor, tenant.ID, in.From, in.TemplateVersionID != nil)
	if err != nil {
		s.logger.Err(err).Str("from", in.From).Msg("resolving send authorization")
		return nil, false, internalFailure()
	}
	switch {
	case res.TenantWideBlocked:
		return nil, false, forbidden("company mailbox sends require an employee-owned credential")
	case res.MailboxSenderErr != nil:
		if authz.IsAuthzError(res.MailboxSenderErr) {
			return nil, false, forbidden(res.MailboxSenderErr.Error())
		}
		return nil, false, internalFailure()
	case res.IdentityUnverified:
		return nil, false, badRequest("from address is not an authorized mailbox or verified send identity")
	case res.MailboxExpired:
		return nil, false, forbidden("sender mailbox expired")
	}
	mailbox := res.Mailbox

	// Reserve user daily quota atomically with job creation.
	if actor.Permission != nil && actor.Permission.DailySendQuota > 0 {
		quota.UserDaily = &store.OutboundUserDailyQuota{
			UserID: userID,
			Since:  todayStart,
			Limit:  actor.Permission.DailySendQuota,
		}
	}

	// Check suppression list — block sending to suppressed addresses.
	for _, rcpt := range append(append(in.To, in.CC...), in.BCC...) {
		suppressed, err := s.store.IsSuppressed(ctx, tenant.ID, rcpt)
		if err != nil {
			s.logger.Err(err).Str("address", rcpt).Msg("checking suppression list")
			return nil, false, internalFailure()
		}
		if suppressed {
			return nil, false, badRequest("recipient " + rcpt + " is suppressed (hard bounce); remove from suppression list to retry")
		}
	}

	// Build and submit the outbound job.
	job, replayed, err := s.outbound.SubmitWithReplay(ctx, outbound.SendRequest{
		TenantID:          tenant.ID,
		SenderMailboxID:   mailboxID(mailbox),
		UserID:            userID,
		APIKeyID:          apiKeyID,
		ZoneID:            zone.ID,
		From:              in.From,
		To:                in.To,
		CC:                in.CC,
		BCC:               in.BCC,
		Subject:           in.Subject,
		TextBody:          in.TextBody,
		HTMLBody:          in.HTMLBody,
		Headers:           in.Headers,
		TemplateVersionID: in.TemplateVersionID, AttachmentIDs: in.AttachmentIDs, IdempotencyKey: in.IdempotencyKey,
		TemplateVars: in.TemplateVars,
		Quota:        quota,
		Draft:        in.Draft,
	})
	if err != nil {
		if errors.Is(err, store.ErrSendAsDailyQuotaExceeded) {
			return nil, false, quotaFailure("send-as daily quota exceeded")
		}
		if errors.Is(err, store.ErrOutboundDailyQuotaExceeded) {
			return nil, false, quotaFailure("daily send quota exceeded")
		}
		if errors.Is(err, store.ErrDraftAlreadyConsumed) {
			return nil, false, conflict("draft was already submitted or consumed in another window; check send status instead of retrying")
		}
		if authz.IsAuthzError(err) {
			return nil, false, forbidden(err.Error())
		}
		if _, ok := app.As(err); ok {
			return nil, false, appFailure(err)
		}
		s.logger.Err(err).Msg("submitting outbound job")
		return nil, false, plainFailure(err.Error())
	}

	return job, replayed, nil
}

// AccessibleOutboundJob resolves a single outbound job for the actor: tenant
// context must exist, the actor's zone allowlist must cover the job, and
// either the owner rule or a current content authority (sender identity or
// mailbox read grant) must hold. Visibility failures collapse to
// ErrOutboundJobNotFound so existence is not disclosed.
func (s *Service) AccessibleOutboundJob(ctx context.Context, tenant *models.Tenant, actor authz.Actor, jobID uuid.UUID) (*models.OutboundJob, error) {
	if tenant == nil {
		return nil, ErrOutboundJobAuthRequired
	}

	job, err := s.store.GetOutboundJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job != nil && !actor.Permission.AllowsZone(job.ZoneID) {
		return nil, ErrOutboundJobNotFound
	}
	if !canAccessOutboundJob(actor, tenant.ID, job) {
		allowed, err := s.ContentAllowed(ctx, actor, job)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrOutboundJobNotFound
		}
	}
	return job, nil
}

// ContentAllowed decides whether the actor may see a job's content and
// protocol details. Never derive content authority from an administrative
// role: a task's durable sender identity or a CURRENT read grant for its
// original mailbox is required.
func (s *Service) ContentAllowed(ctx context.Context, actor authz.Actor, job *models.OutboundJob) (bool, error) {
	if job == nil || actor.TenantID != job.TenantID || !actor.Permission.AllowsZone(job.ZoneID) {
		return false, nil
	}
	uid := actor.EffectiveUserID()
	if uid == nil {
		return false, nil
	}
	u, err := s.store.GetUser(ctx, *uid)
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
	mb, err := s.store.ForTenant(job.TenantID).GetMailbox(ctx, *job.SenderMailboxID)
	if err != nil {
		return false, err
	}
	if mb == nil || mb.ZoneID != job.ZoneID {
		return false, nil
	}
	g, err := authz.MailboxRights(ctx, s.store, job.TenantID, uid, mb)
	return g != nil && g.CanRead, err
}

// RedactOutboundJob builds the actor-safe view of a job. Copy before
// redacting: cached/shared store objects and delivery state must not be
// mutated by presentation. RcptTo contains BCC; protocol errors may echo it.
func (s *Service) RedactOutboundJob(ctx context.Context, actor authz.Actor, job *models.OutboundJob) (*models.OutboundJob, error) {
	allowed, err := s.ContentAllowed(ctx, actor, job)
	if err != nil {
		return nil, err
	}
	cp := *job
	cp.DeliveryToken = nil
	cp.RawMIME = nil
	cp.ContentRedacted = !allowed
	cp.DeliveryUncertain = job.InFlightDomain != "" && job.State != models.OutboundProcessing
	if !allowed {
		cp.TextBody = ""
		cp.HTMLBody = ""
		cp.BCC = nil
		cp.HeadersJSON = nil
		cp.AttachmentIDs = nil
		cp.InFlightDomain = ""
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

func canAccessOutboundJob(actor authz.Actor, tenantID uuid.UUID, job *models.OutboundJob) bool {
	if job == nil || job.TenantID != tenantID {
		return false
	}
	// Same owner rule as the scoped list query, via the single authz seam.
	return authz.CanAccessOwned(actor, job.UserID, job.APIKeyID)
}

// extractDomainFromAddress extracts the domain part from an email address.
func extractDomainFromAddress(addr string) string {
	idx := strings.LastIndex(addr, "@")
	if idx < 0 || idx == len(addr)-1 {
		return ""
	}
	return addr[idx+1:]
}

func mailboxID(m *models.Mailbox) *uuid.UUID {
	if m == nil {
		return nil
	}
	id := m.ID
	return &id
}
