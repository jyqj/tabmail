package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"tabmail/internal/company"
)

type companySetup interface {
	company.SettingsService
	company.EmployeeInvitations
}
type CompanySetupHandler struct {
	repo   companySetup
	logger zerolog.Logger
}

func NewCompanySetupHandler(repo companySetup, l zerolog.Logger) *CompanySetupHandler {
	return &CompanySetupHandler{repo, l}
}
func (h *CompanySetupHandler) result(w http.ResponseWriter, v any, e error) {
	companyResponse(w, h.logger, v, e)
}
func (h *CompanySetupHandler) Settings(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.GetCompanySettings(r.Context(), companyActor(r).TenantID)
	h.result(w, v, e)
}

func (h *CompanySetupHandler) Configure(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.Settings](w, r)
	if !ok {
		return
	}
	out, e := h.repo.ConfigureCompany(r.Context(), companyActor(r), v)
	h.result(w, out, e)
}

func (h *CompanySetupHandler) Invitations(w http.ResponseWriter, r *http.Request) {
	v, e := h.repo.ListEmployeeInvitations(r.Context(), companyActor(r))
	h.result(w, v, e)
}

func (h *CompanySetupHandler) Invite(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[company.InvitationInput](w, r)
	if !ok {
		return
	}
	buf := make([]byte, 32)
	if _, e := rand.Read(buf); e != nil {
		errInternal(w)
		return
	}
	token := hex.EncodeToString(buf)
	out, e := h.repo.InviteEmployee(r.Context(), companyActor(r), v, company.Hash(token))
	if e != nil {
		h.result(w, nil, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.result(w, map[string]any{"invitation": out, "activation_token": token}, nil)
}

func (h *CompanySetupHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	h.result(w, map[string]bool{"revoked": true}, h.repo.RevokeEmployeeInvitation(r.Context(), companyActor(r), id))
}

func (h *CompanySetupHandler) Activate(w http.ResponseWriter, r *http.Request) {
	v, ok := companyBody[struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}](w, r)
	if !ok {
		return
	}
	if len(v.Token) != 64 || len(v.Password) < 12 || len(v.Password) > 72 {
		errBadRequest(w, "activation token and 12-72 byte password required")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
	if e != nil {
		errInternal(w)
		return
	}
	h.result(w, map[string]bool{"activated": true}, h.repo.ActivateEmployee(r.Context(), company.Hash(v.Token), string(hash)))
}
