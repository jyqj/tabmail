package handlers

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/app"
	"tabmail/internal/app/employees"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

type EmployeeLifecycleHandler struct {
	service *employees.Service
	logger  zerolog.Logger
}

func NewEmployeeLifecycleHandler(repo company.OffboardingPlanner, l zerolog.Logger) *EmployeeLifecycleHandler {
	return &EmployeeLifecycleHandler{service: employees.New(repo), logger: l}
}
func companyResponse(w http.ResponseWriter, l zerolog.Logger, v any, e error) {
	if e != nil {
		if authz.IsAuthzError(e) {
			e = app.Forbidden(e.Error())
		}
		respondAppError(w, l, e)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	ok(w, v)
}
func (h *EmployeeLifecycleHandler) Preview(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	in, valid := companyBody[struct {
		SuccessorID uuid.UUID                  `json:"successor_user_id"`
		Options     company.OffboardingOptions `json:"options"`
		Reason      string                     `json:"reason"`
	}](w, r)
	if !valid {
		return
	}
	p, e := h.service.Preview(r.Context(), companyActor(r), id, in.SuccessorID, in.Options, in.Reason)
	companyResponse(w, h.logger, p, e)
}
func (h *EmployeeLifecycleHandler) Execute(w http.ResponseWriter, r *http.Request) {
	id, valid := companyID(w, r, "id")
	if !valid {
		return
	}
	in, valid := companyBody[struct {
		PlanID uuid.UUID `json:"plan_id"`
	}](w, r)
	if !valid {
		return
	}
	p, e := h.service.Execute(r.Context(), companyActor(r), id, in.PlanID)
	companyResponse(w, h.logger, p, e)
}
