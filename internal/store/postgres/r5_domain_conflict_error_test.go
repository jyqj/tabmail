package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/store"
	"testing"
)

// Actual pgconn error shapes exercise the adapter mapper; this is not a live
// PostgreSQL constraint/transaction test. CreateZone calls this same mapper.
func TestContinueDomainConflictPGClassification(t *testing.T) {
	for _, name := range []string{"nil", "opaque domain", "localized domain", "wrapped domain", "joined domain", "primary key", "tenant identity", "unknown constraint", "missing constraint", "foreign key", "message only", "cancelled unique", "ordinary"} {
		t.Run(name, func(t *testing.T) {
			var original error
			var pg *pgconn.PgError
			want := false
			switch name {
			case "nil":
			case "message only":
				original = errors.New("duplicate unique domain_zones_domain_key 23505")
			case "cancelled unique":
				original = fmt.Errorf("unique domain_zones_domain_key: %w", context.Canceled)
			case "ordinary":
				original = errors.New("storage unavailable")
			default:
				pg = &pgconn.PgError{Code: "23505", ConstraintName: "domain_zones_domain_key", Message: "opaque"}
				original = pg
				want = true
				switch name {
				case "localized domain":
					pg.Message = "域名值已存在"
				case "wrapped domain":
					original = fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", pg))
				case "joined domain":
					original = errors.Join(errors.New("contextual annotation"), pg)
				case "primary key":
					pg.ConstraintName = "domain_zones_pkey"
					want = false
				case "tenant identity":
					pg.ConstraintName = "domain_zones_tenant_identity"
					want = false
				case "unknown constraint":
					pg.ConstraintName = "another_unique_index"
					want = false
				case "missing constraint":
					pg.ConstraintName = ""
					want = false
				case "foreign key":
					pg.Code = "23503"
					want = false
				}
			}
			got := classifyZoneCreateError(original)
			if errors.Is(got, store.ErrDomainAlreadyExists) != want {
				t.Errorf("classification=%v want conflict=%v original=%v", got, want, original)
			}
			if original == nil {
				if got != nil {
					t.Errorf("nil changed: %v", got)
				}
				return
			}
			if !errors.Is(got, original) {
				t.Errorf("original error identity lost: %v", got)
			}
			if !want && got != original {
				t.Errorf("non-domain error rewritten: %v", got)
			}
			if pg != nil {
				var restored *pgconn.PgError
				if !errors.As(got, &restored) || restored != pg {
					t.Errorf("PgError cause lost: %v", got)
				}
			}
		})
	}
}
