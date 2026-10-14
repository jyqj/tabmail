package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	domainapp "tabmail/internal/app/domains"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

type continueDomainStore struct {
	*testutil.FakeStore
	failure                     error
	creates, identities, audits int
}

func (s *continueDomainStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &models.EffectiveConfig{MaxDomains: 100}, nil
}
func (s *continueDomainStore) CreateZone(ctx context.Context, z *models.DomainZone) error {
	s.creates++
	if s.failure != nil {
		return s.failure
	}
	return s.FakeStore.CreateZone(ctx, z)
}
func (s *continueDomainStore) CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error {
	s.identities++
	return s.FakeStore.CreateSendIdentity(ctx, si)
}
func (s *continueDomainStore) InsertAudit(ctx context.Context, entry *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, entry)
}

type continueDomainInvalidator struct{ zones, routes int }

func (i *continueDomainInvalidator) InvalidateZone(string)      { i.zones++ }
func (i *continueDomainInvalidator) InvalidateRoutes(uuid.UUID) { i.routes++ }

// The final storage port is synthetic. Both modes use the production domain
// service; HTTP also uses real JWT Auth, RequireAuth/Admin and company handler.
func TestContinueDomainConflictPipeline(t *testing.T) {
	for _, mode := range []string{"service", "HTTP"} {
		for _, name := range []string{"typed", "wrapped typed", "joined typed cause", "misleading duplicate", "misleading unique", "misleading SQLSTATE", "cancelled unique", "deadline duplicate", "ordinary failure", "unmapped PG error", "fake duplicate", "fake normalized duplicate", "fake other tenant duplicate", "success"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				f := newOutboundAccessFixture(t)
				st := &continueDomainStore{FakeStore: f.st}
				inv := &continueDomainInvalidator{}
				svc := domainapp.NewService(st, nil, "mx.example.test", inv, zerolog.Nop())
				h := NewCompanyDomainHandler(svc, nil, zerolog.Nop())
				domain := "created.example"
				wantStatus := http.StatusInternalServerError
				var original error
				switch name {
				case "typed":
					original = store.ErrDomainAlreadyExists
					wantStatus = 409
				case "wrapped typed":
					original = fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", store.ErrDomainAlreadyExists))
					wantStatus = 409
				case "joined typed cause":
					original = errors.Join(store.ErrDomainAlreadyExists, errors.New("private-original-store-cause"))
					wantStatus = 409
				case "misleading duplicate":
					original = errors.New("private duplicate driver description")
				case "misleading unique":
					original = errors.New("private UNIQUE transport unavailable")
				case "misleading SQLSTATE":
					original = errors.New("private connection to host-23505 failed")
				case "cancelled unique":
					original = fmt.Errorf("private unique request: %w", context.Canceled)
				case "deadline duplicate":
					original = fmt.Errorf("private duplicate deadline: %w", context.DeadlineExceeded)
				case "ordinary failure":
					original = errors.New("private storage unavailable")
				case "unmapped PG error":
					original = &pgconn.PgError{Code: "23505", ConstraintName: "unrelated_unique_index", Message: "private storage diagnostic"}
				case "fake duplicate", "fake normalized duplicate", "fake other tenant duplicate":
					tenant := f.tenantID
					if name == "fake other tenant duplicate" {
						tenant = f.otherTenantID
					}
					f.st.SeedZone(&models.DomainZone{ID: uuid.New(), TenantID: tenant, Domain: domain})
					wantStatus = 409
					if name == "fake normalized duplicate" {
						domain = " Created.Example. "
					}
				case "success":
					wantStatus = 201
				}
				st.failure = original
				if mode == "service" {
					tenant, e := st.GetTenant(context.Background(), f.tenantID)
					if e != nil {
						t.Fatal(e)
					}
					actor := authz.Actor{Type: authz.PrincipalUser, ID: f.tenantAdmin.ID, TenantID: f.tenantID, Role: models.RoleAdmin, IsAdmin: true}
					zone, err := svc.CreateZone(context.Background(), actor, tenant, domain)
					if wantStatus == 201 {
						if err != nil || zone == nil || zone.Domain != "created.example" {
							t.Errorf("success result: zone=%v err=%v", zone, err)
						}
					} else {
						expected := app.KindInternal
						if wantStatus == 409 {
							expected = app.KindConflict
						}
						ae, ok := app.As(err)
						if !ok || ae.Kind != expected || zone != nil {
							t.Errorf("kind: err=%v expected=%s zone=%v", err, expected, zone)
						}
						if original != nil && !errors.Is(err, original) {
							t.Errorf("original cause lost: got %v want %v", err, original)
						}
						if wantStatus == 409 && !errors.Is(err, store.ErrDomainAlreadyExists) {
							t.Errorf("typed domain conflict missing: %v", err)
						}
					}
				} else {
					req := httptest.NewRequest("POST", "/api/v1/company/domains", strings.NewReader(fmt.Sprintf(`{"domain":%q}`, domain)))
					for k, v := range outboundUserHeaders(t, f.tenantAdmin) {
						req.Header.Set(k, v)
					}
					state := middleware.NewAuthState(nil)
					w := httptest.NewRecorder()
					middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireAuth(middleware.RequireAdmin(http.HandlerFunc(h.Create)))).ServeHTTP(w, req)
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if e := state.StopContext(ctx); e != nil {
						t.Fatal(e)
					}
					if w.Code != wantStatus {
						t.Errorf("status=%d want=%d body=%s", w.Code, wantStatus, w.Body.String())
					}
					if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "23505") || strings.Contains(w.Body.String(), "unrelated_unique_index") || strings.Contains(w.Body.String(), "dkim_private") {
						t.Errorf("storage/private data leaked: %s", w.Body.String())
					}
					if wantStatus == 409 && !strings.Contains(w.Body.String(), `"code":"CONFLICT"`) {
						t.Errorf("conflict code changed: %s", w.Body.String())
					}
				}
				wantEffects := 0
				if wantStatus == 201 {
					wantEffects = 1
				}
				if st.creates != 1 || st.identities != wantEffects || st.audits != wantEffects || inv.zones != wantEffects || inv.routes != 0 {
					t.Errorf("side effects creates=%d identities=%d audits=%d zone/routedeviction=%d/%d want=%d", st.creates, st.identities, st.audits, inv.zones, inv.routes, wantEffects)
				}
			})
		}
	}
}

func TestContinueDomainConflictAuthorization(t *testing.T) {
	for _, name := range []string{"anonymous", "employee", "frozen", "stale session"} {
		t.Run(name, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			st := &continueDomainStore{FakeStore: f.st, failure: store.ErrDomainAlreadyExists}
			h := NewCompanyDomainHandler(domainapp.NewService(st, nil, "mx.example.test", nil, zerolog.Nop()), nil, zerolog.Nop())
			headers := outboundUserHeaders(t, f.tenantAdmin)
			want := 401
			switch name {
			case "anonymous":
				headers = nil
			case "employee":
				headers = outboundUserHeaders(t, f.userA)
				want = 403
			case "frozen":
				f.tenantAdmin.IsActive = false
				if e := st.UpdateUser(context.Background(), f.tenantAdmin); e != nil {
					t.Fatal(e)
				}
			case "stale session":
				f.tenantAdmin.SessionVersion++
				if e := st.UpdateUser(context.Background(), f.tenantAdmin); e != nil {
					t.Fatal(e)
				}
			}
			req := httptest.NewRequest("POST", "/api/v1/company/domains", strings.NewReader(`{"domain":"created.example"}`))
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			state := middleware.NewAuthState(nil)
			w := httptest.NewRecorder()
			middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireAuth(middleware.RequireAdmin(http.HandlerFunc(h.Create)))).ServeHTTP(w, req)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if e := state.StopContext(ctx); e != nil {
				t.Fatal(e)
			}
			if w.Code != want || st.creates != 0 || st.identities != 0 || st.audits != 0 {
				t.Errorf("status=%d want=%d side effects=%d/%d/%d", w.Code, want, st.creates, st.identities, st.audits)
			}
		})
	}
}
