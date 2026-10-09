package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"tabmail/internal/app/admin"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

// Router registers AdminHandler.DeleteTenant under RequireSuperAdmin. The
// handler delegates to this service; do not replace the service with a raw
// DELETE while claiming its post-command audit behavior was exercised.
func r5TenantDeleteService(f *companyFixture) *adminapp.Service {
	return adminapp.NewService(f.st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
}

func TestR5TenantDeleteNormalCatalogControls(t *testing.T) {
	for _, kind := range []string{"empty", "message-document-index", "published-template"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "empty" {
				st, pool, _ := testpg.NewPostgres(t)
				ctx := context.Background()
				tenant := &models.Tenant{Name: "R5 empty deletion", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}
				must(t, st.CreateTenant(ctx, tenant))
				f := &companyFixture{st: st, pool: pool, tenant: tenant}
				must(t, r5TenantDeleteService(f).DeleteTenant(ctx, tenant.ID, "super_admin:test-only"))
				r5TenantDeleteAbsent(t, f)
				return
			}
			f := seedCompany(t)
			var source uuid.UUID
			var key string
			if kind == "message-document-index" {
				_, doc := r5IndexFixture(t, f, true)
				source, key = doc.MessageID, doc.SourceKey
			} else {
				r5VersionOrderFixture(t, f)
			}
			before := r5TenantDeleteSnapshot(t, f)
			ctx := context.Background()
			err := r5TenantDeleteService(f).DeleteTenant(ctx, f.tenant.ID, "super_admin:test-only")
			if err != nil {
				description := r5TenantDeleteConstraint(t, f, err)
				if r5TenantDeleteSnapshot(t, f) != before {
					t.Fatal("normal constraint refusal retained cascade prefix or an audit receipt")
				}
				if key != "" {
					r5RestoreReferences(t, f, ctx, key, 1)
					if r5MaintenanceState(t, f, source)[0] != 1 {
						t.Fatal("constraint refusal lost source")
					}
				}
				t.Logf("normal path rejected by actual final catalog, not a concurrency failure: %s", description)
				return
			}
			r5TenantDeleteAbsent(t, f)
			if key != "" {
				r5RestoreReferences(t, f, ctx, key, 0)
			}
		})
	}
}

func r5TenantDeleteConstraint(t *testing.T, f *companyFixture, err error) string {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23503" || pg.ConstraintName == "" {
		t.Fatalf("unexpected normal delete error, not a catalog constraint classification: SQLSTATE=%s err=%v", r5SQLState(err), err)
	}
	var definition string
	must(t, f.pool.QueryRow(context.Background(), `SELECT conrelid::regclass::text||' -> '||confrelid::regclass::text||': '||pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1 AND connamespace='public'::regnamespace`, pg.ConstraintName).Scan(&definition))
	return "SQLSTATE=" + pg.Code + " constraint=" + pg.ConstraintName + " " + definition
}

func r5TenantDeleteSnapshot(t *testing.T, f *companyFixture) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('tenant',(SELECT to_jsonb(t) FROM tenants t WHERE id=$1),'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE tenant_id=$1),'settings',(SELECT to_jsonb(c) FROM company_settings c WHERE tenant_id=$1),'zones',(SELECT jsonb_agg(to_jsonb(z) ORDER BY id) FROM domain_zones z WHERE tenant_id=$1),'invitations',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM employee_invitations i WHERE tenant_id=$1),'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY mailbox_id,user_id) FROM mailbox_grants g WHERE tenant_id=$1),'mailboxes',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM mailboxes m WHERE tenant_id=$1),'messages',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM messages m WHERE tenant_id=$1),'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY message_id) FROM mail_documents d WHERE tenant_id=$1),'index',(SELECT jsonb_agg(to_jsonb(j) ORDER BY message_id) FROM mail_index_jobs j WHERE tenant_id=$1),'templates',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM mail_templates t WHERE tenant_id=$1),'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM mail_template_versions v WHERE tenant_id=$1),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_log a),'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM outbox_events e),'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY sequence) FROM mailbox_event_log e WHERE tenant_id=$1))::text`, f.tenant.ID).Scan(&state))
	return state
}

func r5TenantDeleteAbsent(t *testing.T, f *companyFixture) {
	t.Helper()
	var remains, audits int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM tenants WHERE id=$1)+(SELECT count(*) FROM users WHERE tenant_id=$1)+(SELECT count(*) FROM mailboxes WHERE tenant_id=$1)+(SELECT count(*) FROM messages WHERE tenant_id=$1)+(SELECT count(*) FROM mail_documents WHERE tenant_id=$1)+(SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1)+(SELECT count(*) FROM mail_templates WHERE tenant_id=$1)+(SELECT count(*) FROM mail_template_versions WHERE tenant_id=$1)+(SELECT count(*) FROM mailbox_event_log WHERE tenant_id=$1),(SELECT count(*) FROM audit_log WHERE action='tenant.delete' AND resource_id=$1 AND tenant_id IS NULL)`, f.tenant.ID).Scan(&remains, &audits))
	if remains != 0 || audits != 1 {
		t.Fatalf("successful tenant deletion left dependent rows or wrong service audit receipt: remains=%d audits=%d", remains, audits)
	}
}
