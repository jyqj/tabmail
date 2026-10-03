package handlers

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"net/http"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/recovery"
	"tabmail/internal/app/submissions"
	"tabmail/internal/company"
	"time"
)

type recoveryRepository interface {
	company.RecoveryService
	company.DeliveryRecovery
}
type CompanyRecoveryHandler struct {
	repo      recoveryRepository
	store     app.AuditStore
	objects   recovery.Objects
	subs      *submissions.Service
	logger    zerolog.Logger
	inspector *recovery.Service
}

func NewCompanyRecoveryHandler(repo recoveryRepository, st app.AuditStore, obj recovery.Objects, subs *submissions.Service, l zerolog.Logger) *CompanyRecoveryHandler {
	return &CompanyRecoveryHandler{repo: repo, store: st, objects: obj, subs: subs, logger: l, inspector: recovery.New(repo)}
}
func (h *CompanyRecoveryHandler) result(w http.ResponseWriter, v any, e error) {
	companyResponse(w, h.logger, v, e)
}
func (h *CompanyRecoveryHandler) Recovery(w http.ResponseWriter, r *http.Request) {
	page := pageFromReq(r)
	v, total, e := h.repo.ListRecoveryReceipts(r.Context(), companyActor(r), page)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	okList(w, v, total, page.Page, page.PerPage)
}

func (h *CompanyRecoveryHandler) InspectReceipt(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	body, ok := companyBody[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	v, e := h.repo.InspectRecoveryReceipt(r.Context(), companyActor(r), id, body.Reason)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	h.result(w, map[string]any{"receipt": v, "original_valid": recovery.Verify(r.Context(), h.objects, v)}, nil)
}

func (h *CompanyRecoveryHandler) RetryReceipt(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	body, ok := companyBody[struct {
		UpdatedAt time.Time   `json:"updated_at"`
		Targets   []uuid.UUID `json:"targets"`
		Reason    string      `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	v, e := h.repo.InspectRecoveryReceipt(r.Context(), companyActor(r), id, body.Reason)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	if !v.UpdatedAt.Equal(body.UpdatedAt) || !recovery.Verify(r.Context(), h.objects, v) {
		errConflict(w, "receipt changed or original failed verification; inspect again")
		return
	}
	h.result(w, map[string]bool{"queued": true}, h.repo.RetryRecoveryReceipt(r.Context(), companyActor(r), id, body.UpdatedAt, body.Targets, body.Reason, v.RawHash))
}

func (h *CompanyRecoveryHandler) Recipients(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	if h.subs == nil || !h.subs.OutboundEnabled() {
		errNotFound(w, "outbound disabled")
		return
	}
	// A ledger outcome endpoint is still an ordinary operation receipt. Current
	// content read does not grant addresses or diagnostics on this surface.
	view, e := h.subs.OutboundReceiptView(r.Context(), middleware.TenantFromCtx(r.Context()), companyActor(r), id)
	if e != nil {
		h.result(w, nil, e)
		return
	}
	h.result(w, view, nil)
}

func (h *CompanyRecoveryHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		UpdatedAt time.Time           `json:"updated_at"`
		Results   []company.Recipient `json:"results"`
		Reason    string              `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	h.result(w, map[string]bool{"reconciled": true}, h.repo.ReconcileOutbound(r.Context(), companyActor(r), id, v.UpdatedAt, v.Results, v.Reason))
}

func (h *CompanyRecoveryHandler) InspectOutbound(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r, "id")
	if !ok {
		return
	}
	v, ok := companyBody[struct {
		Reason string `json:"reason"`
	}](w, r)
	if !ok {
		return
	}
	if h.subs == nil || !h.subs.OutboundEnabled() {
		errNotFound(w, "outbound disabled")
		return
	}
	inspection, e := h.inspector.InspectOutboundRecovery(r.Context(), companyActor(r), id, v.Reason)
	h.result(w, inspection, e)
}
