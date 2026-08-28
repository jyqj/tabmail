# Authorization and asset boundaries

This document records the security invariants enforced by TabMail's current
modular-monolith architecture. Changes to an HTTP route, application service,
repository, or frontend affordance must preserve these invariants and add a
regression test at the application boundary.

## Principals and credentials

A principal is the actor whose authority is evaluated. A credential only
narrows or proves that authority.

| Principal / credential | Tenant scope | Primary use |
| --- | --- | --- |
| `super_admin` JWT | Explicitly selected tenant for tenant data | Platform operations and audited tenant impersonation |
| `admin` JWT | Own tenant | Tenant configuration and ordinary-member administration |
| `user` JWT | Own tenant | Owned/allowed assets and granted capabilities |
| tenant API key | Key tenant, optional owner and zone allowlist | Integrations; scopes are a ceiling, never an expansion |
| mailbox token | One mailbox | Capability-style mailbox access |
| public request | Public projection only | Public resources and public mailboxes |

## Asset hierarchy

```text
tenant
├── domain zone
│   ├── route
│   ├── mailbox
│   │   └── message / raw MIME
│   └── send identity
├── tenant API key
├── webhook endpoint
└── outbound job / delivery attempts
```

Every tenant-scoped read must begin with a tenant-scoped repository whenever a
tenant context exists. Global lookups are reserved for public projections,
SMTP-time address resolution, and explicit platform operations.

Every user-visible asset mutation is a Unit of Work: aggregate state, its audit
obligation, and its durable outbox event commit together. See
[`unit-of-work-and-outbox.md`](unit-of-work-and-outbox.md).

## Mandatory invariants

### Tenant isolation

* A tenant administrator never gains authority over another tenant merely from
  knowing a resource identifier or mailbox address.
* A platform administrator must select the target tenant before protected
  tenant data is returned or mutated.
* A global mailbox fallback may expose only a mailbox whose access mode is
  explicitly `public`; protected cross-tenant mailboxes return `NOT_FOUND`.
* Message writes always use the tenant-scoped repository.

### Member administration

* A tenant administrator may manage ordinary members in the selected tenant.
* A tenant administrator cannot manage peer administrators, create an
  administrator role, deactivate itself, change its own role, or delete itself.
* A platform administrator may manage administrators only inside the explicitly
  selected tenant.

The target-aware rule lives in `authz.CanManageTenantMember`. The coarse
`tenant.users.manage` action remains platform-only because it has no target
role with which to constrain a tenant administrator.

### Mailbox and message content

* Public mailbox content remains public regardless of the viewer's unrelated
  authenticated tenant.
* Administrators of the mailbox's tenant receive metadata but use audited
  break-glass access for message body and raw source.
* Sanitizer failure drops HTML and reports `body_access=sanitize_failed`; raw
  untrusted HTML is never returned as a fallback.

### Outbound sending

`SendIdentity` is the authoritative send-as asset.

A submission is accepted only when all of the following hold:

1. envelope addresses are parsed and canonicalized;
2. the sender domain belongs to the selected tenant and passes TXT/MX checks;
3. the caller has `send.from` authority and the zone is in scope;
4. an exact or domain-wildcard `SendIdentity` matches the canonical sender;
5. the identity is verified;
6. recipient suppression and quota checks pass.

A mailbox or inbound route does not implicitly grant outbound authority.
Creating or deleting send identities is an administrative operation.

### Webhook delivery

* Outbox fanout combines deployment-level targets with active tenant endpoints.
* Tenant endpoint `event_types` are enforced before delivery rows are created.
* Tenant endpoint secrets are resolved at delivery time, so rotations take
  effect immediately; removal or deactivation fails queued delivery closed.
* Tenant-managed URLs require HTTPS, do not follow redirects, resolve only to
  public addresses, and dial the validated address directly. This prevents
  private-network SSRF and DNS rebinding between validation and connection.
* Deployment-level static endpoints retain their configured legacy behavior.

## Review checklist

Before merging an authorization-sensitive change, reviewers should verify:

1. Is the actor's selected tenant explicit?
2. Does the query enforce tenant scope before loading the row?
3. Is the target asset and action explicit?
4. Can an API key only narrow its owner's current authority?
5. Are public metadata, content, mutation, execution, delegation, and transfer
   treated as distinct actions?
6. Are denial responses information-hiding where cross-tenant identifiers are
   involved?
7. Is the decision covered by both an allow and a deny regression test?
8. Are audit and outbox obligations preserved?
