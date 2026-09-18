package postgres

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// submissionScope must keep the tenant predicate as an independent conjunct
// for every principal branch: a branch that replaces it drops the top-level
// tenant bound and re-exposes rows other tenants tagged with this principal's
// user / mailbox / API-key ids. argBase must also stay correct because
// GetSubmission prepends s.id=$1 and shifts every scope argument by one.
func TestSubmissionScopeKeepsTenantConjunct(t *testing.T) {
	tenant := uuid.New()
	userID := uuid.New()
	keyID := uuid.New()

	user := authz.Actor{Type: authz.PrincipalUser, ID: userID, TenantID: tenant, Role: models.RoleUser}

	// ListSubmissions: argBase=1, tenant is $1, the user dimension is $2.
	where, args := submissionScope(user, 1)
	if len(args) != 2 || args[0] != tenant || args[1] != userID {
		t.Fatalf("user branch args wrong: %v", args)
	}
	if !strings.HasPrefix(where, "s.tenant_id=$1 AND ") {
		t.Fatalf("tenant conjunct lost in user branch: %s", where)
	}
	if !strings.Contains(where, "s.user_id=$2") || !strings.Contains(where, "s.sender_user_id=$2") {
		t.Fatalf("user dimension placeholders wrong: %s", where)
	}
	if !strings.Contains(where, "m.tenant_id=$1") {
		t.Fatalf("mailbox subquery tenant bound wrong: %s", where)
	}

	// GetSubmission: argBase=2 — s.id=$1 is the caller's own placeholder, the
	// scope starts at $2 and must not collapse onto it.
	where, args = submissionScope(user, 2)
	if len(args) != 2 || args[0] != tenant || args[1] != userID {
		t.Fatalf("argBase=2 args wrong: %v", args)
	}
	if !strings.HasPrefix(where, "s.tenant_id=$2 AND ") || !strings.Contains(where, "s.user_id=$3") {
		t.Fatalf("argBase=2 placeholders wrong: %s", where)
	}

	// Ownerless (tenant-wide) API key: both the tenant conjunct and the
	// api_key dimension must be present as separate conjuncts.
	key := authz.Actor{Type: authz.PrincipalAPIKey, ID: keyID, TenantID: tenant, TenantWide: true}
	where, args = submissionScope(key, 1)
	if len(args) != 2 || args[0] != tenant || args[1] != keyID {
		t.Fatalf("api key branch args wrong: %v", args)
	}
	if !strings.HasPrefix(where, "s.tenant_id=$1 AND ") || !strings.Contains(where, "s.api_key_id=$2") {
		t.Fatalf("api key branch predicate wrong: %s", where)
	}

	// Unknown principal: tenant bound stays, visibility stays empty.
	where, args = submissionScope(authz.Actor{TenantID: tenant}, 1)
	if where != "s.tenant_id=$1 AND FALSE" || len(args) != 1 {
		t.Fatalf("unknown principal predicate wrong: %s %v", where, args)
	}

	// Zone allowlist appends after the identity dimension.
	zone := uuid.New()
	restricted := user
	restricted.Permission = &models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{zone}}
	where, args = submissionScope(restricted, 1)
	if len(args) != 3 || !strings.Contains(where, "s.zone_id=ANY($3)") {
		t.Fatalf("zone allowlist predicate wrong: %s %v", where, args)
	}
}

func TestSubmissionContentScopeNeverUsesHistoricalAuthorship(t *testing.T) {
	tenant, user := uuid.New(), uuid.New()
	actors := []authz.Actor{
		{Type: authz.PrincipalUser, ID: user, TenantID: tenant, Role: models.RoleUser},
		{Type: authz.PrincipalUser, ID: user, TenantID: tenant, Role: models.RoleSuperAdmin, IsSuperAdmin: true},
		{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, OwnerUserID: &user},
	}
	for _, a := range actors {
		for _, base := range []int{1, 2, 3} {
			where, args := submissionContentScope(a, base)
			if len(args) != 2 || args[0] != tenant || args[1] != user {
				t.Fatalf("scope arguments: %v", args)
			}
			if strings.Contains(where, "s.user_id=") || strings.Contains(where, "s.sender_user_id=") || !strings.Contains(where, "g.can_read") || !strings.Contains(where, "m.expires_at") {
				t.Fatalf("historical authorship bypass: %s", where)
			}
		}
	}
	for _, a := range []authz.Actor{{TenantID: tenant}, {Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: tenant, TenantWide: true}} {
		where, args := submissionContentScope(a, 1)
		if where != "s.tenant_id=$1 AND FALSE" || len(args) != 1 {
			t.Fatalf("principal without user gained content: %s", where)
		}
	}
}
