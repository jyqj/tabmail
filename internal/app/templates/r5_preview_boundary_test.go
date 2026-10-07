package templates

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The adapter deliberately returns shared pointers and ignores cancellation.
// Preview must bind values before rendering and reject stale/invalid responses;
// the real repository remains responsible for its PostgreSQL authority rules.
type previewBoundaryRepo struct {
	company.TemplateAdminService
	mailbox   *company.MailboxAccess
	user      *models.User
	settings  *company.Settings
	version   *company.TemplateVersion
	employee  string
	company   string
	calls     []string
	counts    map[string]int
	faults    map[string]error
	hook      func(string, int)
	actor     authz.Actor
	mailboxID uuid.UUID
	versionID uuid.UUID
}

func (r *previewBoundaryRepo) step(port string) error {
	r.calls = append(r.calls, port)
	r.counts[port]++
	if r.hook != nil {
		r.hook(port, r.counts[port])
	}
	return r.faults[port]
}
func (r *previewBoundaryRepo) GetWorkMailbox(_ context.Context, actor authz.Actor, id uuid.UUID) (*company.MailboxAccess, error) {
	if actor.ID != r.actor.ID || actor.TenantID != r.actor.TenantID || id != r.mailboxID {
		return nil, errors.New("preview changed mailbox authority inputs")
	}
	err := r.step("mailbox")
	return r.mailbox, err
}
func (r *previewBoundaryRepo) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	if id != r.actor.ID {
		return nil, errors.New("preview changed identity lookup")
	}
	err := r.step("user")
	return r.user, err
}
func (r *previewBoundaryRepo) GetCompanySettings(_ context.Context, tenant uuid.UUID) (*company.Settings, error) {
	if tenant != r.actor.TenantID {
		return nil, errors.New("preview changed company lookup")
	}
	err := r.step("settings")
	return r.settings, err
}
func (r *previewBoundaryRepo) TemplateForSend(_ context.Context, tenant uuid.UUID, user, key *uuid.UUID, mailbox, version uuid.UUID) (*company.TemplateVersion, string, string, error) {
	if tenant != r.actor.TenantID || user == nil || *user != r.actor.ID || key != nil || mailbox != r.mailboxID || version != r.versionID {
		return nil, "", "", errors.New("preview changed published authority inputs")
	}
	err := r.step("version")
	return r.version, r.employee, r.company, err
}

type previewBoundaryFixture struct {
	svc   *Service
	repo  *previewBoundaryRepo
	actor authz.Actor
	input PreviewInput
}

func newPreviewBoundaryFixture(published bool) *previewBoundaryFixture {
	a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser}
	mailbox, version := uuid.New(), uuid.New()
	draft := company.TemplateDraft{Subject: "Hello {{.customer}}", TextBody: "{{.employee_name}} at {{.company_name}} from {{.sender_address}}", HTMLBody: "<p>{{.customer}}</p>", Variables: []company.Variable{{Name: "customer", Type: "text", Required: true, MaxLength: 100}}}
	r := &previewBoundaryRepo{
		mailbox:  &company.MailboxAccess{Mailbox: models.Mailbox{ID: mailbox, TenantID: a.TenantID, FullAddress: "sender@company.test"}, CanManage: !published, CanSend: published},
		user:     &models.User{ID: a.ID, TenantID: a.TenantID, IsActive: true, DisplayName: "Employee"},
		settings: &company.Settings{TenantID: a.TenantID, Name: "Company"},
		version:  &company.TemplateVersion{ID: version, TemplateID: uuid.New(), Version: 1, Snapshot: draft, ContentHash: company.Digest(draft)},
		employee: "Employee", company: "Company", counts: map[string]int{}, faults: map[string]error{}, actor: a, mailboxID: mailbox, versionID: version,
	}
	in := PreviewInput{Mailbox: mailbox, Draft: &draft, Vars: map[string]string{"customer": "Customer"}}
	if published {
		in.Draft, in.Version = nil, &version
	}
	return &previewBoundaryFixture{New(r, r), r, a, in}
}

func (f *previewBoundaryFixture) assertRejected(t *testing.T, ctx context.Context) error {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("invalid preview source panicked: %v", p)
		}
	}()
	result, err := f.svc.Preview(ctx, f.actor, f.input)
	if result != nil || err == nil {
		t.Errorf("unqualified preview released a render: result=%+v err=%v", result, err)
	}
	return err
}

func TestR5PreviewBoundaryPreservesSeparateCapabilities(t *testing.T) {
	for _, mode := range []string{"management-without-content-rights", "published-send-without-read", "draft-precedes-version"} {
		t.Run(mode, func(t *testing.T) {
			f := newPreviewBoundaryFixture(mode == "published-send-without-read")
			if mode == "published-send-without-read" {
				f.svc.identities = nil // Published lookup owns its identity projection.
			}
			if mode == "draft-precedes-version" {
				f.input.Version = &f.repo.versionID
				f.repo.faults["version"] = errors.New("draft input must keep existing precedence")
			}
			got, err := f.svc.Preview(context.Background(), f.actor, f.input)
			want := &Rendered{Subject: "Hello Customer", TextBody: "Employee at Company from sender@company.test", HTMLBody: "<p>Customer</p>"}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("legitimate preview changed: %+v %v", got, err)
			}
			if mode == "draft-precedes-version" && f.repo.counts["version"] != 0 {
				t.Error("management draft queried published usage")
			}
		})
	}
}

func TestR5PreviewBoundaryRejectsInvalidInitialSources(t *testing.T) {
	for _, mode := range []string{"nil-mailbox", "wrong-mailbox", "foreign-mailbox", "nil-user", "wrong-user", "foreign-user", "inactive-user", "nil-company", "foreign-company", "nil-version", "wrong-version", "revoked-version", "corrupt-snapshot"} {
		t.Run(mode, func(t *testing.T) {
			published := mode == "nil-version" || mode == "wrong-version" || mode == "revoked-version" || mode == "corrupt-snapshot"
			f := newPreviewBoundaryFixture(published)
			switch mode {
			case "nil-mailbox":
				f.repo.mailbox = nil
			case "wrong-mailbox":
				f.repo.mailbox.Mailbox.ID = uuid.New()
			case "foreign-mailbox":
				f.repo.mailbox.Mailbox.TenantID = uuid.New()
			case "nil-user":
				f.repo.user = nil
			case "wrong-user":
				f.repo.user.ID = uuid.New()
			case "foreign-user":
				f.repo.user.TenantID = uuid.New()
			case "inactive-user":
				f.repo.user.IsActive = false
			case "nil-company":
				f.repo.settings = nil
			case "foreign-company":
				f.repo.settings.TenantID = uuid.New()
			case "nil-version":
				f.repo.version = nil
			case "wrong-version":
				f.repo.version.ID = uuid.New()
			case "revoked-version":
				now := time.Now()
				f.repo.version.RevokedAt = &now
			case "corrupt-snapshot":
				f.repo.version.Snapshot.TextBody = "unbound replacement bytes"
			}
			f.assertRejected(t, context.Background())
		})
	}
}

func TestR5PreviewBoundaryManagementRechecksBeforeRelease(t *testing.T) {
	for _, change := range []string{"management-revoked", "mailbox-address", "employee-frozen", "employee-name", "company-name", "company-removed", "repository-error"} {
		t.Run(change, func(t *testing.T) {
			f := newPreviewBoundaryFixture(false)
			fault := errors.New("current company lookup failed")
			f.repo.hook = func(port string, call int) {
				if call != 2 {
					return
				}
				switch {
				case change == "management-revoked" && port == "mailbox":
					f.repo.mailbox.CanManage = false
				case change == "mailbox-address" && port == "mailbox":
					f.repo.mailbox.Mailbox.FullAddress = "changed@company.test"
				case change == "employee-frozen" && port == "user":
					f.repo.user.IsActive = false
				case change == "employee-name" && port == "user":
					f.repo.user.DisplayName = "Changed employee"
				case change == "company-name" && port == "settings":
					f.repo.settings.Name = "Changed company"
				case change == "company-removed" && port == "settings":
					f.repo.settings = nil
				case change == "repository-error" && port == "settings":
					f.repo.faults[port] = fault
				}
			}
			err := f.assertRejected(t, context.Background())
			if change == "repository-error" && !errors.Is(err, fault) {
				t.Errorf("lost repository error: %v", err)
			}
		})
	}
}

func TestR5PreviewBoundaryPublishedRechecksBeforeRelease(t *testing.T) {
	for _, change := range []string{"send-revoked", "mailbox-address", "grant-revoked", "version-revoked", "snapshot-replaced", "employee-name", "company-name"} {
		t.Run(change, func(t *testing.T) {
			f := newPreviewBoundaryFixture(true)
			fault := errors.New("current template use denied")
			f.repo.hook = func(port string, call int) {
				if call != 2 {
					return
				}
				switch {
				case change == "send-revoked" && port == "mailbox":
					f.repo.mailbox.CanSend = false
				case change == "mailbox-address" && port == "mailbox":
					f.repo.mailbox.Mailbox.FullAddress = "changed@company.test"
				case change == "grant-revoked" && port == "version":
					f.repo.faults[port] = fault
				case change == "version-revoked" && port == "version":
					now := time.Now()
					f.repo.version.RevokedAt = &now
				case change == "snapshot-replaced" && port == "version":
					f.repo.version.Snapshot.TextBody = "different self-consistent source"
					f.repo.version.ContentHash = company.Digest(f.repo.version.Snapshot)
				case change == "employee-name" && port == "version":
					f.repo.employee = "Changed employee"
				case change == "company-name" && port == "version":
					f.repo.company = "Changed company"
				}
			}
			err := f.assertRejected(t, context.Background())
			if change == "grant-revoked" && !errors.Is(err, fault) {
				t.Errorf("lost current usage denial: %v", err)
			}
		})
	}
}

func TestR5PreviewBoundaryCancellationStopsAtEachPort(t *testing.T) {
	for _, published := range []bool{false, true} {
		mode := map[bool]string{false: "management", true: "published"}[published]
		ports := []string{"mailbox", "user", "settings", "mailbox", "user", "settings"}
		if published {
			ports = []string{"mailbox", "version", "mailbox", "version"}
		}
		for stopAt := 0; stopAt <= len(ports); stopAt++ {
			name := "before-call"
			if stopAt > 0 {
				name = ports[stopAt-1] + map[bool]string{true: "-recheck", false: "-initial"}[stopAt > len(ports)/2]
			}
			t.Run(mode+"/"+name, func(t *testing.T) {
				f := newPreviewBoundaryFixture(published)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				fault := errors.New("cancelled preview port fault")
				if stopAt == 0 {
					cancel()
				} else {
					f.repo.hook = func(port string, _ int) {
						if len(f.repo.calls) == stopAt {
							cancel()
							f.repo.faults[port] = fault
						}
					}
				}
				err := f.assertRejected(t, ctx)
				if !errors.Is(err, context.Canceled) || stopAt > 0 && !errors.Is(err, fault) {
					t.Errorf("cancelled preview lost cancellation/port cause: %v", err)
				}
				if len(f.repo.calls) != stopAt {
					t.Errorf("cancelled preview performed ports %v, want stop at %d", f.repo.calls, stopAt)
				}
			})
		}
	}
}
