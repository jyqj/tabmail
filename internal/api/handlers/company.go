package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/enterprise"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type CompanyHandler struct {
	repo           enterprise.Repository
	store          store.Store
	logger         zerolog.Logger
	fullNaming     bool
	legacyWebhooks bool
}

func NewCompanyHandler(st store.Store, fullNaming bool, legacyWebhooks bool, logger zerolog.Logger) *CompanyHandler {
	repo, ok := st.(enterprise.Repository)
	if !ok {
		return nil
	}
	return &CompanyHandler{repo: repo, store: st, logger: logger, fullNaming: fullNaming, legacyWebhooks: legacyWebhooks}
}
func (h *CompanyHandler) member(r *http.Request, admin bool) (*enterprise.Company, *enterprise.Member, error) {
	user := middleware.UserFromCtx(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	if user == nil || tenant == nil || user.TenantID != tenant.ID {
		return nil, nil, app.Forbidden("employee session in this company required")
	}
	c, err := h.repo.GetCompany(r.Context(), tenant.ID)
	if err != nil {
		return nil, nil, err
	}
	if c == nil {
		return nil, nil, app.NotFound("company not configured")
	}
	m, err := h.repo.GetCompanyMember(r.Context(), tenant.ID, user.ID)
	if err != nil {
		return nil, nil, err
	}
	if m == nil || !m.Active || (admin && m.Role != "admin") {
		return nil, nil, app.Forbidden("company permission required")
	}
	return c, m, nil
}
func (h *CompanyHandler) run(w http.ResponseWriter, r *http.Request, admin bool, fn func(*enterprise.Company, *enterprise.Member) (any, error)) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	c, m, err := h.member(r, admin)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	value, err := fn(c, m)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, value)
}
func (h *CompanyHandler) State(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	user := middleware.UserFromCtx(r.Context())
	if tenant == nil || user == nil {
		errForbidden(w, "employee session required")
		return
	}
	c, err := h.repo.GetCompany(r.Context(), tenant.ID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	var m *enterprise.Member
	if c != nil {
		m, err = h.repo.GetCompanyMember(r.Context(), tenant.ID, user.ID)
		if err != nil {
			respondAppError(w, h.logger, err)
			return
		}
	}
	ok(w, map[string]any{"company": c, "member": m})
}
func (h *CompanyHandler) Enable(w http.ResponseWriter, r *http.Request) {
	if h.legacyWebhooks {
		errBadRequest(w, "disable TABMAIL_WEBHOOK_URLS before enabling company mode")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !h.fullNaming {
		errBadRequest(w, "company mode requires full mailbox naming and plus-tag stripping disabled")
		return
	}
	var body struct {
		ZoneID  uuid.UUID `json:"primary_zone_id"`
		Name    string    `json:"name"`
		Confirm bool      `json:"confirm_lockdown"`
	}
	if err := decodeBody(r, &body); err != nil || !body.Confirm {
		errBadRequest(w, "confirm_lockdown must acknowledge revocation of old credentials and pending jobs")
		return
	}
	user := middleware.UserFromCtx(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	if user == nil || tenant == nil {
		errForbidden(w, "administrator session required")
		return
	}
	c, err := h.repo.EnableCompany(r.Context(), tenant.ID, user.ID, body.ZoneID, body.Name)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, c)
}
func (h *CompanyHandler) Members(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		return h.repo.ListCompanyMembers(r.Context(), c.TenantID)
	})
}
func (h *CompanyHandler) Provision(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		var body enterprise.Provision
		if err := decodeBody(r, &body); err != nil {
			return nil, app.BadRequest("invalid employee request")
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token := hex.EncodeToString(b)
		employee, err := h.repo.ProvisionCompanyMember(r.Context(), c.TenantID, m.UserID, body, enterprise.HashToken(token))
		if err != nil {
			return nil, err
		}
		return enterprise.Invitation{Member: *employee, Token: token, ExpiresAt: time.Now().UTC().Add(72 * time.Hour)}, nil
	})
}
func (h *CompanyHandler) Activate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil || len(body.Token) != 64 || len(body.Password) < 12 || len(body.Password) > 72 {
		errBadRequest(w, "activation token and a 12–72 byte password are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		errInternal(w)
		return
	}
	if err = h.repo.ActivateCompanyMember(r.Context(), enterprise.HashToken(body.Token), string(hash)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, map[string]string{"status": "activated; sign in with your company email"})
}
func (h *CompanyHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			return nil, app.BadRequest("invalid employee id")
		}
		var b struct {
			Role   string `json:"company_role"`
			Active *bool  `json:"is_active"`
			Quota  int    `json:"daily_send_quota"`
		}
		if err = decodeBody(r, &b); err != nil || b.Active == nil {
			return nil, app.BadRequest("company_role, is_active and daily_send_quota are required")
		}
		err = h.repo.UpdateCompanyMember(r.Context(), c.TenantID, m.UserID, id, b.Role, *b.Active, b.Quota)
		return map[string]bool{"updated": err == nil}, err
	})
}
func (h *CompanyHandler) Mailboxes(w http.ResponseWriter, r *http.Request) {
	c, m, err := h.member(r, false)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	p := pageFromReq(r)
	var items []*models.Mailbox
	var total int
	if r.URL.Query().Get("all") == "true" && m.Role == "admin" {
		items, total, err = h.store.ListMailboxes(r.Context(), c.TenantID, p)
	} else {
		items, total, err = h.repo.ListCompanyMailboxes(r.Context(), c.TenantID, m.UserID, p)
	}
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	if items == nil {
		items = []*models.Mailbox{}
	}
	okList(w, items, total, p.Page, p.PerPage)
}
func (h *CompanyHandler) CreateMailbox(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		var b struct {
			Local string `json:"local_part"`
		}
		if err := decodeBody(r, &b); err != nil {
			return nil, app.BadRequest("invalid mailbox request")
		}
		return h.repo.CreateCompanyMailbox(r.Context(), c.TenantID, m.UserID, b.Local)
	})
}
func (h *CompanyHandler) Grants(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, false, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		return h.repo.ListCompanyGrants(r.Context(), c.TenantID, m.UserID, r.URL.Query().Get("all") == "true" && m.Role == "admin")
	})
}
func (h *CompanyHandler) SetGrant(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		var g enterprise.Grant
		if err := decodeBody(r, &g); err != nil {
			return nil, app.BadRequest("invalid mailbox grant")
		}
		err := h.repo.SetCompanyGrant(r.Context(), c.TenantID, m.UserID, g)
		return map[string]bool{"updated": err == nil}, err
	})
}
func (h *CompanyHandler) Templates(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, false, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		return h.repo.ListCompanyTemplates(r.Context(), c.TenantID, m.UserID, r.URL.Query().Get("all") == "true" && m.Role == "admin")
	})
}
func (h *CompanyHandler) SaveTemplate(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		var t enterprise.Template
		if err := decodeBody(r, &t); err != nil {
			return nil, app.BadRequest("invalid template")
		}
		return h.repo.SaveCompanyTemplate(r.Context(), c.TenantID, m.UserID, t)
	})
}
func (h *CompanyHandler) TemplateStatus(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			return nil, app.BadRequest("invalid template id")
		}
		var b struct {
			Status string `json:"status"`
		}
		if err = decodeBody(r, &b); err != nil {
			return nil, app.BadRequest("invalid status")
		}
		err = h.repo.SetCompanyTemplateStatus(r.Context(), c.TenantID, m.UserID, id, b.Status)
		return map[string]bool{"updated": err == nil}, err
	})
}
func (h *CompanyHandler) Preview(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, false, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		var b struct {
			TemplateID uuid.UUID         `json:"template_id"`
			MailboxID  uuid.UUID         `json:"mailbox_id"`
			Variables  map[string]string `json:"variables"`
		}
		if err := decodeBody(r, &b); err != nil {
			return nil, app.BadRequest("invalid preview request")
		}
		t, err := h.repo.GetCompanyTemplate(r.Context(), c.TenantID, b.TemplateID)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, app.NotFound("template not found")
		}
		if m.Role != "admin" {
			g, err := h.repo.GetCompanyGrant(r.Context(), c.TenantID, m.UserID, b.MailboxID)
			if err != nil {
				return nil, err
			}
			if err = enterprise.CheckSender(m, g, true); err != nil {
				return nil, err
			}
			bound := false
			for _, id := range t.MailboxIDs {
				bound = bound || id == b.MailboxID
			}
			if !bound || t.Status != "published" {
				return nil, app.Forbidden("published template permission required")
			}
		}
		subject, text, html, err := enterprise.Render(*t, b.Variables, m.DisplayName, c.Name)
		return map[string]string{"subject": subject, "text_body": text, "html_body": html}, err
	})
}

// CompanyGuard blocks legacy management surfaces rather than trusting sidebar
// visibility. Company API keys are disabled until explicit service principals exist.
func CompanyGuard(st store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/company") {
				w.Header().Set("Cache-Control", "no-store")
			}
			if r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/accept-invite" {
				if checker, ok := st.(interface {
					HasCompanies(context.Context) (bool, error)
				}); ok {
					closed, err := checker.HasCompanies(r.Context())
					if err != nil {
						errInternal(w)
						return
					}
					if closed {
						errForbidden(w, "public registration and platform invitations are disabled in company mode")
						return
					}
				}
			}
			tenant := middleware.TenantFromCtx(r.Context())
			if tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			c, err := enterprise.Lookup(r.Context(), st, tenant.ID)
			if err != nil {
				errInternal(w)
				return
			}
			if c == nil {
				next.ServeHTTP(w, r)
				return
			}
			if middleware.AuthModeFromCtx(r.Context()) == middleware.AuthModeAPIKey {
				errForbidden(w, "company mode requires an employee session; legacy API keys are disabled")
				return
			}
			user := middleware.UserFromCtx(r.Context())
			if user != nil {
				m, err := st.(enterprise.Reader).GetCompanyMember(r.Context(), tenant.ID, user.ID)
				if err != nil {
					errInternal(w)
					return
				}
				if m == nil || !m.Active || (strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && m.Role != "admin") {
					errForbidden(w, "company permission required")
					return
				}
			}
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
				for _, prefix := range []string{"/api/v1/domains", "/api/v1/mailboxes", "/api/v1/keys", "/api/v1/send-identities", "/api/v1/webhook-endpoints", "/api/v1/suppression", "/api/v1/admin/users", "/api/v1/admin/permissions", "/api/v1/admin/domains"} {
					if r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, prefix+"/") {
						errForbidden(w, "use company governance endpoints; legacy mutation disabled")
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (h *CompanyHandler) Reinvite(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, true, func(c *enterprise.Company, m *enterprise.Member) (any, error) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			return nil, app.BadRequest("invalid employee id")
		}
		b := make([]byte, 32)
		if _, err = rand.Read(b); err != nil {
			return nil, err
		}
		token := hex.EncodeToString(b)
		if err = h.repo.ReissueCompanyInvitation(r.Context(), c.TenantID, m.UserID, id, enterprise.HashToken(token)); err != nil {
			return nil, err
		}
		return map[string]any{"activation_token": token, "expires_at": time.Now().UTC().Add(72 * time.Hour)}, nil
	})
}
