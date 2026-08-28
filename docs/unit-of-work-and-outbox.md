# Unit of Work and transactional outbox

TabMail is a modular monolith, but one HTTP or SMTP use case often updates
several persistence concerns:

```text
asset state + audit obligation + integration event
```

Those writes are one business outcome. They must either all commit or all roll
back. This document defines the transaction boundary introduced for that
invariant.

## The invariant

For a user-visible asset mutation, success means all of the following are
durable:

1. the asset row or rows were changed;
2. the security audit entry was inserted;
3. the domain event was inserted into `outbox_events`.

A request must not return success when only a prefix of that list committed.
The webhook HTTP request is deliberately **not** part of the database
transaction. A worker expands and delivers the committed outbox event later.

```text
request
  │
  ▼
application service
  │
  ├─ authorize and validate
  │
  └─ store.WithinTx(txCtx)
       ├─ mutate aggregate
       ├─ insert audit_log
       └─ insert outbox_events
             │
             └── COMMIT
                    │
                    ▼
              webhook worker
```

## Application API

Application services use `app.CommitMutation`:

```go
err := app.CommitMutation(
    ctx,
    service.store,      // store.Transactor
    service.store,      // app.AuditStore
    service.dispatcher, // app.EventEnqueuer
    audit,
    &event,
    func(txCtx context.Context) error {
        return service.store.UpdateZone(txCtx, zone)
    },
)
```

The callback must use `txCtx` for every repository operation. Reusing the outer
`ctx` leaves the Unit of Work and is a correctness bug.

`app.InsertAuditRequired` is fail-closed. It is used by transactional mutations
and break-glass reads. `app.InsertAudit` remains only for explicitly
best-effort operational telemetry.

## Repository implementation

`postgres.PgStore.WithinTx` carries a `pgx.Tx` in the callback context.
Repository methods call `s.db(ctx)`, which resolves either that transaction or
the normal connection pool. The application layer never imports pgx or handles
SQL transactions directly.

Nested Units of Work are supported. pgx implements a nested transaction as a
savepoint, so a lower-level repository method may create an internal critical
section without committing the outer application transaction.

The repository test `TestRepositoryMethodsDoNotBypassTransactionContext`
rejects direct `s.pool.Exec`, `Query`, `QueryRow`, or `Begin` calls outside the
PostgreSQL bootstrap file. Tenant-scoped views also resolve the executor from
the supplied context.

## Covered aggregates

The transaction boundary currently covers these user-visible mutations:

- tenants, plans, overrides and API keys;
- system settings and SMTP policy;
- domain zones, verification state and routes;
- mailboxes and destructive message operations;
- send identities;
- tenant webhook endpoints;
- permission profiles and user overrides;
- registration, member changes, password changes and session revocation;
- admin invitation creation and acceptance;
- outbound submission;
- inbound message metadata plus its `message.received` outbox event.

Domain creation treats its wildcard send identity as part of the same aggregate.
Invitation acceptance atomically claims the invitation before creating the
administrator tenant, user and refresh token.

## Deliberate exceptions

Not every write is a business asset mutation.

Queue leases, retry counters, heartbeat timestamps, `last_used_at`, message
`seen` state and delivery-attempt bookkeeping are operational state. They do
not emit a second domain event for every transition. Their own repository
methods still join an existing transaction when called with a transaction
context.

Object storage cannot join a PostgreSQL transaction. The adopted pattern is:

- persist or stage the object before the database mutation when required;
- commit database references, audit and outbox atomically;
- perform destructive object cleanup after commit;
- use reference counting and reconciliation to remove orphaned objects.

Realtime SSE publication is also post-commit. Clients must never observe a
message or deletion before the database commit succeeds.

## Refresh-token rotation

Refresh tokens are consumed with one conditional statement:

```sql
UPDATE refresh_tokens
SET revoked_at = now()
WHERE id = $1
  AND revoked_at IS NULL
  AND expires_at > now()
RETURNING revoked_at;
```

Exactly one concurrent refresh can consume a token. A replay or concurrent
loser revokes the user's entire refresh-token family and records a
`session.refresh_reuse` audit entry in the same transaction.

## Failure behavior

| Failure | Result |
| --- | --- |
| validation or authorization | no transaction is opened |
| aggregate mutation fails | rollback; no audit or outbox row |
| audit insert fails | rollback aggregate and outbox |
| outbox insert fails | rollback aggregate and audit |
| commit fails | request fails; success is never reported |
| webhook delivery fails later | aggregate remains committed; delivery retries |
| post-commit object deletion fails | warning plus later reconciliation |

Application errors such as `Forbidden` and `Conflict` keep their public error
kind through the transaction helper. Infrastructure failures are converted to
the stable internal-error envelope.

## Review checklist

For every new mutation:

1. Is the use case implemented in an application service rather than a handler?
2. Is the aggregate mutation inside `CommitMutation` or `WithinTransaction`?
3. Does every repository call inside the callback receive `txCtx`?
4. Is the resource ID allocated before the audit and event are constructed?
5. Does the audit omit credentials, message bodies and private keys?
6. Does the event contain a tenant ID when it belongs to a tenant?
7. Are externally visible side effects delayed until after commit?
8. Is rollback covered by a database-backed test?
9. Does a concurrency-sensitive claim use a conditional SQL update rather than
   a read-then-write sequence?
10. If the mutation is intentionally exempt, is the operational-state reason
    documented in code?
