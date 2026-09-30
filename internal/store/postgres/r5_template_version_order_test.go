package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
)

// The registered CompanyTemplateHandler Publish/RevokeTemplateVersion routes
// call these complete commands. Publish takes T UPDATE before template/version;
// revoke takes T KEY SHARE before version/template. Opposite downstream names
// are not evidence of a cycle: observe the real parent serialization first.
func TestR5TemplatePublishRevokeCompleteCommandsParentOrder(t *testing.T) {
	for _, first := range []string{"publish-first", "revoke-first"} {
		t.Run(first, func(t *testing.T) {
			f := seedCompany(t)
			tpl, v1 := r5VersionOrderFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			before := r5VersionOrderState(t, f, tpl.ID)
			immutable := r5VersionOrderImmutable(t, f, v1.ID)
			action := "template.publish"
			if first == "revoke-first" {
				action = "template.version.revoke"
			}
			name := "r5_version_gate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			key := "r5-version-gate:" + tpl.ID.String()
			_, err := f.pool.Exec(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.resource_id='`+tpl.ID.String()+`'::uuid AND NEW.action='`+action+`' THEN PERFORM pg_advisory_xact_lock(hashtextextended('`+key+`',0)); END IF; RETURN NEW; END $$;
 CREATE TRIGGER `+name+` BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
			must(t, err)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key)
			must(t, err)
			type published struct {
				v   *company.TemplateVersion
				err error
			}
			pubDone := make(chan published, 1)
			revokeDone := make(chan error, 1)
			publish := func() { v, e := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision); pubDone <- published{v, e} }
			revoke := func() { revokeDone <- f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v1.Version, tpl.Revision) }
			if first == "publish-first" {
				go publish()
			} else {
				go revoke()
			}
			firstPID := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			if first == "publish-first" {
				go revoke()
			} else {
				go publish()
			}
			secondPID := r5WaitBlockedBy(t, f, ctx, firstPID, "FROM tenants")
			r5MaintenanceTrace(t, f, ctx, hold.Conn().PgConn().PID(), firstPID, secondPID)
			must(t, hold.Rollback(ctx))
			var publication published
			select {
			case publication = <-pubDone:
			case <-ctx.Done():
				t.Fatal("publish terminal missing; timeout is not lock-cycle evidence")
			}
			revokeErr := r5ConcurrentResult(t, ctx, revokeDone)
			want := before
			want[0]++ // exactly one successful template revision transition
			want[3]++ // one required audit
			want[4]++ // one company change outbox event
			if first == "publish-first" {
				must(t, publication.err)
				r5VersionOrderConflict(t, revokeErr)
				want[1]++
				if publication.v == nil || publication.v.Version != v1.Version+1 || publication.v.ContentHash != company.Digest(tpl.Draft) {
					t.Fatal("successful publish receipt does not match committed immutable version")
				}
				if r5VersionOrderImmutable(t, f, publication.v.ID) == "" {
					t.Fatal("publish receipt points to no committed version")
				}
			} else {
				must(t, revokeErr)
				r5VersionOrderConflict(t, publication.err)
				want[2]++
			}
			if got := r5VersionOrderState(t, f, tpl.ID); got != want {
				t.Fatalf("revision/version/revocation/audit/outbox/event atomicity: got=%v want=%v", got, want)
			}
			if r5VersionOrderImmutable(t, f, v1.ID) != immutable {
				t.Fatal("publish/revoke rewrote preexisting immutable version content")
			}
		})
	}
}

func TestR5TemplateVersionRequiredAuditFailureRollsBack(t *testing.T) {
	for _, operation := range []string{"publish", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			f := seedCompany(t)
			tpl, v1 := r5VersionOrderFixture(t, f)
			ctx := context.Background()
			before := r5AuditedSnapshot(t, f)
			action := "template.publish"
			if operation == "revoke" {
				action = "template.version.revoke"
			}
			_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_version_audit_failure CHECK (NOT(resource_id='`+tpl.ID.String()+`'::uuid AND action='`+action+`')) NOT VALID`)
			must(t, err)
			if operation == "publish" {
				_, err = f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
			} else {
				err = f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v1.Version, tpl.Revision)
			}
			if err == nil {
				t.Fatal("required audit failure was ignored")
			}
			if r5AuditedSnapshot(t, f) != before {
				t.Fatal("audit victim retained template revision/version/revoke/outbox effects")
			}
			_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_version_audit_failure`)
			must(t, err)
			if operation == "publish" {
				_, err = f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
			} else {
				err = f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v1.Version, tpl.Revision)
			}
			must(t, err)
		})
	}
}

func TestR5TemplateVersionAlreadyRevokedIsNoOp(t *testing.T) {
	f := seedCompany(t)
	tpl, v1 := r5VersionOrderFixture(t, f)
	ctx := context.Background()
	must(t, f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v1.Version, tpl.Revision))
	before := r5AuditedSnapshot(t, f)
	// A consumed old revision must not prevent confirming an existing revoke.
	must(t, f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v1.Version, tpl.Revision))
	if r5AuditedSnapshot(t, f) != before {
		t.Fatal("idempotent revoke rewrote revision, immutable content, audit or outbox")
	}
}

func r5VersionOrderFixture(t *testing.T, f *companyFixture) (*company.Template, *company.TemplateVersion) {
	t.Helper()
	ctx := context.Background()
	tpl, err := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "R5 version order " + uuid.NewString(), Draft: templateDraft()})
	must(t, err)
	v, err := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
	must(t, err)
	tpl.Revision++
	return tpl, v
}

func r5VersionOrderState(t *testing.T, f *companyFixture, id uuid.UUID) [6]int {
	t.Helper()
	var state [6]int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT revision FROM mail_templates WHERE id=$1),(SELECT count(*) FROM mail_template_versions WHERE template_id=$1),(SELECT count(*) FROM mail_template_versions WHERE template_id=$1 AND revoked_at IS NOT NULL),(SELECT count(*) FROM audit_log),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM mailbox_event_log)`, id).Scan(&state[0], &state[1], &state[2], &state[3], &state[4], &state[5]))
	return state
}

func r5VersionOrderImmutable(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var content string
	must(t, f.pool.QueryRow(context.Background(), `SELECT (to_jsonb(v)-'revoked_at')::text FROM mail_template_versions v WHERE id=$1`, id).Scan(&content))
	return content
}

func r5VersionOrderConflict(t *testing.T, err error) {
	t.Helper()
	e, ok := app.As(app.FromAuthz(err))
	if !ok || e.Kind != app.KindConflict {
		t.Fatalf("stale revision should conflict without partial effects: SQLSTATE=%s err=%v", r5SQLState(err), err)
	}
}
