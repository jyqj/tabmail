package testpg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
)

// R5Fixture is test-owned state, never a production provider or serialized DSN.
// Roles and mailboxes are provisioned by the actual company commands.
type R5Fixture struct {
	Store     *postgres.PgStore
	Pool      *pgxpool.Pool
	Companies [2]R5Company
}
type R5Company struct {
	Tenant   *models.Tenant
	Zone     *models.DomainZone
	Admin    *models.User
	Actor    authz.Actor
	Users    map[string]*models.User
	Personal map[string]*models.Mailbox
	Shared   *models.Mailbox
}

// NewR5Fixture is an explicit R5 integration entry. Missing DSN is a failure,
// not the legacy NewPostgres helper's documented local skip.
func NewR5Fixture(t *testing.T) *R5Fixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit R5 fixture requires owned TABMAIL_TEST_DB_DSN")
	}
	st, pool, _ := NewPostgres(t)
	f := &R5Fixture{Store: st, Pool: pool}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, domain := range []string{"fixture-a.test", "fixture-b.test"} {
		c := R5Company{Users: map[string]*models.User{}, Personal: map[string]*models.Mailbox{}}
		c.Tenant = &models.Tenant{Name: domain, PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
		must(st.CreateTenant(ctx, c.Tenant))
		c.Admin = &models.User{TenantID: c.Tenant.ID, Email: "admin@" + domain, DisplayName: "Fixture administrator", Role: models.RoleAdmin, IsActive: true, PasswordHash: "test-only-not-a-password"}
		must(st.CreateUser(ctx, c.Admin))
		c.Actor = authz.Actor{Type: authz.PrincipalUser, ID: c.Admin.ID, TenantID: c.Tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
		c.Zone = &models.DomainZone{TenantID: c.Tenant.ID, Domain: domain, IsVerified: true, MXVerified: true}
		must(st.CreateZone(ctx, c.Zone))
		_, err := st.ConfigureCompany(ctx, c.Actor, company.Settings{Name: domain, PrimaryZoneID: c.Zone.ID})
		must(err)
		for _, role := range []string{"reader", "organizer", "sender", "frozen"} {
			invitation := company.Hash("fixture-invitation-" + domain + "-" + role)
			_, err = st.InviteEmployee(ctx, c.Actor, company.InvitationInput{Email: role + "@contact-" + domain, LocalPart: role, DisplayName: role}, invitation)
			must(err)
			must(st.ActivateEmployee(ctx, invitation, "test-only-not-a-password"))
			user, e := st.GetUserByEmail(ctx, role+"@contact-"+domain)
			must(e)
			if user == nil {
				t.Fatal("actual activated fixture user missing")
			}
			c.Users[role] = user
			mailbox, e := st.GetMailboxByAddress(ctx, role+"@"+domain)
			must(e)
			if mailbox == nil {
				t.Fatal("actual personal fixture mailbox missing")
			}
			c.Personal[role] = mailbox
		}
		c.Shared, err = st.CreateWorkMailbox(ctx, c.Actor, company.MailboxInput{LocalPart: "shared", Kind: "shared"})
		must(err)
		for _, role := range []string{"reader", "organizer", "sender"} {
			current, e := st.GetWorkMailbox(ctx, c.Actor, c.Shared.ID)
			must(e)
			grant := models.MailboxGrant{TenantID: c.Tenant.ID, MailboxID: c.Shared.ID, UserID: c.Users[role].ID, CanRead: true, CanOrganize: role == "organizer", CanSend: role == "sender"}
			must(st.SetWorkGrant(ctx, c.Actor, grant, current.Revision))
		}
		inactive := false
		user, e := st.UpdateUserGuarded(ctx, c.Actor, c.Tenant.ID, c.Users["frozen"].ID, models.UserAdminPatch{IsActive: &inactive})
		must(e)
		c.Users["frozen"] = user
		f.Companies[i] = c
	}
	return f
}

func (c R5Company) UserActor(role string) authz.Actor {
	u := c.Users[role]
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: c.Tenant.ID, Role: u.Role}
}
