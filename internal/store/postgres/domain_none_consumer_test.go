package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"testing"
)

func TestDomainNoneConsumerSubmissionScope(t *testing.T) {
	for _, mode := range []string{"", "all", "none", "list"} {
		var p models.EffectivePermission
		if err := json.Unmarshal([]byte(`{"domain_access_mode":"`+mode+`","allowed_zone_ids":[]}`), &p); err != nil {
			t.Fatal(err)
		}
		a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New(), Permission: &p}
		for _, content := range []bool{false, true} {
			for _, base := range []int{1, 2, 3} {
				where, args := submissionScopeFor(a, base, !content)
				restricted := mode == "none" || mode == "list"
				if !strings.HasPrefix(where, "s.tenant_id=") || strings.Contains(where, "s.zone_id=ANY(") != restricted || len(args) != 2+btoiDomain(restricted) {
					t.Errorf("mode=%q content=%v base=%d SQL=%s args=%v", mode, content, base, where, args)
				}
			}
		}
	}
}
func btoiDomain(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Query capture stops before scanning: this exercises the exact production
// receipt statement without connecting to PostgreSQL or weakening predicates.
type domainReceiptCaptureTx struct {
	pgx.Tx
	sql  string
	args []any
}

var domainReceiptCaptureStop = errors.New("domain receipt query captured")

func (tx *domainReceiptCaptureTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.sql, tx.args = sql, args
	return nil, domainReceiptCaptureStop
}
func TestDomainNoneConsumerReceiptEnvelope(t *testing.T) {
	for _, observedNone := range []bool{false, true} {
		var none models.EffectivePermission
		if err := json.Unmarshal([]byte(`{"domain_access_mode":"none"}`), &none); err != nil {
			t.Fatal(err)
		}
		// Old zone data alongside none must not become an allowlist.
		none.AllowedZoneIDs = []uuid.UUID{uuid.New()}
		a := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: uuid.New()}
		observed, current := a, a
		if observedNone {
			observed.Permission = &none
		} else {
			current.Permission = &none
		}
		tx := &domainReceiptCaptureTx{}
		_, _, err := readOutboundReceipts(context.Background(), tx, observed, current, nil, nil, models.Page{Page: 1, PerPage: 10})
		if !errors.Is(err, domainReceiptCaptureStop) {
			t.Fatal(err)
		}
		// A profile restriction must gate the metadata-visible CTE independently
		// of whether the submitter owns the historical job or can read its mailbox.
		if !strings.Contains(tx.sql, "j.tenant_id=$1 AND j.zone_id=ANY(") {
			t.Errorf("observedNone=%v missing credential bound: %s", observedNone, tx.sql)
		}
		arrays := 0
		for _, arg := range tx.args {
			if ids, ok := arg.([]uuid.UUID); ok {
				arrays++
				if len(ids) != 0 {
					t.Errorf("none receipt retained stale ids: %v", ids)
				}
			}
		}
		if arrays == 0 {
			t.Fatal("none receipt omitted deny-all bind")
		}

	}
}

func TestDomainNoneConsumerOutboundScope(t *testing.T) {
	tenant, user, zone := uuid.New(), uuid.New(), uuid.New()
	for _, mode := range []string{"", "all", "none", "list", "unknown"} {
		for _, ids := range [][]uuid.UUID{nil, {}, {zone}} {
			var p models.EffectivePermission
			if err := json.Unmarshal([]byte(`{"domain_access_mode":"`+mode+`"}`), &p); err != nil {
				t.Fatal(err)
			}
			p.AllowedZoneIDs = ids
			a := authz.Actor{Type: authz.PrincipalUser, ID: user, TenantID: tenant, Permission: &p}
			f := authz.OwnerListScope(a, tenant)
			f.ReaderUserID = &user
			sql, args, ok := outboundJobsScopeSQL(f)
			restricted := mode != "all" && (mode != "" || len(ids) > 0)
			if !ok || !strings.HasPrefix(sql, "tenant_id=$1 AND ") || strings.Contains(sql, "zone_id=ANY(") != restricted || len(args) != 3+btoiDomain(restricted) {
				t.Errorf("mode=%q ids=%v SQL=%s args=%v", mode, ids, sql, args)
			}
			if restricted {
				bound, ok := args[len(args)-1].([]uuid.UUID)
				if !ok {
					t.Fatalf("wrong zone bind %T", args[len(args)-1])
				}
				if (mode == "none" || mode == "unknown" || len(ids) == 0) && len(bound) != 0 {
					t.Errorf("denied scope retained zone bind: %v", bound)
				}
			}
		}
	}
	// Direct legacy constructor still treats an untagged nil/empty array as all.
	for _, ids := range [][]uuid.UUID{nil, {}} {
		sql, _, ok := outboundJobsScopeSQL(authz.OwnerListFilter{TenantID: tenant, UserID: &user, AllowedZoneIDs: ids})
		if !ok || strings.Contains(sql, "zone_id=ANY(") {
			t.Errorf("legacy changed: %s", sql)
		}
	}
}
