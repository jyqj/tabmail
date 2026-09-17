package handlers

import (
	"net/http"

	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	domainapp "tabmail/internal/app/domains"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// CompanyDomainHandler is the narrow company domain onboarding surface: create
// a company domain, list them with verification summaries, and drive DNS
// verification. It is tenant-administrator only and scoped to the actor's own
// company — it deliberately re-exposes none of the open-platform zone surface
// (visibility, random subdomains, routes, address suggestion).
type CompanyDomainHandler struct {
	service *domainapp.Service
	repo    company.Repository
	logger  zerolog.Logger
}

func NewCompanyDomainHandler(service *domainapp.Service, repo company.Repository, l zerolog.Logger) *CompanyDomainHandler {
	return &CompanyDomainHandler{service: service, repo: repo, logger: l.With().Str("handler", "company_domains").Logger()}
}

// requireCompanyAdmin is the company-side guard: the interactive caller must
// carry tenant-administrator authority in their own tenant. The routes are
// additionally gated by middleware.RequireAdmin; this check is authoritative
// because it inspects the resolved actor, not the transport auth mode.
func (h *CompanyDomainHandler) requireCompanyAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !companyActor(r).IsTenantAdmin() {
		errForbidden(w, "company administrator required")
		return false
	}
	return true
}

func (h *CompanyDomainHandler) result(w http.ResponseWriter, v any, e error) {
	if e != nil {
		if authz.IsAuthzError(e) {
			e = app.Forbidden(e.Error())
		}
		respondAppError(w, h.logger, e)
		return
	}
	ok(w, v)
}

// domainView projects a zone into the company wizard DTO with the DNS records
// the administrator must publish. Signing material and platform fields never
// leave the service layer.
func (h *CompanyDomainHandler) domainView(zone *models.DomainZone) company.Domain {
	txt, mx, dkimHost, dkimRecord := h.service.ZoneDNSRequirements(zone)
	return company.Domain{
		ID:          zone.ID,
		Domain:      zone.Domain,
		IsVerified:  zone.IsVerified,
		MXVerified:  zone.MXVerified,
		DKIMEnabled: zone.DKIMEnabled,
		TXTRecord:   txt,
		ExpectedMX:  mx,
		DKIMHost:    dkimHost,
		DKIMRecord:  dkimRecord,
		CreatedAt:   zone.CreatedAt,
	}
}

func (h *CompanyDomainHandler) verificationView(zone *models.DomainZone, checks domainapp.VerificationChecks) company.DomainVerification {
	view := h.domainView(zone)
	return company.DomainVerification{
		ID:          view.ID,
		Domain:      view.Domain,
		IsVerified:  view.IsVerified,
		MXVerified:  view.MXVerified,
		DKIMEnabled: view.DKIMEnabled,
		TXTRecord:   view.TXTRecord,
		ExpectedMX:  view.ExpectedMX,
		DKIMHost:    view.DKIMHost,
		DKIMRecord:  view.DKIMRecord,
		Checks: company.DomainVerificationChecks{
			TXT:   company.DNSCheck(checks.TXT),
			MX:    company.DNSCheck(checks.MX),
			SPF:   company.DNSCheck(checks.SPF),
			DKIM:  company.DNSCheck(checks.DKIM),
			DMARC: company.DNSCheck(checks.DMARC),
		},
	}
}

// Create handles POST /company/domains.
func (h *CompanyDomainHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireCompanyAdmin(w, r) {
		return
	}
	body, okBody := companyBody[struct {
		Domain string `json:"domain"`
	}](w, r)
	if !okBody {
		return
	}
	zone, e := h.service.CreateZone(r.Context(), companyActor(r), middleware.TenantFromCtx(r.Context()), body.Domain)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	created(w, h.domainView(zone))
}

// List handles GET /company/domains.
func (h *CompanyDomainHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.requireCompanyAdmin(w, r) {
		return
	}
	actor := companyActor(r)
	zones, e := h.service.ListZones(r.Context(), actor, middleware.TenantFromCtx(r.Context()))
	if e != nil {
		h.result(w, nil, e)
		return
	}
	out := make([]company.Domain, 0, len(zones))
	for _, zone := range zones {
		out = append(out, h.domainView(zone))
	}
	ok(w, out)
}

// Verify handles POST /company/domains/{id}/verify — trigger live DNS checks
// and persist the resulting verification state.
func (h *CompanyDomainHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if !h.requireCompanyAdmin(w, r) {
		return
	}
	id, okID := companyID(w, r, "id")
	if !okID {
		return
	}
	zone, checks, e := h.service.TriggerVerify(r.Context(), companyActor(r), id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	ok(w, h.verificationView(zone, checks))
}

// Verification handles GET /company/domains/{id}/verification.
func (h *CompanyDomainHandler) Verification(w http.ResponseWriter, r *http.Request) {
	if !h.requireCompanyAdmin(w, r) {
		return
	}
	id, okID := companyID(w, r, "id")
	if !okID {
		return
	}
	actor := companyActor(r)
	zone, e := h.service.ManagedZone(r.Context(), actor, id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	status, e := h.service.VerificationStatus(r.Context(), actor, id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	ok(w, h.verificationView(zone, status.Checks))
}

// Delete handles DELETE /company/domains/{id}. The service-level DeleteZone
// semantics (tenant-isolated, zone-manage authorization, cache invalidation,
// audit) match the company scenario, so the entry point exists — a misspelled
// domain must be removable before ConfigureCompany. The one company-specific
// guard lives here: the configured primary domain cannot be deleted, because
// company_settings pins it with a foreign key.
func (h *CompanyDomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.requireCompanyAdmin(w, r) {
		return
	}
	id, okID := companyID(w, r, "id")
	if !okID {
		return
	}
	settings, e := h.repo.GetCompanySettings(r.Context(), companyActor(r).TenantID)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if settings != nil && settings.PrimaryZoneID == id {
		errConflict(w, "domain is the company primary domain; configure a different domain first")
		return
	}
	h.result(w, map[string]bool{"deleted": true}, h.service.DeleteZone(r.Context(), companyActor(r), id))
}
