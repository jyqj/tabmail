package domainapp

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"testing"
)

var domainQualificationPassed = errors.New("domain qualification passed")

type domainNoneConsumerStore struct{ *domainTestStore }

func (*domainNoneConsumerStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return nil, domainQualificationPassed
}

func TestDomainNoneConsumerRootQualification(t *testing.T) {
	for _, mode := range []string{"", "all", "list", "none", "unknown"} {
		for _, ids := range [][]uuid.UUID{nil, {}, {uuid.New()}} {
			for _, admin := range []bool{false, true} {
				tenant, user := &models.Tenant{ID: uuid.New()}, uuid.New()
				actor := authz.Actor{Type: authz.PrincipalUser, ID: user, TenantID: tenant.ID, IsAdmin: admin, Permission: &models.EffectivePermission{CanCreateDomains: true, DomainAccessMode: mode, AllowedZoneIDs: ids}}
				st := &domainNoneConsumerStore{newDomainTestStore()}
				svc := NewService(st, nil, "mx.example", nil, zerolog.Nop())
				_, err := svc.CreateZone(context.Background(), actor, tenant, "root.example")
				wantDenied := !admin && mode != "all" && (mode != "" || len(ids) > 0)
				if wantDenied {
					if e, ok := app.As(err); !ok || e.Kind != app.KindForbidden {
						t.Errorf("mode=%q ids=%v admin=%v should deny root, got %v", mode, ids, admin, err)
					}
				} else if !errors.Is(err, domainQualificationPassed) {
					t.Errorf("mode=%q ids=%v admin=%v qualified root got %v", mode, ids, admin, err)
				}
			}
		}
	}
}
