package postgres_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

type r5HistoricalMemberFixture struct {
	st                       *postgres.PgStore
	pool                     *pgxpool.Pool
	tenant                   *models.Tenant
	admin, member, successor *models.User
	actor, sender            authz.Actor
	zone                     *models.DomainZone
	shared                   *models.Mailbox
}

func r5HistoricalMemberSeed(t *testing.T) *r5HistoricalMemberFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("historical member deletion acceptance requires owned TABMAIL_TEST_DB_DSN; no skip")
	}
	ctx := context.Background()
	st, pool, _ := testpg.NewPostgres(t) // The helper owns close/drop cleanup, even on Fatal.
	f := &r5HistoricalMemberFixture{st: st, pool: pool}
	f.tenant = &models.Tenant{Name: "Historical identity", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, f.tenant))
	f.admin = r5HistoricalMemberUser(t, f, f.tenant.ID, models.RoleAdmin)
	f.member = r5HistoricalMemberUser(t, f, f.tenant.ID, models.RoleUser)
	f.successor = r5HistoricalMemberUser(t, f, f.tenant.ID, models.RoleUser)
	canSend := true
	must(t, st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.member.ID, CanSend: &canSend}))
	f.actor = r5HistoricalMemberActor(f.admin)
	f.sender = r5HistoricalMemberActor(f.member)
	f.zone = &models.DomainZone{TenantID: f.tenant.ID, Domain: "historical.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, f.zone))
	_, err := st.ConfigureCompany(ctx, f.actor, company.Settings{Name: f.tenant.Name, PrimaryZoneID: f.zone.ID})
	must(t, err)
	f.shared, err = st.CreateWorkMailbox(ctx, f.actor, company.MailboxInput{LocalPart: "shared", Kind: "shared"})
	must(t, err)
	must(t, st.SetMailboxGrant(ctx, &models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.member.ID, CanRead: true, CanSend: true}))
	return f
}

func r5HistoricalMemberUser(t *testing.T, f *r5HistoricalMemberFixture, tenant uuid.UUID, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{TenantID: tenant, Email: uuid.NewString() + "@historical.test", Role: role, IsActive: true, PasswordHash: "test-only-historical"}
	must(t, f.st.CreateUser(context.Background(), u))
	return u
}

func r5HistoricalMemberActor(u *models.User) authz.Actor {
	v := u.SessionVersion
	return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, IsAdmin: u.Role == models.RoleAdmin, IsSuperAdmin: u.Role == models.RoleSuperAdmin, SessionVersion: &v}
}

func r5HistoricalMemberCredentials(t *testing.T, f *r5HistoricalMemberFixture, user uuid.UUID) {
	t.Helper()
	must(t, f.st.CreateAPIKey(context.Background(), &models.TenantAPIKey{TenantID: f.tenant.ID, OwnerUserID: &user, KeyHash: company.Hash(uuid.NewString()), KeyPrefix: "test", Label: "retained credential", Scopes: []string{"send:write"}}))
	must(t, f.st.CreateRefreshToken(context.Background(), &models.RefreshToken{UserID: user, TokenHash: company.Hash(uuid.NewString()), ExpiresAt: time.Now().Add(time.Hour)}))
}

// These are tiny isolated fixtures. Compare actual durable rows, not just the
// domain error or counts; a refusal must not revoke keys, mutate users, null
// sender attribution, consume tombstones, cascade drafts, or append audits.
func r5HistoricalMemberState(t *testing.T, f *r5HistoricalMemberFixture) string {
	t.Helper()
	var out string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'users',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM users r),
 'overrides',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM user_permission_overrides r),
 'keys',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM tenant_api_keys r),
 'refresh',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM refresh_tokens r),
 'mailboxes',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM mailboxes r),
 'grants',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.mailbox_id,r.user_id) FROM mailbox_grants r),
 'plans',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM employee_offboarding_plans r),
 'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM draft_creation_receipts r),
 'drafts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM mail_drafts r),
 'attachments',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM mail_attachments r),
 'jobs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM outbound_jobs r),
 'recipients',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.job_id,r.address) FROM outbound_recipients r),
 'sent',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM sent_mail_assets r),
 'items',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.asset_id) FROM sent_mail_items r),
 'audits',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM audit_log r),
 'outbox',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM outbox_events r))::text`).Scan(&out))
	return out
}

func r5HistoricalMemberReject(t *testing.T, f *r5HistoricalMemberFixture, target uuid.UUID, want error) {
	t.Helper()
	r5HistoricalMemberCredentials(t, f, target)
	var owns bool
	must(t, f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM mailboxes WHERE tenant_id=$1 AND owner_user_id=$2)`, f.tenant.ID, target).Scan(&owns))
	if owns {
		t.Fatal("historical refusal fixture is accidentally protected by mailbox ownership")
	}
	before := r5HistoricalMemberState(t, f)
	err := f.st.DeleteUserGuarded(context.Background(), f.actor, f.tenant.ID, target)
	if !errors.Is(err, want) {
		t.Fatalf("guarded deletion error=%v, want=%v", err, want)
	}
	if r5HistoricalMemberState(t, f) != before {
		t.Fatal("rejected deletion changed durable identity/evidence/credentials/audit/outbox")
	}
}

func TestR5MemberHistoricalDeleteIncomingFKSchema19(t *testing.T) {
	f := r5HistoricalMemberSeed(t)
	var version int
	must(t, f.pool.QueryRow(context.Background(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
	if version != 19 {
		t.Fatalf("incoming FK contract requires current schema19, got=%d", version)
	}
	rows, err := f.pool.Query(context.Background(), `SELECT c.conrelid::regclass::text||'.'||a.attname,c.confdeltype::text
 FROM pg_constraint c CROSS JOIN LATERAL unnest(c.conkey) AS k(attnum)
 JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum
 WHERE c.contype='f' AND c.confrelid='users'::regclass AND a.attname<>'tenant_id'`)
	must(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var column, action string
		must(t, rows.Scan(&column, &action))
		got[column] = action
	}
	must(t, rows.Err())
	// a=NO ACTION, c=CASCADE, n=SET NULL. The explicit guard covers all
	// NO ACTION parents; transient credentials/current ACLs keep their existing
	// cascade policy. Only the job SET NULL would erase durable send identity.
	want := map[string]string{
		"mailboxes.owner_user_id": "a", "mail_attachments.user_id": "a", "draft_creation_receipts.user_id": "a",
		"employee_offboarding_plans.target_id": "a", "employee_offboarding_plans.successor_id": "a",
		"tenant_api_keys.owner_user_id": "c", "refresh_tokens.user_id": "c", "user_permission_overrides.user_id": "c",
		"mailbox_grants.user_id": "c", "mail_template_grants.user_id": "c", "mail_drafts.user_id": "c", "message_user_states.user_id": "c",
		"admin_invitations.invited_by": "n", "domain_zones.owner_user_id": "n", "mailbox_grants.granted_by": "n",
		"outbound_jobs.user_id": "n", "webhook_endpoints.created_by": "n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("current schema19 incoming users FK/action drift: got=%v want=%v", got, want)
	}
}

func TestR5MemberHistoricalDeleteRetainedReferences(t *testing.T) {
	for _, name := range []string{"expired-preview-target", "expired-preview-successor", "executed-target", "executed-successor", "deleted-draft-tombstone", "legacy-sealed-draft", "attachment", "outbound-user-only", "outbound-sender-only"} {
		t.Run(name, func(t *testing.T) {
			f := r5HistoricalMemberSeed(t)
			ctx := context.Background()
			target := f.member.ID
			switch name {
			case "expired-preview-target", "expired-preview-successor", "executed-target", "executed-successor":
				plan, err := f.st.PreviewOffboarding(ctx, f.actor, f.member.ID, f.successor.ID, company.OffboardingOptions{Drafts: "seal"}, "Retain historical identity evidence")
				must(t, err)
				if strings.HasPrefix(name, "executed") {
					_, err = f.st.ExecuteOffboarding(ctx, f.actor, f.member.ID, plan.ID)
					must(t, err)
				} else {
					_, err = f.pool.Exec(ctx, `UPDATE employee_offboarding_plans SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, plan.ID)
					must(t, err)
				}
				if strings.HasSuffix(name, "successor") {
					target = f.successor.ID
				}
			case "deleted-draft-tombstone":
				draft, err := f.st.SaveMailDraft(ctx, f.sender, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "consumed identity tombstone"}})
				must(t, err)
				must(t, f.st.DeleteMailDraft(ctx, f.sender, draft.ID, draft.Revision))
				var drafts, receipts int
				must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM mail_drafts WHERE id=$1),(SELECT count(*) FROM draft_creation_receipts WHERE id=$1)`, draft.ID).Scan(&drafts, &receipts))
				if drafts != 0 || receipts != 1 {
					t.Fatal("formal draft deletion did not leave exactly its creation tombstone")
				}
			case "legacy-sealed-draft":
				// Independent guard witness: a pre-receipt/imported sealed draft.
				// Ordinary current SaveMailDraft also creates a protected receipt.
				_, err := f.pool.Exec(ctx, `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload,sealed_at) VALUES($1,$2,$3,$4,'{}',clock_timestamp())`, uuid.New(), f.tenant.ID, f.member.ID, f.shared.ID)
				must(t, err)
			case "attachment":
				_, err := f.st.ReserveMailAttachment(ctx, f.sender, company.Attachment{MailboxID: f.shared.ID, Filename: "historical.txt", Size: 3})
				must(t, err)
			case "outbound-user-only", "outbound-sender-only":
				job := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, MailFrom: f.shared.FullAddress, RcptTo: []string{"recipient@historical.test"}, State: models.OutboundSent}
				if name == "outbound-user-only" {
					job.UserID = &f.member.ID
				} else {
					job.SenderUserID = &f.member.ID
					job.SenderMailboxID = &f.shared.ID
				}
				must(t, f.st.CreateOutboundJob(ctx, job))
			}
			r5HistoricalMemberReject(t, f, target, store.ErrMemberHasHistoricalIdentity)
		})
	}
}

func TestR5MemberHistoricalDeleteTransferredSentProvenance(t *testing.T) {
	for _, cleaned := range []bool{false, true} {
		name := map[bool]string{false: "retained-explicit-sender", true: "archive-after-job-cleanup"}[cleaned]
		t.Run(name, func(t *testing.T) {
			f := r5HistoricalMemberSeed(t)
			ctx := context.Background()
			personal, err := f.st.CreateWorkMailbox(ctx, f.actor, company.MailboxInput{LocalPart: "departing", Kind: "personal", OwnerUserID: &f.member.ID})
			must(t, err)
			job := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.member.ID, SenderUserID: &f.member.ID, SenderMailboxID: &personal.ID, MailFrom: personal.FullAddress, To: []string{"recipient@historical.test"}, BCC: []string{"hidden@historical.test"}, RcptTo: []string{"recipient@historical.test", "hidden@historical.test"}, Subject: "immutable historical sent", TextBody: "synthetic retained sent content", State: models.OutboundSent}
			must(t, f.st.CreateOutboundJob(ctx, job))
			access, err := f.st.GetWorkMailbox(ctx, f.actor, personal.ID)
			must(t, err)
			must(t, f.st.TransferWorkMailbox(ctx, f.actor, personal.ID, f.successor.ID, access.Revision, "Mailbox moved, original send identity must not move"))
			if !cleaned {
				// This must be a historical-identity refusal, not ownership refusal.
				r5HistoricalMemberReject(t, f, f.member.ID, store.ErrMemberHasHistoricalIdentity)
				return
			}
			// Model the pre-existing legitimate queue retention boundary BEFORE
			// attempting member deletion. The archive deliberately has no job FK
			// and no original user UUID; do not fabricate it from the new owner.
			_, err = f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, job.ID)
			must(t, err)
			var before, after string
			query := `SELECT jsonb_build_object('asset',(SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1),'item',(SELECT to_jsonb(i) FROM sent_mail_items i WHERE asset_id=$1))::text`
			must(t, f.pool.QueryRow(ctx, query, job.ID).Scan(&before))
			must(t, f.st.DeleteUserGuarded(ctx, f.actor, f.tenant.ID, f.member.ID))
			must(t, f.pool.QueryRow(ctx, query, job.ID).Scan(&after))
			var owner uuid.UUID
			must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, personal.ID).Scan(&owner))
			if after != before || owner != f.successor.ID || !strings.Contains(after, job.TextBody) || !strings.Contains(after, "hidden@historical.test") {
				t.Fatal("member deletion changed immutable sent archive or current mailbox ownership")
			}
		})
	}
}

func TestR5MemberHistoricalDeleteUnreferencedSuccess(t *testing.T) {
	f := r5HistoricalMemberSeed(t)
	ctx := context.Background()
	r5HistoricalMemberCredentials(t, f, f.member.ID)
	var auditsBefore int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&auditsBefore))
	must(t, f.st.DeleteUserGuarded(ctx, f.actor, f.tenant.ID, f.member.ID))
	var users, keys, tokens, grants, audits, deleteAudits int
	must(t, f.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM users WHERE id=$1),
 (SELECT count(*) FROM tenant_api_keys WHERE owner_user_id=$1),
 (SELECT count(*) FROM refresh_tokens WHERE user_id=$1),
 (SELECT count(*) FROM mailbox_grants WHERE user_id=$1),
 (SELECT count(*) FROM audit_log),
 (SELECT count(*) FROM audit_log WHERE resource_id=$1 AND action='user.delete')`, f.member.ID).Scan(&users, &keys, &tokens, &grants, &audits, &deleteAudits))
	if users != 0 || keys != 0 || tokens != 0 || grants != 0 || audits != auditsBefore+1 || deleteAudits != 1 {
		t.Fatal("unreferenced member deletion did not atomically delete credentials/current ACL and audit once")
	}
}

func TestR5MemberHistoricalDeleteIndependentAttributionRetained(t *testing.T) {
	f := r5HistoricalMemberSeed(t)
	ctx := context.Background()
	template, err := f.st.SaveMailTemplate(ctx, f.actor, company.Template{Name: "Historical author snapshot", Draft: company.TemplateDraft{Subject: "retained", TextBody: "synthetic immutable template"}})
	must(t, err)
	_, err = f.st.PublishMailTemplate(ctx, f.actor, template.ID, template.Revision)
	must(t, err)
	_, err = f.st.InviteEmployee(ctx, f.actor, company.InvitationInput{Email: "new@contact.historical.test", LocalPart: "new", DisplayName: "New"}, company.Hash("test-only-historical-invitation"))
	must(t, err)
	_, err = f.st.PreviewOffboarding(ctx, f.actor, f.member.ID, f.successor.ID, company.OffboardingOptions{Drafts: "seal"}, "Keep independent creator attribution")
	must(t, err)
	must(t, f.st.SetTemplateGrant(ctx, f.actor, company.TemplateGrant{TemplateID: template.ID, MailboxID: f.shared.ID, UserID: f.member.ID}, true))
	// Administrator is only the non-FK author/inviter/grantor/plan creator;
	// it is not the departing/successor identity or a historical send principal.
	root := r5HistoricalMemberUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
	query := `SELECT jsonb_build_object(
 'templates',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM mail_templates r),
 'versions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM mail_template_versions r),
 'invitations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM employee_invitations r),
 'plans',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM employee_offboarding_plans r),
 'template_grants',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.template_id,r.mailbox_id,r.user_id) FROM mail_template_grants r),
 'old_audit',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM audit_log r WHERE action<>'user.delete'))::text`
	var before, after string
	must(t, f.pool.QueryRow(ctx, query).Scan(&before))
	must(t, f.st.DeleteUserGuarded(ctx, r5HistoricalMemberActor(root), f.tenant.ID, f.admin.ID))
	must(t, f.pool.QueryRow(ctx, query).Scan(&after))
	if after != before || !strings.Contains(after, f.admin.ID.String()) {
		t.Fatal("deletion changed independent historical creator attribution or audit snapshots")
	}
}

func TestR5MemberHistoricalDeleteFKFinalFenceRollback(t *testing.T) {
	for _, name := range []string{"immediate-delete-fence", "deferred-commit-fence"} {
		t.Run(name, func(t *testing.T) {
			f := r5HistoricalMemberSeed(t)
			ctx := context.Background()
			// Test-only future incoming parent, not named in preflight. Immediate
			// failure restores keys; deferred failure also rolls back the audit.
			ddl := `CREATE TABLE r5_identity_future_reference(user_id UUID REFERENCES users(id))`
			if name == "deferred-commit-fence" {
				ddl = `CREATE TABLE r5_identity_future_reference(user_id UUID REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED)`
			}
			_, err := f.pool.Exec(ctx, ddl)
			must(t, err)
			_, err = f.pool.Exec(ctx, `INSERT INTO r5_identity_future_reference VALUES($1)`, f.member.ID)
			must(t, err)
			r5HistoricalMemberReject(t, f, f.member.ID, store.ErrMemberHasHistoricalIdentity)
			var retained int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM r5_identity_future_reference WHERE user_id=$1`, f.member.ID).Scan(&retained))
			if retained != 1 {
				t.Fatal("FK rejection removed its evidence reference")
			}
		})
	}
}

func TestR5MemberHistoricalDeleteAuthorityAndMailboxGuards(t *testing.T) {
	for _, name := range []string{"self", "hierarchy", "last-admin", "owned-mailbox", "stale-actor-session", "frozen-actor", "foreign-target"} {
		t.Run(name, func(t *testing.T) {
			f := r5HistoricalMemberSeed(t)
			ctx := context.Background()
			target := f.member.ID
			var want error
			switch name {
			case "self":
				target = f.admin.ID
			case "hierarchy":
				u := r5HistoricalMemberUser(t, f, f.tenant.ID, models.RoleSuperAdmin)
				target = u.ID
			case "last-admin":
				rootTenant := &models.Tenant{Name: "External platform actor", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, rootTenant))
				root := r5HistoricalMemberUser(t, f, rootTenant.ID, models.RoleSuperAdmin)
				f.actor = r5HistoricalMemberActor(root)
				f.actor.TenantID = f.tenant.ID
				target, want = f.admin.ID, store.ErrLastAdministrator
			case "owned-mailbox":
				_, err := f.st.CreateWorkMailbox(ctx, f.actor, company.MailboxInput{LocalPart: "owned", Kind: "personal", OwnerUserID: &f.member.ID})
				must(t, err)
				want = store.ErrMemberOwnsMailbox
			case "stale-actor-session":
				must(t, f.st.UpdateUserPassword(ctx, f.admin.ID, "new-test-only-session"))
			case "frozen-actor":
				f.admin.IsActive = false
				must(t, f.st.UpdateUser(ctx, f.admin))
			case "foreign-target":
				other := &models.Tenant{Name: "Foreign target", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, other))
				target, want = r5HistoricalMemberUser(t, f, other.ID, models.RoleUser).ID, store.ErrMemberNotFound
			}
			before := r5HistoricalMemberState(t, f)
			err := f.st.DeleteUserGuarded(ctx, f.actor, f.tenant.ID, target)
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatalf("guard error=%v want=%v", err, want)
				}
			} else if !authz.IsAuthzError(err) {
				t.Fatalf("authority guard error=%v, want forbidden", err)
			}
			if r5HistoricalMemberState(t, f) != before {
				t.Fatal("existing authority/mailbox refusal gained side effects")
			}
		})
	}
}
