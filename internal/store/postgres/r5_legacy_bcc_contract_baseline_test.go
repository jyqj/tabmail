//go:build r5protocol

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
)

// This is an executable CURRENT-contract capability baseline, not a fabricated
// backfill command and not certification of future migration write idempotence.
// Original immutable assets and structured job provenance are actually queried.
func TestR5LegacyBCCContractCapabilities(t *testing.T) {
	executed := 0
	for _, c := range r5SharedCases(t) {
		if c.ID != "BC02" && c.ID != "BC03" {
			continue
		}
		executed++
		variants := []string{"trusted_structured_source"}
		if c.ID == "BC03" {
			variants = []string{"missing_source", "same_id_foreign_source"}
		}
		for _, variant := range variants {
			t.Run(c.ID+"/"+variant, func(t *testing.T) {
				f := seedCompany(t)
				ctx := context.Background()
				j := r5LegacyJob(t, f)
				h := r5SharedRouter(t, f)
				if variant != "trusted_structured_source" {
					_, e := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
					must(t, e)
				}
				if variant == "same_id_foreign_source" {
					foreign := &models.Tenant{Name: "BCC capability foreign source", PlanID: f.tenant.PlanID}
					must(t, f.st.CreateTenant(ctx, foreign))
					zone := &models.DomainZone{TenantID: foreign.ID, Domain: "foreign-capability.test", IsVerified: true, MXVerified: true}
					must(t, f.st.CreateZone(ctx, zone))
					forged := &models.OutboundJob{ID: j.ID, TenantID: foreign.ID, ZoneID: zone.ID, MailFrom: "foreign@foreign-capability.test", To: []string{"visible@foreign.test"}, BCC: []string{"FOREIGN_PRIVATE_BCC@foreign.test"}, RcptTo: []string{"visible@foreign.test", "FOREIGN_PRIVATE_BCC@foreign.test"}, State: models.OutboundSent}
					must(t, f.st.CreateOutboundJob(ctx, forged))
				}
				var exactSources int
				var columns []byte
				var before []byte
				var version int64
				must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM sent_mail_assets a JOIN outbound_jobs j ON j.id=a.id AND j.tenant_id=a.tenant_id AND j.zone_id=a.zone_id AND j.sender_mailbox_id=a.sender_mailbox_id WHERE a.id=$1`, j.ID).Scan(&exactSources))
				if (variant == "trusted_structured_source" && exactSources != 1) || (variant != "trusted_structured_source" && exactSources != 0) {
					t.Fatal("actual provenance premise missing")
				}
				must(t, f.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(column_name ORDER BY ordinal_position),'[]'::jsonb) FROM information_schema.columns WHERE table_schema='public' AND table_name='sent_mail_assets'`).Scan(&columns))
				must(t, f.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
				must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&before))
				config, e := pgx.ParseConfig(f.pool.Config().ConnString())
				must(t, e)
				// The CURRENT production migrator is invoked twice. On a current DB these
				// are real no-op starts; they cannot be called a future BCC-backfill replay.
				for i := 0; i < 2; i++ {
					must(t, postgres.Migrate(ctx, config))
				}
				var after []byte
				must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&after))
				if !bytes.Equal(before, after) {
					t.Fatal("current no-op migration startup changed immutable legacy values")
				}
				success := c
				success.Expected.Status = 200
				success.Expected.Wire.Code = ""
				response := r5Wire(t, h, r3Token(t, f.employee), "GET", "/api/v1/company/submissions/"+j.ID.String()+"/content", nil, success)
				if bytes.Contains(response.Body.Bytes(), []byte("FOREIGN_PRIVATE_BCC")) {
					t.Fatal("unprovable foreign job copied into another tenant asset")
				}
				if variant == "trusted_structured_source" {
					source, e := f.st.GetOutboundJob(ctx, j.ID)
					must(t, e)
					if source == nil || len(source.BCC) == 0 {
						t.Fatal("actual trustworthy structured BCC source not present")
					}
					for _, address := range source.BCC {
						if !bytes.Contains(after, []byte(address)) {
							t.Errorf("R5_PROTOCOL_CAPABILITY_TARGET_BC02: current schema/migration contract has structured trustworthy BCC but no durable recipient snapshot; future formal backfill remains absent; current_version=%d asset_columns=%s exact_source_count=%d", version, columns, exactSources)
						}
					}
				} else {
					var wire map[string]any
					must(t, json.Unmarshal(response.Body.Bytes(), &wire))
					if !bytes.Contains(response.Body.Bytes(), []byte("legacy_unknown")) {
						t.Errorf("R5_PROTOCOL_CAPABILITY_TARGET_BC03: actual missing/unprovable source read response lacks explicit legacy_unknown completeness; current_version=%d asset_columns=%s exact_source_count=%d", version, columns, exactSources)
					}
				}
				// Only schema names/version and source cardinality are reported, never
				// bodies, BCC values, tokens, production data, or private fixture headers.

			})
		}
	}
	if executed != 2 {
		t.Fatal(fmt.Sprintf("capability baseline case coverage drift: %d", executed))
	}
}
