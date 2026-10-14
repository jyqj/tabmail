package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/app/templates"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// The shipping JWT middleware, handler, Preview and authority predicates run.
// The final database adapter is synthetic; this is not a PostgreSQL lock test.
type r5CrossHomePreviewRepo struct {
	company.TemplateAdminService
	store                                                    *testutil.FakeStore
	mailbox                                                  models.Mailbox
	mailboxReads, identityReads, settingsReads, versionReads int
	actors                                                   []authz.Actor
	hook                                                     func(string, int)
}

func (r *r5CrossHomePreviewRepo) GetWorkMailbox(ctx context.Context, a authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	r.mailboxReads++
	r.actors = append(r.actors, a)
	if r.hook != nil {
		r.hook("mailbox", r.mailboxReads)
	}
	if id != r.mailbox.ID || a.TenantID != r.mailbox.TenantID {
		return nil, app.NotFound("mailbox not found")
	}
	u, err := r.store.GetUser(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	current, valid := authz.RefreshMemberActor(a, a.TenantID, u)
	if !valid {
		return nil, authz.ErrForbidden("current employee required")
	}
	d := authz.EvaluateMailboxAccess(current, &r.mailbox, nil)
	return &company.MailboxAccess{Mailbox: r.mailbox, CanManage: d.CanManage, CanRead: d.CanRead, CanSend: d.CanSend}, nil
}
func (r *r5CrossHomePreviewRepo) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	r.identityReads++
	if r.hook != nil {
		r.hook("identity", r.identityReads)
	}
	return r.store.GetUser(ctx, id)
}
func (r *r5CrossHomePreviewRepo) GetCompanySettings(_ context.Context, tenant uuid.UUID) (*company.Settings, error) {
	r.settingsReads++
	if r.hook != nil {
		r.hook("settings", r.settingsReads)
	}
	if tenant != r.mailbox.TenantID {
		return nil, app.Forbidden("wrong selected company")
	}
	return &company.Settings{TenantID: tenant, Name: "Selected company"}, nil
}
func (r *r5CrossHomePreviewRepo) TemplateForSend(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, uuid.UUID, uuid.UUID) (*company.TemplateVersion, string, string, error) {
	r.versionReads++
	return nil, "", "", authz.ErrForbidden("no published use grant")
}

func TestR5PreviewCrossHomeHTTP(t *testing.T) {
	for _, mode := range []string{"cross-home", "same-home-superadmin", "ordinary-home-admin", "ordinary-selected-foreign", "demoted-token", "frozen-before-auth", "session-before-auth", "demoted-before-identity", "session-after-settings", "published-no-send"} {
		t.Run(mode, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			user, tenant := f.platformAdmin, f.tenantID
			if mode == "same-home-superadmin" {
				tenant = f.otherTenantID
			}
			if mode == "ordinary-home-admin" || mode == "ordinary-selected-foreign" {
				user = f.tenantAdmin
			}
			if mode == "ordinary-selected-foreign" {
				tenant = f.otherTenantID
			}
			user.DisplayName = "Home-tenant operator"
			if err := f.st.CreateUser(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			headers := outboundUserHeaders(t, user) // Signed before the stale-credential controls.
			headers["X-Tenant-ID"] = tenant.String()
			repo := &r5CrossHomePreviewRepo{store: f.st, mailbox: models.Mailbox{ID: uuid.New(), TenantID: tenant, FullAddress: "selected@company.test"}}
			changeUser := func(change func(*models.User)) {
				u, err := f.st.GetUser(context.Background(), user.ID)
				if err != nil {
					t.Fatal(err)
				}
				change(u)
				if err := f.st.CreateUser(context.Background(), u); err != nil {
					t.Fatal(err)
				}
			}
			wantStatus := http.StatusOK
			lateGuardReached := false
			switch mode {
			case "ordinary-selected-foreign":
				wantStatus = http.StatusNotFound
			case "demoted-token":
				changeUser(func(u *models.User) { u.Role = models.RoleAdmin })
				wantStatus = http.StatusNotFound
			case "frozen-before-auth":
				changeUser(func(u *models.User) { u.IsActive = false })
				wantStatus = http.StatusUnauthorized
			case "session-before-auth":
				changeUser(func(u *models.User) { u.SessionVersion++ })
				wantStatus = http.StatusUnauthorized
			case "demoted-before-identity":
				wantStatus = http.StatusForbidden
				repo.hook = func(port string, count int) {
					if port == "identity" && count == 1 {
						lateGuardReached = true
						changeUser(func(u *models.User) { u.Role = models.RoleAdmin })
					}
				}
			case "session-after-settings":
				wantStatus = http.StatusForbidden
				repo.hook = func(port string, count int) {
					if port == "settings" && count == 2 {
						lateGuardReached = true
						changeUser(func(u *models.User) { u.SessionVersion++ })
					}
				}
			case "published-no-send":
				wantStatus = http.StatusForbidden
			}
			in := templates.PreviewInput{Mailbox: repo.mailbox.ID, Draft: &company.TemplateDraft{Subject: "Management preview", TextBody: "{{.employee_name}} / {{.company_name}} / {{.sender_address}}"}}
			if mode == "published-no-send" {
				version := uuid.New()
				in.Draft, in.Version = nil, &version
			}
			body, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/company/templates/preview", bytes.NewReader(body))
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			h := NewCompanyTemplateHandler(repo, repo, zerolog.Nop())
			state := middleware.NewAuthState(nil)
			w := httptest.NewRecorder()
			middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireAuth(http.HandlerFunc(h.Preview))).ServeHTTP(w, req)
			if err := state.StopContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if w.Code != wantStatus {
				t.Fatalf("status=%d want=%d response=%s", w.Code, wantStatus, w.Body.String())
			}
			if wantStatus == http.StatusOK {
				var out struct {
					Data templates.Rendered `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.Data.Subject != "Management preview" || out.Data.TextBody != "Home-tenant operator / Selected company / selected@company.test" {
					t.Fatalf("wrong preview identity sources: %+v", out.Data)
				}
				if repo.mailboxReads != 3 || repo.identityReads != 2 || repo.settingsReads != 2 {
					t.Fatalf("missing final source/authority checks: %+v", repo)
				}
				for _, a := range repo.actors {
					if a.TenantID != tenant || a.ID != user.ID || a.SessionVersion == nil || *a.SessionVersion != user.SessionVersion {
						t.Fatalf("JWT selection/version lost: %+v", a)
					}
				}
			} else if strings.Contains(w.Body.String(), "Home-tenant operator") || strings.Contains(w.Body.String(), "Management preview") {
				t.Fatalf("refused request leaked rendered bytes: %s", w.Body.String())
			}
			if (mode == "demoted-before-identity" || mode == "session-after-settings") && !lateGuardReached {
				t.Fatalf("never reached required current-identity guard: %+v", repo)
			}
			if repo.versionReads != 0 {
				t.Fatal("management capability queried/granted published use")
			}
		})
	}
}
