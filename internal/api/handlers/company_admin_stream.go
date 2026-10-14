package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// CompanyAdminEventReader is an optional narrow transport seam, not another
// broker or an extension of company mutations. Production uses PgStore's
// original outbox and currentMemberActor guard.
type CompanyAdminEventReader interface {
	ListCompanyAdminEvents(context.Context, authz.Actor, int) ([]models.OutboxEvent, error)
	WithCompanyAdminEventAccess(context.Context, authz.Actor, func() error) error
}

type CompanyAdminEventHandler struct {
	reader     CompanyAdminEventReader
	revalidate func(*http.Request) (*http.Request, error)
	logger     zerolog.Logger
}

func NewCompanyAdminEventHandler(reader CompanyAdminEventReader, revalidate func(*http.Request) (*http.Request, error), logger zerolog.Logger) *CompanyAdminEventHandler {
	return &CompanyAdminEventHandler{reader: reader, revalidate: revalidate, logger: logger}
}

type companyAdminMetadata struct {
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type companyAdminInvalidation struct {
	Type       string               `json:"type"`
	TenantID   string               `json:"tenant_id"`
	OccurredAt time.Time            `json:"occurred_at"`
	Metadata   companyAdminMetadata `json:"metadata"`
}

// Personal message.* / sent.* changes are intentionally not admin broadcasts.
// New producers must explicitly join this allow-list rather than inheriting an
// arbitrary payload or a wildcard action prefix.
func companyAdminResourceType(action string) string {
	switch action {
	case "company.configure", "company.index_retry":
		return "company"
	case "employee.invite", "employee.invite_revoke":
		return "invitation"
	case "employee.activate", "employee.offboard.preview", "employee.offboard", "permission.override.patch", "permission.profile.assign":
		return "user"
	case "mailbox.provision", "mailbox.handover", "mailbox.grant", "mailbox.send_policy", "mailbox.convert_shared":
		return "mailbox"
	case "template.save", "template.publish", "template.retire", "template.version.revoke", "template.grant":
		return "template"
	case "permission.profile.update", "permission.profile.delete":
		return "permission_profile"
	case "ingress.inspect", "ingress.retry":
		return "ingest_job"
	case "outbound.retry", "outbound.reconcile", "outbound.break_glass":
		return "outbound_job"
	}
	return ""
}

func projectCompanyAdminInvalidation(event models.OutboxEvent, tenant uuid.UUID) (companyAdminInvalidation, bool) {
	var value companyAdminInvalidation
	if event.ID == uuid.Nil || event.EventType != "company.admin.changed" || json.Unmarshal(event.Payload, &value) != nil {
		return value, false
	}
	resource, err := uuid.Parse(value.Metadata.ResourceID)
	if value.Type != event.EventType || value.TenantID != tenant.String() || value.OccurredAt.IsZero() || err != nil || resource == uuid.Nil {
		return value, false
	}
	allowed := companyAdminResourceType(value.Metadata.Action)
	if allowed == "" || value.Metadata.ResourceType != allowed {
		return value, false
	}
	value.Metadata.ResourceID = resource.String()
	return value, true
}

func (h *CompanyAdminEventHandler) Events(w http.ResponseWriter, r *http.Request) {
	initial := companyActor(r)
	if initial.Type != authz.PrincipalUser || initial.ID == uuid.Nil || initial.TenantID == uuid.Nil || !initial.IsTenantAdmin() {
		errForbidden(w, "interactive company administrator required")
		return
	}
	if h.reader == nil || h.revalidate == nil {
		errInternal(w)
		return
	}
	freshActor := func() (*http.Request, authz.Actor, error) {
		if err := r.Context().Err(); err != nil {
			return nil, authz.Actor{}, err
		}
		fresh, err := h.revalidate(r)
		if err != nil {
			return nil, authz.Actor{}, err
		}
		if fresh == nil {
			return nil, authz.Actor{}, authz.ErrForbidden("stream authentication unavailable")
		}
		current := companyActor(fresh)
		sameSession := (initial.SessionVersion == nil && current.SessionVersion == nil) || (initial.SessionVersion != nil && current.SessionVersion != nil && *initial.SessionVersion == *current.SessionVersion)
		if current.Type != initial.Type || current.ID != initial.ID || current.TenantID != initial.TenantID || !current.IsTenantAdmin() || !sameSession {
			return nil, authz.Actor{}, authz.ErrForbidden("stream authority changed")
		}
		return fresh, current, nil
	}
	read := func() ([]models.OutboxEvent, error) {
		fresh, current, err := freshActor()
		if err != nil {
			return nil, err
		}
		return h.reader.ListCompanyAdminEvents(fresh.Context(), current, 200)
	}
	events, err := read()
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "private, no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	scope := struct {
		TenantID string `json:"tenant_id"`
	}{initial.TenantID.String()}
	frame := func(name, id string, payload any) error {
		fresh, current, err := freshActor()
		if err != nil {
			return err
		}
		return h.reader.WithCompanyAdminEventAccess(fresh.Context(), current, func() error {
			if err := fresh.Context().Err(); err != nil {
				return err
			}
			// One small frame has one absolute two-second budget. Do not reset
			// this while writing/flushing or continue the batch after an error.
			controller := http.NewResponseController(w)
			if err := controller.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return err
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if id != "" {
				if _, err = fmt.Fprintf(w, "id: %s\n", id); err != nil {
					return err
				}
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
				return err
			}
			if err = controller.Flush(); err != nil {
				return err
			}
			if err = fresh.Context().Err(); err != nil {
				return err
			}
			// End this frame's budget before the idle interval. In particular,
			// HTTP/2's deadline timer must not expire while no frame is pending.
			return controller.SetWriteDeadline(time.Time{})
		})
	}
	// UUID Last-Event-ID is advisory deduplication only. Never query after it:
	// outbox retention, late commits and reconnects all require a fresh snapshot.
	seen := make(map[uuid.UUID]bool, 400)
	order := make([]uuid.UUID, 0, 400)
	emit := func(events []models.OutboxEvent) bool {
		for _, event := range events {
			value, valid := projectCompanyAdminInvalidation(event, initial.TenantID)
			if !valid || seen[event.ID] {
				continue
			}
			if err := frame("company.admin.changed", event.ID.String(), value); err != nil {
				return false
			}
			seen[event.ID] = true
			order = append(order, event.ID)
			if len(order) > 400 {
				delete(seen, order[0])
				order = order[1:]
			}
		}
		return true
	}
	if frame("ready", "", scope) != nil || frame("resync", "", scope) != nil {
		return
	}
	if !emit(events) {
		return
	}
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	resync := time.NewTicker(25 * time.Second)
	defer resync.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			events, err = read()
			if err != nil || !emit(events) {
				return
			}
		case <-resync.C:
			events, err = read()
			if err != nil || !emit(events) {
				return
			}
			if frame("resync", "", scope) != nil {
				return
			}
		}
	}
}
