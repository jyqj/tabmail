package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

type streamBoundaryRepo struct {
	mailbox               models.Mailbox
	calls, reads          int
	allowFirst, allowPoll bool
	pollErr               error
	cancel                context.CancelFunc
}

func (s *streamBoundaryRepo) GetWorkMailbox(_ context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	s.calls++
	if a.TenantID != s.mailbox.TenantID || id != s.mailbox.ID {
		return nil, errors.New("wrong stream scope")
	}
	if s.calls > 1 && s.pollErr != nil {
		return nil, s.pollErr
	}
	allowed := s.allowFirst
	if s.calls > 1 {
		allowed = s.allowPoll
	}
	return &company.MailboxAccess{Mailbox: s.mailbox, CanRead: allowed, CanManage: true}, nil
}
func (s *streamBoundaryRepo) ListMailboxEvents(_ context.Context, tenant, mailbox uuid.UUID, _ int64, _ int) ([]company.MailEvent, int64, error) {
	s.reads++
	if tenant != s.mailbox.TenantID || mailbox != s.mailbox.ID {
		return nil, 0, errors.New("wrong event scope")
	}
	if s.cancel != nil {
		s.cancel()
	}
	return []company.MailEvent{{Sequence: 42, Type: "changed"}}, 42, nil
}
func TestCompanyEventStreamDoesNotInheritAdministrativeReadBypass(t *testing.T) {
	for _, mode := range []string{"initial-denial", "revoked-before-poll", "lookup-failure", "nil-revalidation", "allowed"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			r := &streamBoundaryRepo{mailbox: models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, FullAddress: "shared@company.test"}, allowFirst: true}
			var revalidate func(*http.Request) (*http.Request, error)
			switch mode {
			case "initial-denial":
				r.allowFirst = false
			case "lookup-failure":
				r.pollErr = errors.New("database unavailable")
			case "nil-revalidation":
				revalidate = func(*http.Request) (*http.Request, error) { return nil, nil }
			case "allowed":
				r.allowPoll = true
			}
			h := NewMailboxEventHandler(r, revalidate, zerolog.Nop())
			response := doOutboundHandlerRequest(t, f.st, func(w http.ResponseWriter, req *http.Request) {
				ctx, cancel := context.WithCancel(req.Context())
				defer cancel()
				r.cancel = cancel
				h.Events(w, req.WithContext(ctx))
			}, http.MethodGet, "/events", map[string]string{"id": r.mailbox.ID.String()}, outboundUserHeaders(t, f.tenantAdmin))
			if mode == "allowed" {
				if r.reads != 1 || !strings.Contains(response.Body.String(), "id: 42") {
					t.Fatalf("authorized stream broken: %d %s", r.reads, response.Body.String())
				}
			} else {
				if r.reads != 0 || strings.Contains(response.Body.String(), "event: changed") || strings.Contains(response.Body.String(), "event: ready") {
					t.Fatal("administrative authority outlived read grant")
				}
				if mode == "initial-denial" && response.Code != http.StatusForbidden {
					t.Fatalf("expected 403, got %d", response.Code)
				}
			}
		})
	}
}
