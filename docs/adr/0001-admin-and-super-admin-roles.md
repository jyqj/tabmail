# ADR 0001 — Admin and super-admin are two distinct roles

- Status: Accepted
- Date: 2026-06-13

## Context

TabMail is multi-tenant. Two privileged user roles exist (`models.UserRole`):

- `super_admin`
- `admin`

The earlier memory note "role=admin is global super-admin but the system is
multi-tenant" flagged this as an ambiguity. In practice the code already
implements a coherent **two-tier** model, but the boundary was never written
down and was re-derived inline as `actor.IsSuperAdmin || actor.IsAdmin` at ~20
call sites, which read as redundant and obscured which actions are genuinely
cross-tenant.

Ground truth in the code:

- `internal/api/middleware/auth.go`: `IsSuperAdmin(ctx)` is true **only** for
  `RoleSuperAdmin`; `IsAdmin(ctx)` is true for `RoleAdmin` **or**
  `RoleSuperAdmin`. So `IsAdmin ⊇ IsSuperAdmin`, and the compound
  `IsSuperAdmin || IsAdmin` is exactly `IsAdmin` — "admin within the tenant, or
  above."
- `internal/authz/authz.go` `Authorize`: `super_admin` bypasses tenant
  isolation entirely; `admin` has full access **within its tenant** but cannot
  manage other users (`ActionTenantUsersManage` requires super admin).
- Cross-tenant capabilities are guarded by `IsSuperAdmin` alone:
  `X-Tenant-ID` impersonation (`middleware/auth.go`), assigning
  `RoleSuperAdmin` (`handlers/auth.go`), and system-scoped permission profiles
  (`handlers/permissions.go`).

## Decision

Keep the two-tier model and name it.

- **`super_admin` is a global platform operator.** It acts across tenants:
  impersonation via `X-Tenant-ID`, system-wide configuration, role escalation.
  Expressed by `Actor.IsGlobalAdmin()` (≡ `IsSuperAdmin`).
- **`admin` is a tenant-scoped operator.** It has full privileged access within
  its own tenant but cannot manage tenant users or act on other tenants.
- The common privileged check — "is this actor an admin within its tenant
  (tenant admin or super admin)?" — is expressed by `Actor.IsTenantAdmin()`
  (≡ `IsSuperAdmin || IsAdmin`). `messageapp.Viewer.IsTenantAdmin()` mirrors it
  for the public/token viewer model.

The duplicated inline `IsSuperAdmin || IsAdmin` / `!IsSuperAdmin && !IsAdmin`
checks are replaced by these named predicates. This is a pure rename — behavior
is unchanged because `IsAdmin` already includes `IsSuperAdmin`.

## Alternatives considered

- **Collapse to a single `IsAdmin`.** Rejected: it would erase the cross-tenant
  vs tenant-scoped distinction that impersonation, role escalation, and
  system-profile scope depend on.
- **Move every list/read check into `authz.Authorize`.** Rejected for now: many
  list paths have no loaded `Resource` to authorize against; this is a larger
  refactor with no behavior change, out of scope here.

## Consequences

- Cross-tenant authority has one name (`IsGlobalAdmin`) and one documented
  meaning; reviewers can grep for it to audit the cross-tenant surface.
- The tenant-admin boundary has one name (`IsTenantAdmin`) instead of ~20 inline
  compound expressions.
- This ADR does not change who can do what. A future change to the role model
  (e.g. a dedicated tenant-owner role) should update this ADR.
