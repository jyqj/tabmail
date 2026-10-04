# PE05 fixture owner join — 2026-10-04

Base `6e045457cd53744660bf5531d0967ab9bf068363`. Narrow test-infrastructure repair only.

`r5UIGrantBarrier` returns the package-shared `r5UIBarrierOwner` interface:
`ownerClose() error`, `ownerDone() <-chan struct{}`, `ownerResult() error`.
Close is idempotent and synchronous: cancel, release the held transaction once,
then await coordinator completion. The coordinator releases its transaction and
joins the separately tracked revoker before publishing immutable result/done.
Query, rollback, timeout/cancellation and revoker failures are retained with
`errors.Join`; cleanup reports failures rather than timing out silently. Polling
retries only `pgx.ErrNoRows`, not arbitrary database failures. A mutex serializes
transaction setup and rollback. No join waits inside the close `sync.Once`.

Batch owner handoff: call the existing barrier after fixture creation, retain
its returned interface, and require a nil `ownerClose`/`ownerResult` before
acknowledging fixture completion. The barrier registers its own cleanup after
seed, so testing's LIFO order joins it before HTTP server/PG pool cleanup. Done
alone indicates termination; a nonnil result still rejects the case.

Focused race validation passed: four top-level tests and eight subtests, including
six controlled synctest lifecycle phases, release-before-join, autonomous query
failure retaining the child failure, and two concurrent isolated loopback PG/HTTP
owners. Each real owner proves first save 200, completed revocation, next save
403, immediate NOWAIT lock reacquisition, cancel/release/join, and LIFO ordering.
Database, tenant and HTTP endpoint identities differ. Safe log/source hashes are
in the [receipt](evidence/R5-FIXTURE-OWNER-JOIN-20261004.json); logs stay private.

Earlier failures are preserved in the receipt. The first synctest run timed out
because a second close waited on a mutex that synctest does not regard as durable
blocking. Moving the join outside Once fixed it. The first PG cancellation probe
hit NOWAIT failure then leaked its own test transaction on Fatal, causing a pool
cleanup timeout; immediate rollback defers fixed the test leak. A subsequent
probe retained SQLSTATE 55P03: pgx cancellation can return before the backend has
released a query lock. The final controlled child deliberately ignores context
cancellation and instead waits for held-transaction release, then completes the
real SQL operation before join. This is proof of release/join ordering, not new
qualification of pgx server-side cancellation behavior or of an external batch.

Production API/store/authorization conditions, fixed catalog/markers/RC assertions,
launcher/helper/cache, two fork replaces and existing 20/120/180/75 budgets remain
unchanged. No formal 46-case producer, TSX run or wholePG180 run. Central 10/171,
wholePG180, audit and prior failures stay open. No merge/deploy. Draft delivery
status is reported separately after the authorized single attempt.
